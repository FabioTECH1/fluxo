package scheduler

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"fluxo/internal/services/executiondomain"
	"github.com/google/uuid"
)

const loaderEpochSchema = executiondomain.SchedulerLoaderSchema
const loaderEpochContract = executiondomain.SchedulerLoaderReaderContract

type LoaderProvider struct {
	Identity    CronBackendIdentity `json:"identity"`
	Profile     string              `json:"profile"`
	PolicyHash  string              `json:"policy_hash"`
	RuntimeHash string              `json:"runtime_hash"`
}
type LoaderTarget struct {
	Binding     string `json:"binding"`
	Nonce       string `json:"nonce"`
	TriggerHash string `json:"trigger_hash"`
	Received    bool   `json:"received"`
}
type LoaderEpoch struct {
	ID          string               `json:"id"`
	Provider    LoaderProvider       `json:"provider"`
	Predecessor string               `json:"predecessor"`
	Targets     map[int]LoaderTarget `json:"targets"`
	Phase       string               `json:"phase"`
}

// LoaderBinding is the one-use bootstrap seal's exact durable loader authority.
// It contains no claim that configuration is frozen after qualification.
type LoaderBinding struct {
	EpochID      string `json:"epoch_id"`
	EntryID      int    `json:"entry_id"`
	Binding      string `json:"binding"`
	Nonce        string `json:"nonce"`
	TriggerHash  string `json:"trigger_hash"`
	ProviderHash string `json:"provider_hash"`
}

func (b LoaderBinding) Valid() bool {
	return loaderUUID(b.EpochID) && b.EntryID > 0 && loaderUUID(b.Binding) && loaderUUID(b.Nonce) && loaderDigestValid(b.TriggerHash) && loaderDigestValid(b.ProviderHash)
}

type loaderTriggerPublication struct {
	Original []byte `json:"original"`
	Absent   bool   `json:"absent"`
	Intended []byte `json:"intended"`
}
type LoaderPublicationPlan struct {
	ID           string                           `json:"id"`
	EpochID      string                           `json:"epoch_id"`
	Epoch        LoaderEpoch                      `json:"epoch"`
	OriginalCore []byte                           `json:"original_core"`
	IntendedCore []byte                           `json:"intended_core"`
	Triggers     map[int]loaderTriggerPublication `json:"triggers"`
	Phase        string                           `json:"phase"`
}
type loaderEpochStore struct {
	Format       int                              `json:"format"`
	Current      string                           `json:"current"`
	Epochs       map[string]LoaderEpoch           `json:"epochs"`
	Publications map[string]LoaderPublicationPlan `json:"publications"`
	Pending      string                           `json:"pending"`
	Archives     map[string]loaderArchiveRef      `json:"archives"`
}

func loaderStorePath() string   { return filepath.Join(stateDir, "loader-epochs.json") }
func loaderAnchorPath() string  { return filepath.Join(stateDir, "loader-epochs.initialized") }
func loaderInitialPath() string { return filepath.Join(stateDir, "loader-epochs.initializing") }
func emptyLoaderStore() loaderEpochStore {
	return loaderEpochStore{Format: 1, Epochs: map[string]LoaderEpoch{}, Publications: map[string]LoaderPublicationPlan{}, Archives: map[string]loaderArchiveRef{}}
}
func loaderDigestValid(s string) bool {
	raw, e := hex.DecodeString(s)
	return e == nil && len(raw) == 32 && hex.EncodeToString(raw) == s
}
func loaderUUID(s string) bool                   { _, e := uuid.Parse(s); return e == nil }
func loaderProviderHash(p LoaderProvider) string { raw, _ := json.Marshal(p); return hashBytes(raw) }

func decodeLoaderStore(raw []byte) (loaderEpochStore, error) {
	var s loaderEpochStore
	if len(raw) > loaderFileBudget {
		return s, errors.New("loader ledger exceeds read budget")
	}
	if !loaderStoreFieldsPresent(raw) {
		return s, errors.New("incomplete loader epoch authority fields")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil {
		return s, err
	}
	if d.Decode(new(any)) != io.EOF {
		return s, errors.New("trailing loader store data")
	}
	if s.Format != 1 || s.Epochs == nil || s.Publications == nil || s.Archives == nil || len(s.Epochs) > 2 || len(s.Publications) != len(s.Epochs) {
		return s, errors.New("invalid loader epoch store")
	}
	if !loaderUUID(s.Current) {
		return s, errors.New("persisted loader store lost its current epoch")
	}
	for id, e := range s.Epochs {
		if id != e.ID || !loaderUUID(id) || e.Targets == nil || (e.Phase != "prepared" && e.Phase != "published") || e.Provider.Profile == "" || !loaderDigestValid(e.Provider.PolicyHash) || !loaderDigestValid(e.Provider.RuntimeHash) || ValidateBackendIdentity(e.Provider.Identity) != nil {
			return s, errors.New("invalid loader epoch")
		}
		if e.Predecessor != "" {
			_, live := s.Epochs[e.Predecessor]
			_, archived := s.Archives[e.Predecessor]
			if !live && !archived || e.Predecessor == id {
				return s, errors.New("lost loader predecessor history")
			}
		}
		for entry, t := range e.Targets {
			if entry <= 0 || !loaderUUID(t.Binding) || !loaderUUID(t.Nonce) || !loaderDigestValid(t.TriggerHash) || e.Phase == "prepared" && t.Received {
				return s, errors.New("invalid loader target")
			}
		}
	}
	if s.Current != "" {
		if _, ok := s.Epochs[s.Current]; !ok {
			return s, errors.New("lost current loader epoch")
		}
	}
	for id, p := range s.Publications {
		e, ok := s.Epochs[p.EpochID]
		if id != p.ID || !loaderUUID(id) || !ok || p.Epoch.ID != p.EpochID || p.Triggers == nil || len(p.OriginalCore) == 0 || len(p.IntendedCore) == 0 || (p.Phase != "intent" && p.Phase != "committed") {
			return s, errors.New("invalid loader publication")
		}
		if p.Phase == "intent" && (s.Pending != id || e.Phase != "prepared") || p.Phase == "committed" && e.Phase != "published" {
			return s, errors.New("loader publication phase lost its exact pending/committed boundary")
		}
		if p.Epoch.Phase != "prepared" || !reflect.DeepEqual(p.Epoch.Provider, e.Provider) || p.Epoch.Predecessor != e.Predecessor || len(p.Epoch.Targets) != len(e.Targets) {
			return s, errors.New("loader publication epoch snapshot changed")
		}
		for entry, t := range p.Epoch.Targets {
			current, ok := e.Targets[entry]
			if !ok || t.Received || t.Binding != current.Binding || t.Nonce != current.Nonce || t.TriggerHash != current.TriggerHash {
				return s, errors.New("loader publication target snapshot changed")
			}
		}
		var original, intended journal
		if json.Unmarshal(p.OriginalCore, &original) != nil || json.Unmarshal(p.IntendedCore, &intended) != nil || original.Schema != loaderEpochSchema || intended.Schema != loaderEpochSchema {
			return s, errors.New("loader publication has incompatible core snapshots")
		}
		if !intended.LoaderEpochEstablished || !reflect.DeepEqual(original.HistoricalRetirement, intended.HistoricalRetirement) || historicalRetirementPending(original) || historicalRetirementPending(intended) || !reflect.DeepEqual(original.Migrations, intended.Migrations) || !reflect.DeepEqual(original.Legacy, intended.Legacy) || !reflect.DeepEqual(original.Invocations, intended.Invocations) || !reflect.DeepEqual(original.Holds, intended.Holds) || !reflect.DeepEqual(original.Released, intended.Released) || !reflect.DeepEqual(original.Upgrade, intended.Upgrade) || !reflect.DeepEqual(original.LastSweep, intended.LastSweep) {
			return s, errors.New("loader publication changes retained history or execution obligations")
		}
		for entry, t := range p.Triggers {
			target, ok := e.Targets[entry]
			row, found := intended.Entries[entry]
			if !ok || !found || !row.Enabled || row.Identity != target.Binding || row.LoaderNonce != target.Nonce || string(t.Intended) != CronContent(row) || hashBytes(t.Intended) != target.TriggerHash || len(t.Intended) == 0 || t.Absent && len(t.Original) != 0 {
				return s, errors.New("loader trigger intent conflicts with epoch")
			}
			if old, exists := original.Entries[entry]; exists && (old.MigrationNonce != row.MigrationNonce || old.LegacyBoot != row.LegacyBoot || old.Ready != row.Ready) {
				return s, errors.New("loader publication rewrites historical registration retirement evidence")
			}
		}
		if len(p.Triggers) != len(e.Targets) || len(e.Targets) != enabledLoaderEntries(intended) {
			return s, errors.New("incomplete loader publication")
		}
	}
	for id, e := range s.Epochs {
		plans := 0
		for _, p := range s.Publications {
			if p.EpochID == id {
				plans++
			}
		}
		if plans != 1 {
			return s, errors.New("loader epoch publication history is missing or ambiguous")
		}
		seen := map[string]bool{id: true}
		previous := e.Predecessor
		for previous != "" {
			if seen[previous] {
				return s, errors.New("cyclic loader predecessor history")
			}
			seen[previous] = true
			if e, ok := s.Epochs[previous]; ok {
				previous = e.Predecessor
			} else {
				previous = s.Archives[previous].Predecessor
			}
		}
		if e.Phase == "prepared" && (s.Pending == "" || s.Publications[s.Pending].EpochID != id) {
			return s, errors.New("prepared loader epoch lost its publication intent")
		}
	}
	archivePublicationIDs := map[string]bool{}
	for id, ref := range s.Archives {
		if !loaderUUID(id) || !loaderArchiveRefValid(ref) {
			return s, errors.New("invalid immutable loader archive reference")
		}
		if _, active := s.Epochs[id]; active {
			return s, errors.New("loader archive overlaps current mutable epoch")
		}
		if _, active := s.Publications[ref.PublicationID]; active {
			return s, errors.New("loader archive publication overlaps active intent")
		}
		if archivePublicationIDs[ref.PublicationID] {
			return s, errors.New("duplicate publication identity across loader archives")
		}
		archivePublicationIDs[ref.PublicationID] = true
	}
	seen := map[string]bool{}
	id := s.Current
	for id != "" {
		if seen[id] {
			return s, errors.New("cyclic loader archive lineage")
		}
		seen[id] = true
		if e, ok := s.Epochs[id]; ok {
			id = e.Predecessor
		} else if ref, ok := s.Archives[id]; ok {
			id = ref.Predecessor
		} else {
			return s, errors.New("lost loader archive predecessor")
		}
	}
	if len(seen) != len(s.Epochs)+len(s.Archives) {
		return s, errors.New("unreachable loader history cannot be silently detached")
	}
	if s.Pending != "" {
		p, ok := s.Publications[s.Pending]
		if !ok || p.Phase != "intent" || p.EpochID != s.Current || s.Epochs[p.EpochID].Phase != "prepared" {
			return s, errors.New("invalid pending loader publication")
		}
	}
	return s, nil
}

func loaderStoreFieldsPresent(raw []byte) bool {
	fields := func(raw json.RawMessage, names ...string) (map[string]json.RawMessage, bool) {
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil || m == nil {
			return nil, false
		}
		for _, name := range names {
			value, present := m[name]
			// A nil original payload represents explicit trigger absence only; all
			// mandatory scalar/map authority fields require a concrete JSON value.
			if !present || name != "original" && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return nil, false
			}
		}
		return m, true
	}
	var epochPresent func(json.RawMessage) bool
	epochPresent = func(raw json.RawMessage) bool {
		e, ok := fields(raw, "id", "provider", "predecessor", "targets", "phase")
		if !ok {
			return false
		}
		if _, ok = fields(e["provider"], "identity", "profile", "policy_hash", "runtime_hash"); !ok {
			return false
		}
		var targets map[string]json.RawMessage
		if json.Unmarshal(e["targets"], &targets) != nil || targets == nil {
			return false
		}
		for _, raw := range targets {
			target, ok := fields(raw, "binding", "nonce", "trigger_hash", "received")
			if !ok || string(target["received"]) == "null" {
				return false
			}
		}
		return true
	}
	root, ok := fields(raw, "format", "current", "epochs", "publications", "pending", "archives")
	if !ok {
		return false
	}
	var epochs, plans, archives map[string]json.RawMessage
	if json.Unmarshal(root["epochs"], &epochs) != nil || epochs == nil || json.Unmarshal(root["publications"], &plans) != nil || plans == nil || json.Unmarshal(root["archives"], &archives) != nil || archives == nil {
		return false
	}
	for _, raw := range archives {
		if _, ok := fields(raw, "publication_id", "predecessor", "sha256", "device", "inode", "size", "modified_ns", "changed_ns"); !ok {
			return false
		}
	}
	for _, raw := range epochs {
		if !epochPresent(raw) {
			return false
		}
	}
	for _, raw := range plans {
		p, ok := fields(raw, "id", "epoch_id", "epoch", "original_core", "intended_core", "triggers", "phase")
		if !ok || !epochPresent(p["epoch"]) {
			return false
		}
		var triggers map[string]json.RawMessage
		if json.Unmarshal(p["triggers"], &triggers) != nil || triggers == nil {
			return false
		}
		for _, raw := range triggers {
			t, ok := fields(raw, "original", "absent", "intended")
			if !ok || bytes.Equal(bytes.TrimSpace(t["original"]), []byte("null")) && !bytes.Equal(bytes.TrimSpace(t["absent"]), []byte("true")) {
				return false
			}
		}
	}
	return true
}

func readLoaderStore() (loaderEpochStore, error) {
	s := emptyLoaderStore()
	_, a := os.Lstat(loaderAnchorPath())
	_, b := os.Lstat(loaderStorePath())
	_, c := os.Lstat(loaderInitialPath())
	if os.IsNotExist(a) && os.IsNotExist(b) && os.IsNotExist(c) {
		return s, nil
	}
	if c == nil {
		return s, errors.New("loader initialization requires recovery")
	}
	if !os.IsNotExist(c) {
		return s, c
	}
	anchor, _, err := readLoaderProtectedFile(loaderAnchorPath(), 16)
	if err != nil || string(anchor) != "1\n" {
		return s, errors.New("invalid loader initialization anchor")
	}
	raw, _, err := readLoaderProtectedFile(loaderStorePath(), loaderFileBudget)
	if err != nil {
		return s, err
	}
	if len(raw) > 64<<20 {
		return s, errors.New("loader history exceeds preservation budget")
	}
	s, err = decodeLoaderStore(raw)
	if err != nil {
		return s, err
	}
	if err = verifyLoaderArchiveReferences(s); err != nil {
		return s, err
	}
	return s, nil
}

// CheckLoaderPublicationState fences ordinary core readers/writers during an
// interrupted publication. Only explicit exact-intent recovery bypasses it.
func CheckLoaderPublicationState(j journal) error {
	if j.Schema != loaderEpochSchema {
		return nil
	}
	s, err := readLoaderStore()
	if err != nil {
		return err
	}
	return validateLoaderPublicationCoreState(j, s)
}

func validateLoaderPublicationCoreState(j journal, s loaderEpochStore) error {
	if historicalRetirementPending(j) && (s.Current != "" || s.Pending != "" || len(s.Epochs) != 0) {
		return errors.New("historical retirement conflicts with loader publication history")
	}
	if s.Pending != "" {
		return errors.New("recover pending loader publication before ordinary Scheduler work")
	}
	if j.LoaderEpochEstablished != (s.Current != "") {
		return errors.New("loader core/anchor history disagrees; preserve and reconcile")
	}
	return nil
}

func requireLoaderWriter(ctx context.Context) error {
	path := filepath.Join(stateDir, "dispatcher")
	if err := trustedFile(path); err != nil {
		return err
	}
	sha, err := fileDigest(path)
	if err != nil {
		return err
	}
	helper, err := executiondomain.ReadHelperCapabilities(ctx, path, sha)
	if err != nil {
		return err
	}
	return executiondomain.GuardLoaderWrite(filepath.Dir(databasePath), helper)
}

func saveLoaderStore(s loaderEpochStore) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if len(raw) > 64<<20 {
		return errors.New("retain/export loader history before continuing; preservation budget reached")
	}
	if _, err = decodeLoaderStore(raw); err != nil {
		return err
	}
	if _, err = os.Lstat(loaderAnchorPath()); os.IsNotExist(err) {
		for _, p := range []string{loaderStorePath(), loaderInitialPath()} {
			if _, e := os.Lstat(p); !os.IsNotExist(e) {
				return errors.New("loader initialization requires explicit recovery")
			}
		}
		if err = atomicFile(loaderInitialPath(), raw, 0600); err != nil {
			return err
		}
		if err = atomicFile(loaderStorePath(), raw, 0600); err != nil {
			return err
		}
		if err = atomicFile(loaderAnchorPath(), []byte("1\n"), 0600); err != nil {
			return err
		}
		if err = os.Remove(loaderInitialPath()); err != nil {
			return err
		}
		return syncStateDir()
	} else if err != nil {
		return err
	}
	if err = trustedFile(loaderStorePath()); err != nil {
		return err
	}
	return atomicFile(loaderStorePath(), raw, 0600)
}

func syncStateDir() error {
	f, e := os.Open(stateDir)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}

func loaderEntryBinding(j journal, s loaderEpochStore, e Entry) (LoaderBinding, error) {
	current, ok := j.Entries[e.ID]
	if j.Schema != loaderEpochSchema || !j.LoaderEpochEstablished || !ok || !e.Enabled || !current.Enabled || e.Identity != current.Identity || identity(e) != identity(current) || e.LoaderNonce != current.LoaderNonce {
		return LoaderBinding{}, errors.New("current loader registration changed")
	}
	epoch, ok := s.Epochs[s.Current]
	target, found := epoch.Targets[e.ID]
	if s.Pending != "" || !ok || epoch.Phase != "published" || !found || !target.Received || target.Binding != e.Identity || target.Nonce != e.LoaderNonce || target.TriggerHash != hashBytes([]byte(CronContent(e))) {
		return LoaderBinding{}, errors.New("waiting for exact current loader epoch receipt")
	}
	return LoaderBinding{EpochID: epoch.ID, EntryID: e.ID, Binding: target.Binding, Nonce: target.Nonce, TriggerHash: target.TriggerHash, ProviderHash: loaderProviderHash(epoch.Provider)}, nil
}

func ValidateEntryLoaderBinding(j journal, e Entry, binding LoaderBinding) error {
	if !binding.Valid() {
		return errors.New("invalid sealed loader binding")
	}
	s, err := readLoaderStore()
	if err != nil {
		return err
	}
	now, err := loaderEntryBinding(j, s, e)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(now, binding) {
		return errors.New("loader epoch changed after bootstrap validation")
	}
	if err = owned(CronPath(e.ID), false); err != nil {
		return err
	}
	raw, err := os.ReadFile(CronPath(e.ID))
	if err != nil || hashBytes(raw) != binding.TriggerHash {
		return errors.New("loader trigger changed after bootstrap validation")
	}
	return nil
}

func RequireEntryLoaderBinding(ctx context.Context, j journal, e Entry) (LoaderBinding, error) {
	s, err := readLoaderStore()
	if err != nil {
		return LoaderBinding{}, err
	}
	b, err := loaderEntryBinding(j, s, e)
	if err != nil {
		return b, err
	}
	if err = ValidateEntryLoaderBinding(j, e, b); err != nil {
		return b, err
	}
	provider := s.Epochs[s.Current].Provider
	now, err := qualifyLoaderProvider(ctx, &provider)
	if err != nil {
		return b, err
	}
	if !reflect.DeepEqual(now, s.Epochs[s.Current].Provider) {
		return b, errors.New("loader provider generation changed; await current epoch receipt")
	}
	return b, nil
}

// RequireLoaderEpochReadyLocked also covers initialized zero-job state. A real
// published empty-target epoch is explicit evidence, never inferred from a
// missing ledger. Genuinely absent subsystem handling stays with the caller.
func RequireLoaderEpochReadyLocked(ctx context.Context, j journal) error {
	s, err := readLoaderStore()
	if err != nil {
		return err
	}
	provider, err := loaderEpochReadySnapshot(j, s)
	if err != nil {
		return err
	}
	for _, e := range j.Entries {
		if !e.Enabled {
			continue
		}
		target := s.Epochs[s.Current].Targets[e.ID]
		raw, _, err := readLoaderProtectedFile(CronPath(e.ID), 1<<20)
		if err != nil || hashBytes(raw) != target.TriggerHash {
			return errors.New("current loader readiness trigger changed")
		}
	}
	now, err := qualifyLoaderProvider(ctx, &provider)
	if err != nil {
		return err
	}
	if !sameLoaderProvider(now, provider) {
		return errors.New("current loader provider generation changed")
	}
	return nil
}

func loaderEpochReadySnapshot(j journal, s loaderEpochStore) (LoaderProvider, error) {
	epoch, ok := s.Epochs[s.Current]
	if j.Schema != loaderEpochSchema || !j.LoaderEpochEstablished || s.Pending != "" || !ok || epoch.Phase != "published" || len(epoch.Targets) != enabledLoaderEntries(j) {
		return LoaderProvider{}, diagnostic("waiting_for_loader_epoch", "Scheduled-job loading has not been verified yet.")
	}
	for _, e := range j.Entries {
		if e.Enabled {
			if _, err := loaderEntryBinding(j, s, e); err != nil {
				return LoaderProvider{}, err
			}
		}
	}
	return epoch.Provider, nil
}
