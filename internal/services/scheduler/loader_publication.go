package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"

	"fluxo/internal/services/executiondomain"
	"github.com/google/uuid"
)

// The caller owns the Scheduler gate. An explicit predecessor is required for
// new missing triggers; ordinary generation renewal accepts only exact current
// generated triggers. The plan is read-only until Apply persists its intent.
func PlanLoaderPublicationLocked(ctx context.Context, intended journal, predecessors map[int][]byte) (LoaderPublicationPlan, error) {
	return planLoaderPublicationLocked(ctx, intended, predecessors, nil)
}

func planLoaderPublicationLocked(ctx context.Context, intended journal, predecessors map[int][]byte, review *LoaderPolicyReview) (LoaderPublicationPlan, error) {
	if intended.Schema != loaderEpochSchema {
		return LoaderPublicationPlan{}, errors.New("loader epochs require fenced schema5")
	}
	if err := loaderPublicationMayStart(intended); err != nil {
		return LoaderPublicationPlan{}, err
	}
	if len(intended.Legacy) != 0 {
		return LoaderPublicationPlan{}, errors.New("finish historical Scheduler retirement before publishing a current loader epoch")
	}
	if err := requireLoaderWriter(ctx); err != nil {
		return LoaderPublicationPlan{}, err
	}
	s, err := readLoaderStore()
	if err != nil {
		return LoaderPublicationPlan{}, err
	}
	if intended.LoaderEpochEstablished && s.Current == "" {
		return LoaderPublicationPlan{}, errors.New("established loader history is missing")
	}
	if s.Pending != "" {
		return LoaderPublicationPlan{}, errors.New("recover pending loader publication first")
	}
	before, err := os.ReadFile(filepath.Join(stateDir, "state.json"))
	if err != nil {
		return LoaderPublicationPlan{}, err
	}
	var original journal
	if json.Unmarshal(before, &original) != nil || original.Schema != loaderEpochSchema {
		return LoaderPublicationPlan{}, errors.New("loader original core is not current")
	}
	if s.Current != "" && !original.LoaderEpochEstablished {
		return LoaderPublicationPlan{}, errors.New("established loader core marker is missing")
	}
	if !reflect.DeepEqual(original.HistoricalRetirement, intended.HistoricalRetirement) || historicalRetirementPending(original) || historicalRetirementPending(intended) || !reflect.DeepEqual(original.Migrations, intended.Migrations) || !reflect.DeepEqual(original.Legacy, intended.Legacy) || !reflect.DeepEqual(original.Invocations, intended.Invocations) || !reflect.DeepEqual(original.Holds, intended.Holds) || !reflect.DeepEqual(original.Released, intended.Released) || !reflect.DeepEqual(original.Upgrade, intended.Upgrade) || !reflect.DeepEqual(original.LastSweep, intended.LastSweep) {
		return LoaderPublicationPlan{}, errors.New("loader plan cannot mutate history, holds or invocation state")
	}
	var saved *LoaderProvider
	if e, ok := s.Epochs[s.Current]; ok {
		p := e.Provider
		saved = &p
	}
	var provider LoaderProvider
	if review == nil {
		provider, err = qualifyLoaderProvider(ctx, saved)
	} else {
		if saved == nil || s.Current != review.Epoch || !sameLoaderProvider(*saved, review.Saved) || !reflect.DeepEqual(original.Entries, intended.Entries) || len(predecessors) != 0 {
			return LoaderPublicationPlan{}, errors.New("reviewed loader policy predecessor or entry set changed")
		}
		provider, err = inspectLoaderProvider(ctx, saved)
		if err == nil && !sameLoaderProvider(provider, review.Candidate) {
			err = errors.New("reviewed loader policy candidate changed")
		}
	}
	if err != nil {
		return LoaderPublicationPlan{}, err
	}
	if current, ok := s.Epochs[s.Current]; ok && current.Phase == "published" && sameLoaderProvider(current.Provider, provider) && reflect.DeepEqual(original.Entries, intended.Entries) && len(current.Targets) == enabledLoaderEntries(intended) {
		unchanged := true
		for id, target := range current.Targets {
			e, found := intended.Entries[id]
			raw, readErr := os.ReadFile(CronPath(id))
			if !found || !e.Enabled || target.Binding != e.Identity || target.Nonce != e.LoaderNonce || target.TriggerHash != hashBytes([]byte(CronContent(e))) || readErr != nil || string(raw) != CronContent(e) {
				unchanged = false
				break
			}
			if err = owned(CronPath(id), false); err != nil {
				return LoaderPublicationPlan{}, err
			}
		}
		if unchanged {
			return LoaderPublicationPlan{OriginalCore: before, IntendedCore: before}, nil
		}
	}
	epoch := LoaderEpoch{ID: uuid.NewString(), Provider: provider, Predecessor: s.Current, Targets: map[int]LoaderTarget{}, Phase: "prepared"}
	plan := LoaderPublicationPlan{ID: uuid.NewString(), EpochID: epoch.ID, OriginalCore: before, Triggers: map[int]loaderTriggerPublication{}, Phase: "intent"}
	// Copy entries so planning cannot mutate the caller's map through aliasing.
	intended.Entries = cloneLoaderEntries(intended.Entries)
	intended.LoaderEpochEstablished = true
	for id, e := range intended.Entries {
		if !e.Enabled {
			continue
		}
		if e.ID != id || !loaderUUID(e.Identity) {
			return plan, errors.New("invalid loader registration binding")
		}
		if err = committed(ctx, e, true); err != nil {
			return plan, err
		}
		raw, readErr := os.ReadFile(CronPath(id))
		absent := os.IsNotExist(readErr)
		if readErr != nil && !absent {
			return plan, readErr
		}
		if !absent {
			if err = owned(CronPath(id), false); err != nil {
				return plan, err
			}
		}
		if old, ok := original.Entries[id]; ok && string(raw) == CronContent(old) && !absent {
			// Exact currently published generated predecessor.
		} else if allowed, ok := predecessors[id]; !ok || !absent || allowed != nil {
			return plan, errors.New("loader publication accepts only exact managed predecessors or explicitly authorized absence; retire plaintext before format activation")
		}
		e.LoaderNonce = uuid.NewString()
		intended.Entries[id] = e
		after := []byte(CronContent(e))
		epoch.Targets[id] = LoaderTarget{Binding: e.Identity, Nonce: e.LoaderNonce, TriggerHash: hashBytes(after)}
		plan.Triggers[id] = loaderTriggerPublication{Original: raw, Absent: absent, Intended: after}
	}
	after, err := executiondomain.MergeActivationJSON(before, intended)
	if err != nil {
		return plan, err
	}
	plan.IntendedCore = after
	// Carry provider/targets in the intended plan wrapper. It is persisted with
	// the publication below before either core nonces or triggers can change.
	plan.Epoch = epoch
	return plan, nil
}

func cloneLoaderEntries(entries map[int]Entry) map[int]Entry {
	out := make(map[int]Entry, len(entries))
	for id, e := range entries {
		out[id] = e
	}
	return out
}

// Apply accepts the exact returned plan/epoch pair, not reconstructable intent
// inferred from a partial core. The caller's in-memory journal is refreshed.
func ApplyLoaderPublicationLocked(ctx context.Context, j *journal, plan LoaderPublicationPlan) error {
	if err := requireLoaderWriter(ctx); err != nil {
		return err
	}
	s, err := readLoaderStore()
	if err != nil {
		return err
	}
	if s.Pending != "" {
		return errors.New("recover pending loader publication first")
	}
	if err = loaderPublicationMayStart(*j); err != nil {
		return err
	}
	if plan.ID == "" {
		current, readErr := os.ReadFile(filepath.Join(stateDir, "state.json"))
		if readErr != nil || len(plan.OriginalCore) == 0 || !bytes.Equal(current, plan.OriginalCore) || !bytes.Equal(plan.OriginalCore, plan.IntendedCore) {
			return errors.New("unchanged loader publication core changed")
		}
		return replaceLoaderJournal(j, current)
	}
	epoch := plan.Epoch
	if epoch.ID != plan.EpochID || !loaderUUID(epoch.ID) {
		return errors.New("explicit loader publication plan is missing")
	}
	if epoch.Predecessor != s.Current {
		return errors.New("loader publication predecessor changed")
	}
	current, err := os.ReadFile(filepath.Join(stateDir, "state.json"))
	if err != nil || !bytes.Equal(current, plan.OriginalCore) {
		return errors.New("loader core changed before publication intent")
	}
	s.Epochs[epoch.ID] = epoch
	s.Current = epoch.ID
	s.Publications[plan.ID] = plan
	s.Pending = plan.ID
	if err = saveLoaderStore(s); err != nil {
		return err
	}
	if err = applyPendingLoaderPublication(ctx, &s); err != nil {
		return err
	}
	return replaceLoaderJournal(j, plan.IntendedCore)
}

func replaceLoaderJournal(j *journal, raw []byte) error {
	var next journal
	if err := json.Unmarshal(raw, &next); err != nil {
		return err
	}
	*j = next
	return nil
}

func applyPendingLoaderPublication(ctx context.Context, s *loaderEpochStore) error {
	if err := requireLoaderWriter(ctx); err != nil {
		return err
	}
	p, ok := s.Publications[s.Pending]
	if !ok || p.Phase != "intent" {
		return errors.New("loader publication intent is missing")
	}
	core := filepath.Join(stateDir, "state.json")
	raw, err := os.ReadFile(core)
	if err != nil {
		return err
	}
	observed := map[int]loaderObservedTrigger{}
	// Validate all predecessors before making any new mutation. Recovery may
	// see either exact side of any interrupted atomic replacement.
	for id := range p.Triggers {
		raw, e := os.ReadFile(CronPath(id))
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		observed[id] = loaderObservedTrigger{Data: raw, Absent: os.IsNotExist(e)}
		if e == nil {
			if err = owned(CronPath(id), false); err != nil {
				return err
			}
		}
	}
	if err = validateLoaderRecoveryState(p, raw, observed); err != nil {
		return err
	}
	if err = archiveSupersededLoaderPairs(s); err != nil {
		return err
	}
	if !bytes.Equal(raw, p.IntendedCore) {
		if err = atomicFile(core, p.IntendedCore, 0600); err != nil {
			return err
		}
	}
	for id, t := range p.Triggers {
		if err = publishCron(CronPath(id), t.Intended); err != nil {
			return err
		}
		actual, e := os.ReadFile(CronPath(id))
		if e != nil || !bytes.Equal(actual, t.Intended) {
			return errors.New("loader trigger publication readback failed")
		}
	}
	actual, err := os.ReadFile(core)
	if err != nil || !bytes.Equal(actual, p.IntendedCore) {
		return errors.New("loader core publication readback failed")
	}
	epoch := s.Epochs[p.EpochID]
	epoch.Phase = "published"
	s.Epochs[epoch.ID] = epoch
	p.Phase = "committed"
	s.Publications[p.ID] = p
	s.Pending = ""
	return saveLoaderStore(*s)
}

type loaderObservedTrigger struct {
	Data   []byte
	Absent bool
}

func validateLoaderRecoveryState(p LoaderPublicationPlan, core []byte, observed map[int]loaderObservedTrigger) error {
	if !bytes.Equal(core, p.OriginalCore) && !bytes.Equal(core, p.IntendedCore) {
		return errors.New("loader recovery core conflicts with exact saved intent")
	}
	if len(observed) != len(p.Triggers) {
		return errors.New("loader recovery observation is incomplete")
	}
	for id, t := range p.Triggers {
		v, ok := observed[id]
		if !ok || !(v.Absent && t.Absent || !v.Absent && bytes.Equal(v.Data, t.Intended) || !v.Absent && !t.Absent && bytes.Equal(v.Data, t.Original)) {
			return errors.New("loader recovery trigger conflicts with exact saved intent")
		}
	}
	return nil
}

func RecoverLoaderPublicationsLocked(ctx context.Context) error {
	if err := requireLoaderWriter(ctx); err != nil {
		return err
	}
	if err := recoverLoaderInitializationLocked(); err != nil {
		return err
	}

	s, err := readLoaderStore()
	if err != nil {
		return err
	}
	if s.Pending == "" {
		return nil
	}
	return applyPendingLoaderPublication(ctx, &s)
}

func ReconcileLoaderEpochs(ctx context.Context) error {
	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	if err = RecoverLoaderPublicationsLocked(ctx); err != nil {
		return err
	}
	j, err := read()
	if err != nil {
		return err
	}
	if j.Schema != loaderEpochSchema {
		return errors.New("activate schema5 before reconciling loader epochs")
	}
	if historicalRetirementPending(j) {
		return nil // Backend receipts and drainage must precede loader publication.
	}
	// A previously durable publication is recovered above, including its exact
	// saved upgrade/hold fields. Healthchecks may not start a NEW generation
	// while the installer owns its monotonic-journal/trigger snapshot boundary.
	if loaderPublicationMayStart(j) != nil {
		return nil
	}
	s, err := readLoaderStore()
	if err != nil {
		return err
	}
	if j.LoaderEpochEstablished && s.Current == "" {
		return errors.New("established loader history is missing")
	}
	if epoch, ok := s.Epochs[s.Current]; ok {
		provider, e := qualifyLoaderProvider(ctx, &epoch.Provider)
		if e != nil {
			return e
		}
		matches := sameLoaderProvider(provider, epoch.Provider) && len(epoch.Targets) == enabledLoaderEntries(j)
		for id, t := range epoch.Targets {
			e, ok := j.Entries[id]
			if !ok || !e.Enabled || t.Binding != e.Identity || t.Nonce != e.LoaderNonce || t.TriggerHash != hashBytes([]byte(CronContent(e))) {
				matches = false
			}
		}
		if matches {
			return nil
		}
	}
	p, err := PlanLoaderPublicationLocked(ctx, j, nil)
	if err != nil {
		return err
	}
	return ApplyLoaderPublicationLocked(ctx, &j, p)
}

func loaderPublicationMayStart(j journal) error {
	if historicalRetirementPending(j) {
		return errors.New("Scheduler loader publication is held for historical retirement")
	}
	if j.Upgrade != nil {
		return errors.New("Scheduler loader publication is held by the installer")
	}
	if len(j.Holds) != 0 {
		return errors.New("Scheduler loader publication is held for maintenance")
	}
	return nil
}

// The existing startup/periodic reconciler reaches generation renewal only for
// the explicitly fenced format. Header inspection grants no write admission;
// Reconcile independently checks the committed floor and actual helper roles.
func reconcileLoaderIfCurrent(ctx context.Context) error {
	raw, err := os.ReadFile(filepath.Join(stateDir, "state.json"))
	if err != nil {
		return err
	}
	var header struct {
		Schema int `json:"schema"`
	}
	if err = json.Unmarshal(raw, &header); err != nil {
		return err
	}
	if header.Schema != loaderEpochSchema {
		return nil
	}
	return ReconcileLoaderEpochs(ctx)
}
func enabledLoaderEntries(j journal) int {
	n := 0
	for _, e := range j.Entries {
		if e.Enabled {
			n++
		}
	}
	return n
}

// Caller owns writer admission and the journal gate. Validate every retained
// initialization participant before the first write. Conflicting evidence is
// never normalized into a usable anchor by recovery.
func recoverLoaderInitializationLocked() error {
	raw, _, err := readLoaderProtectedFile(loaderInitialPath(), loaderFileBudget)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = decodeLoaderStore(raw); err != nil {
		return err
	}
	current, _, readErr := readLoaderProtectedFile(loaderStorePath(), loaderFileBudget)
	if readErr != nil && !os.IsNotExist(readErr) {
		return readErr
	}
	if readErr == nil && !bytes.Equal(current, raw) {
		return errors.New("loader initialization evidence conflicts")
	}
	anchor, _, anchorErr := readLoaderProtectedFile(loaderAnchorPath(), 16)
	if anchorErr != nil && !os.IsNotExist(anchorErr) {
		return anchorErr
	}
	if anchorErr == nil && !bytes.Equal(anchor, []byte("1\n")) {
		return errors.New("loader initialization anchor conflicts; retain original evidence")
	}
	if os.IsNotExist(readErr) {
		if err = atomicFile(loaderStorePath(), raw, 0600); err != nil {
			return err
		}
	}
	if os.IsNotExist(anchorErr) {
		if err = atomicFile(loaderAnchorPath(), []byte("1\n"), 0600); err != nil {
			return err
		}
	}
	if err = os.Remove(loaderInitialPath()); err != nil {
		return err
	}
	return syncStateDir()
}
