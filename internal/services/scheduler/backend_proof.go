package scheduler

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/google/uuid"
)

// Old dispatchers rewrite schema 2. Backend proof is deliberately separate so
// an in-flight v0.4.36 helper cannot discard new authority/history fields.
type backendProofs struct {
	Format   int                       `json:"format"`
	Branches map[string]backendAttempt `json:"branches"`
}
type backendAttempt struct {
	ID               string                   `json:"id"`
	Boot             string                   `json:"boot"`
	Host             legacyHost               `json:"host"`
	Bindings         map[int]string           `json:"bindings"`
	Migration        legacyMigration          `json:"migration"`
	Previous         legacyMigration          `json:"previous"`
	Preparing        bool                     `json:"preparing"`
	Certified        bool                     `json:"certified"`
	RetiredByBoot    string                   `json:"retired_by_boot,omitempty"`
	PreviousAttempts []backendAttemptEvidence `json:"previous_attempts"`
	PublishedAt      int64                    `json:"published_at"`
}
type backendAttemptEvidence struct {
	ID        string          `json:"id"`
	Host      legacyHost      `json:"host"`
	Migration legacyMigration `json:"migration"`
	Bindings  map[int]string  `json:"bindings"`
}

func proofPath() string   { return filepath.Join(stateDir, "backend-proofs.json") }
func proofAnchor() string { return filepath.Join(stateDir, "backend-proofs.initialized") }

// Caller owns the core gate. A missing file after initialization is never an
// empty history. Neither status nor diagnostic reads create these files.
func readBackendProofs() (backendProofs, error) {
	p := backendProofs{Format: 1, Branches: map[string]backendAttempt{}}
	_, a := os.Lstat(proofAnchor())
	_, b := os.Lstat(proofPath())
	if errors.Is(a, os.ErrNotExist) && errors.Is(b, os.ErrNotExist) {
		if _, err := os.Lstat(proofInitialization()); !os.IsNotExist(err) {
			return p, errors.New("Scheduler backend initialization is pending reconciliation")
		}
		return p, nil
	}
	if err := owned(proofAnchor(), false); err != nil {
		return p, fmt.Errorf("Scheduler backend evidence requires reconciliation: %w", err)
	}
	if err := owned(proofPath(), false); err != nil {
		return p, fmt.Errorf("Scheduler backend evidence requires reconciliation: %w", err)
	}
	raw, err := os.ReadFile(proofPath())
	if err != nil {
		return p, err
	}
	return decodeBackendProofs(raw)
}

func decodeBackendProofs(raw []byte) (backendProofs, error) {
	p := backendProofs{}
	if !backendProofFieldsPresent(raw) {
		return p, errors.New("Incomplete Scheduler backend evidence")
	}
	err := json.Unmarshal(raw, &p)
	if err != nil {
		return p, err
	}
	if p.Format != 1 || len(p.Branches) == 0 {
		return p, errors.New("Unsupported Scheduler backend evidence")
	}
	for branch, a := range p.Branches {
		if !branchRE.MatchString(branch) || a.Bindings == nil || a.Migration.Replacements == nil || a.Migration.Deletions == nil || (a.Host.Backend != "" && a.Host.Backend != "cronie-noble-1") || (a.Host.Backend == "cronie-noble-1" && a.Host.Group != cronieAdapter.group) || a.Host.PID <= 1 || a.Host.Start == "" || a.Host.GroupInode == 0 || (a.Host.Backend != "" && len(a.Host.Configuration) != 64) {
			return p, errors.New("Invalid Scheduler backend attempt")
		}
		if _, err = uuid.Parse(a.ID); err != nil {
			return p, err
		}
		if a.PreviousAttempts == nil || a.Preparing && a.Certified || !a.Preparing && a.PublishedAt <= 0 && a.RetiredByBoot == "" {
			return p, errors.New("Invalid Scheduler backend transition")
		}
		if err = validateBackendMigration(a.Previous); err != nil {
			return p, err
		}
		if err = validateBackendMigration(a.Migration); err != nil {
			return p, err
		}
		for _, previous := range a.PreviousAttempts {
			if _, err = uuid.Parse(previous.ID); err != nil || previous.ID == a.ID {
				return p, errors.New("Invalid retained Scheduler attempt")
			}
			if _, err = uuid.Parse(previous.Host.Boot); err != nil || previous.Host.PID <= 1 || previous.Host.Start == "" || previous.Host.GroupInode == 0 {
				return p, errors.New("Invalid retained Scheduler host")
			}
			if err = validateBackendMigration(previous.Migration); err != nil {
				return p, err
			}
			if previous.Bindings == nil || len(previous.Bindings) != len(previous.Migration.Replacements) {
				return p, errors.New("Invalid retained Scheduler bindings")
			}
			for id := range previous.Migration.Replacements {
				if _, err = uuid.Parse(previous.Bindings[id]); err != nil {
					return p, errors.New("Invalid retained Scheduler registration")
				}
			}
		}
		if _, err = uuid.Parse(a.Boot); err != nil || a.Host.Boot != a.Boot {
			return p, errors.New("Invalid Scheduler backend boot")
		}
		if a.Migration.Host != nil || a.Migration.Certified || len(a.Bindings) != len(a.Migration.Replacements) {
			return p, errors.New("Invalid Scheduler backend projection")
		}
		for id, target := range a.Migration.Replacements {
			if _, err = uuid.Parse(a.Bindings[id]); err != nil || id <= 0 || len(target.Hash) != 64 || a.Migration.Deletions[id] {
				return p, errors.New("Invalid Scheduler backend target")
			}
			if _, err = uuid.Parse(target.Nonce); err != nil {
				return p, err
			}
		}
		for id, deleted := range a.Migration.Deletions {
			if id <= 0 || !deleted {
				return p, errors.New("Invalid Scheduler backend deletion")
			}
		}
		if len(a.Migration.Deletions) > 0 {
			if _, err = uuid.Parse(a.Migration.Marker); err != nil || a.Migration.MarkerPhase < 1 || a.Migration.MarkerPhase > 2 {
				return p, errors.New("Invalid Scheduler backend marker")
			}
		}
		if a.RetiredByBoot != "" {
			if _, err = uuid.Parse(a.RetiredByBoot); err != nil || a.RetiredByBoot == a.Boot || !a.Certified {
				return p, errors.New("Invalid Scheduler boot retirement")
			}
		}
		if a.Certified && a.RetiredByBoot == "" {
			for _, target := range a.Migration.Replacements {
				if !target.Received {
					return p, errors.New("Certified Scheduler backend lacks reload evidence")
				}
			}
			if len(a.Migration.Deletions) > 0 && (a.Migration.MarkerPhase != 2 || !a.Migration.MarkerReceived) {
				return p, errors.New("Certified Scheduler backend lacks deletion evidence")
			}
		}
	}
	return p, nil
}

func validateBackendMigration(m legacyMigration) error {
	if m.Host != nil || m.Replacements == nil || m.Deletions == nil {
		return errors.New("Invalid Scheduler backend migration")
	}
	for id, target := range m.Replacements {
		if _, err := uuid.Parse(target.Nonce); err != nil || id <= 0 || m.Deletions[id] {
			return errors.New("Invalid Scheduler backend replacement")
		}
		hash, err := hex.DecodeString(target.Hash)
		if err != nil || len(hash) != 32 {
			return errors.New("Invalid Scheduler backend replacement digest")
		}
	}
	for id, deleted := range m.Deletions {
		if id <= 0 || !deleted {
			return errors.New("Invalid Scheduler backend deletion")
		}
	}
	if len(m.Deletions) > 0 {
		if _, err := uuid.Parse(m.Marker); err != nil || m.MarkerPhase < 1 || m.MarkerPhase > 2 {
			return errors.New("Invalid Scheduler backend marker")
		}
	}
	return nil
}

// A zero bool is not evidence that an omitted transition field was false.
func backendProofFieldsPresent(raw []byte) bool {
	required := func(raw json.RawMessage, names ...string) (map[string]json.RawMessage, bool) {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return nil, false
		}
		for _, name := range names {
			if v, ok := fields[name]; !ok || string(v) == "null" {
				return nil, false
			}
		}
		return fields, true
	}
	fields, ok := required(raw, "format", "branches")
	if !ok {
		return false
	}
	var branches map[string]json.RawMessage
	if json.Unmarshal(fields["branches"], &branches) != nil {
		return false
	}
	for _, value := range branches {
		a, ok := required(value, "id", "boot", "host", "bindings", "migration", "previous", "preparing", "certified", "published_at", "previous_attempts")
		if !ok {
			return false
		}
		if _, ok = required(a["host"], "boot", "pid", "start", "group_inode"); !ok {
			return false
		}
		var history []json.RawMessage
		if json.Unmarshal(a["previous_attempts"], &history) != nil {
			return false
		}
		migrations := []json.RawMessage{a["migration"], a["previous"]}
		for _, previous := range history {
			h, ok := required(previous, "id", "host", "migration", "bindings")
			if !ok {
				return false
			}
			if _, ok = required(h["host"], "boot", "pid", "start", "group_inode"); !ok {
				return false
			}
			// Reuse the migration-field validation below for retained evidence.
			migrations = append(migrations, h["migration"])
		}
		for _, rawMigration := range migrations {
			m, ok := required(rawMigration, "replacements", "deletions", "marker", "marker_phase", "marker_received", "previous_marker", "certified", "reason")
			if !ok {
				return false
			}
			if _, ok = m["host"]; !ok {
				return false
			}
			var replacements map[string]json.RawMessage
			if json.Unmarshal(m["replacements"], &replacements) != nil {
				return false
			}
			for _, target := range replacements {
				if _, ok = required(target, "nonce", "hash", "received"); !ok {
					return false
				}
			}
		}
	}
	return true
}

// The root-private initialization intent is durable before either final file.
// Completed initialization removes it. Consequently loss of an established
// proof or anchor cannot be mistaken for an interrupted first initialization.
func proofInitialization() string { return filepath.Join(stateDir, "backend-proofs.initializing") }

func finishProofInitialization() error {
	if err := os.Remove(proofInitialization()); err != nil && !os.IsNotExist(err) {
		return err
	}
	dir, err := os.Open(stateDir)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func recoverBackendProofInitialization() error {
	if _, err := os.Lstat(proofInitialization()); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := owned(proofInitialization(), false); err != nil {
		return err
	}
	raw, err := os.ReadFile(proofInitialization())
	if err != nil {
		return err
	}
	p, err := decodeBackendProofs(raw)
	if err != nil {
		return err
	}
	if len(p.Branches) != 1 {
		return errors.New("Scheduler backend initialization needs review")
	}
	for _, a := range p.Branches {
		if !a.Preparing || a.Certified || a.PublishedAt != 0 || len(a.PreviousAttempts) != 0 {
			return errors.New("Invalid Scheduler backend initialization intent")
		}
	}
	_, anchorErr := os.Lstat(proofAnchor())
	stored, proofErr := os.ReadFile(proofPath())
	if proofErr == nil {
		if err = owned(proofPath(), false); err != nil {
			return err
		}
		if string(stored) != string(raw) {
			return errors.New("Scheduler backend initialization evidence conflicts")
		}
	} else if !os.IsNotExist(proofErr) {
		return proofErr
	}
	if anchorErr == nil {
		if err = owned(proofAnchor(), false); err != nil {
			return err
		}
		if proofErr != nil {
			return errors.New("Scheduler backend initialized proof is missing")
		}
		return finishProofInitialization()
	}
	if !os.IsNotExist(anchorErr) {
		return anchorErr
	}
	// Finish storing the exact original evidence even if an older helper has
	// since changed registrations, or the daemon/boot has changed. This grants
	// no publication/readiness authority. Normal reconciliation below preserves
	// obligations, rebases mutations, and handles the pinned old domain.
	if os.IsNotExist(proofErr) {
		if err = atomicFile(proofPath(), raw, 0600); err != nil {
			return err
		}
	}
	if err = atomicFile(proofAnchor(), []byte("1\n"), 0600); err != nil {
		return err
	}
	return finishProofInitialization()
}

func saveBackendProofs(p backendProofs) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(proofAnchor()); err == nil {
		if err = owned(proofAnchor(), false); err != nil {
			return err
		}
		if err = owned(proofPath(), false); err != nil {
			return err
		}
		if _, initErr := os.Lstat(proofInitialization()); !os.IsNotExist(initErr) {
			if err = owned(proofInitialization(), false); err != nil {
				return err
			}
			intent, e := os.ReadFile(proofInitialization())
			if e != nil {
				return e
			}
			current, e := os.ReadFile(proofPath())
			if e != nil {
				return e
			}
			if string(intent) != string(current) {
				return errors.New("Scheduler backend initialization evidence conflicts")
			}
			if err = finishProofInitialization(); err != nil {
				return err
			}
		}
		return atomicFile(proofPath(), b, 0600)
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, path := range []string{proofPath(), proofInitialization()} {
		if _, e := os.Lstat(path); !os.IsNotExist(e) {
			return errors.New("Scheduler backend initialization requires reconciliation")
		}
	}
	if err = atomicFile(proofInitialization(), b, 0600); err != nil {
		return err
	}
	if err = atomicFile(proofPath(), b, 0600); err != nil {
		return err
	}
	if err = atomicFile(proofAnchor(), []byte("1\n"), 0600); err != nil {
		return err
	}
	return finishProofInitialization()
}

func cloneMigration(m legacyMigration) legacyMigration {
	b, _ := json.Marshal(m)
	var out legacyMigration
	_ = json.Unmarshal(b, &out)
	return out
}

func attemptMatchesCore(j journal, branch string, a backendAttempt) bool {
	m, ok := j.Migrations[branch]
	if !ok {
		return false
	}
	if !reflect.DeepEqual(m.Deletions, a.Migration.Deletions) || len(m.Replacements) != len(a.Migration.Replacements) {
		return false
	}
	for id, target := range a.Migration.Replacements {
		e, ok := j.Entries[id]
		core, found := m.Replacements[id]
		if !ok || !found || !e.Enabled || e.Identity != a.Bindings[id] || core.Nonce != target.Nonce || core.Hash != target.Hash || e.MigrationNonce != target.Nonce {
			return false
		}
	}
	return true
}

func prepareBackendAttempt(ctx context.Context, j *journal, p *backendProofs, branch string, host legacyHost) error {
	m := cloneMigration(j.Migrations[branch])
	boot, err := bootID()
	if err != nil {
		return err
	}
	if j.Legacy[branch] != boot {
		return diagnostic("legacy_origin_unproven", "Earlier Scheduler work belongs to a different migration boundary; review its recorded state.")
	}
	previous, hasPrevious := p.Branches[branch]
	if !hasPrevious {
		if m.Host != nil || m.Certified || m.MarkerReceived {
			return diagnostic("legacy_origin_unproven", "Earlier Scheduler backend evidence is inconsistent; review migration before PHP maintenance.")
		}
		for _, target := range m.Replacements {
			if target.Received {
				return diagnostic("legacy_origin_unproven", "Earlier Scheduler receipts do not identify their execution domain; review migration.")
			}
		}
	} else if !previous.Certified {
		if previous.Host != host {
			return diagnostic("backend_identity_changed", "The scheduling engine changed during migration. Earlier work still needs verification.")
		}
		for id := range previous.Migration.Replacements {
			if _, ok := m.Replacements[id]; !ok && !m.Deletions[id] {
				return diagnostic("legacy_origin_unproven", "An earlier Scheduler trigger disappeared from migration history; review its retirement.")
			}
		}
		for id := range previous.Migration.Deletions {
			if !m.Deletions[id] {
				if _, ok := m.Replacements[id]; !ok {
					return diagnostic("legacy_origin_unproven", "An earlier Scheduler deletion disappeared from migration history.")
				}
			}
		}
	}
	a := backendAttempt{ID: uuid.NewString(), Boot: boot, Host: host, Bindings: map[int]string{}, Migration: cloneMigration(m), Previous: m, Preparing: true, PreviousAttempts: append([]backendAttemptEvidence{}, previous.PreviousAttempts...)}
	if hasPrevious {
		a.PreviousAttempts = append(a.PreviousAttempts, backendAttemptEvidence{ID: previous.ID, Host: previous.Host, Migration: previous.Migration, Bindings: previous.Bindings})
	}
	for id, target := range m.Replacements {
		e, ok := j.Entries[id]
		if !ok || !e.Enabled {
			return errors.New("Scheduler registration changed before backend binding")
		}
		if err = committed(ctx, e, true); err != nil {
			return err
		}
		if err = owned(CronPath(id), false); err != nil {
			return err
		}
		raw, err := os.ReadFile(CronPath(id))
		if err != nil || hashBytes(raw) != target.Hash || string(raw) != CronContent(e) {
			return errors.New("Scheduler trigger changed before backend binding")
		}
		a.Bindings[id] = e.Identity
		e.MigrationNonce = uuid.NewString()
		a.Migration.Replacements[id] = legacyTrigger{Nonce: e.MigrationNonce, Hash: hashBytes([]byte(CronContent(e)))}
	}
	for id := range m.Deletions {
		if _, err = os.Lstat(CronPath(id)); !errors.Is(err, os.ErrNotExist) {
			return errors.New("Scheduler deletion is incomplete")
		}
	}
	if len(m.Deletions) > 0 {
		a.Migration.PreviousMarker = m.Marker
		a.Migration.Marker = uuid.NewString()
		a.Migration.MarkerPhase = 1
		a.Migration.MarkerReceived = false
	}
	a.Migration.Host = nil
	a.Migration.Certified = false
	p.Branches[branch] = a
	return saveBackendProofs(*p)
}

// Prepare is authoritative before core projection or publication. Recovery
// accepts only the previous or intended bytes; unrelated helper writes survive.
func publishBackendAttempt(ctx context.Context, j *journal, p *backendProofs, branch string) error {
	a := p.Branches[branch]
	current, ok := j.Migrations[branch]
	if !ok {
		return errors.New("Scheduler migration history disappeared during publication")
	}
	if (!sameMigrationObligations(current, a.Previous) && !sameMigrationObligations(current, a.Migration)) || !preparedRegistrationsMatch(*j, a) {
		// A helper may have added, disabled or recreated a schedule after the
		// prepare commit. Rebase the complete current obligation set, keeping
		// the pinned domain and previous attempt as immutable history.
		for id, target := range current.Replacements {
			e, ok := j.Entries[id]
			if !ok {
				return errors.New("Scheduler registration is missing during publication recovery")
			}
			if err := committed(ctx, e, true); err != nil {
				return err
			}
			if err := owned(CronPath(id), false); err != nil {
				return err
			}
			raw, err := os.ReadFile(CronPath(id))
			if err != nil {
				return err
			}
			if hashBytes(raw) == target.Hash && string(raw) == CronContent(e) {
				continue
			}
			intended, known := a.Migration.Replacements[id]
			predecessor, wasPrepared := a.Previous.Replacements[id]
			if known && wasPrepared && e.Identity == a.Bindings[id] && hashBytes(raw) == predecessor.Hash && target.Nonce == intended.Nonce && target.Hash == intended.Hash {
				// Core projection committed, but this file still has the exact
				// durable predecessor. Finish only that authorized publication.
				if err := publishCron(CronPath(id), []byte(CronContent(e))); err != nil {
					return err
				}
				continue
			}
			if !known || e.Identity != a.Bindings[id] || hashBytes(raw) != intended.Hash {
				return errors.New("Scheduler trigger changed during publication recovery")
			}
			e.MigrationNonce = intended.Nonce
			j.Entries[id] = e
			current.Replacements[id] = intended
		}
		if len(current.Deletions) > 0 {
			for id := range current.Deletions {
				if _, err := os.Lstat(CronPath(id)); !os.IsNotExist(err) {
					return errors.New("Scheduler deletion is incomplete")
				}
			}
			raw, err := os.ReadFile(markerPath(branch))
			if os.IsNotExist(err) {
				// The core committed its first deletion before publishing the
				// harmless root receipt marker. Complete only that recorded write.
				if err = publishCron(markerPath(branch), []byte(markerContent(branch, current))); err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if string(raw) != markerContent(branch, current) {
				if err = owned(markerPath(branch), false); err != nil {
					return err
				}
				if string(raw) != markerContent(branch, a.Previous) && string(raw) != markerContent(branch, a.Migration) {
					return errors.New("Scheduler deletion marker changed during publication recovery")
				}
				if err = publishCron(markerPath(branch), []byte(markerContent(branch, current))); err != nil {
					return err
				}
			}
		}
		j.Migrations[branch] = current
		if err := save(*j); err != nil {
			return err
		}
		if err := prepareBackendAttempt(ctx, j, p, branch, a.Host); err != nil {
			return err
		}
		a = p.Branches[branch]
	}
	for id, target := range a.Migration.Replacements {
		e, ok := j.Entries[id]
		if !ok || e.Identity != a.Bindings[id] || !e.Enabled {
			return errors.New("Scheduler registration changed during backend publication")
		}
		if err := committed(ctx, e, true); err != nil {
			return err
		}
		raw, err := os.ReadFile(CronPath(id))
		if err != nil {
			return err
		}
		if hashBytes(raw) != a.Previous.Replacements[id].Hash && hashBytes(raw) != target.Hash {
			return errors.New("Scheduler trigger changed during backend publication")
		}
		e.MigrationNonce = target.Nonce
		e.Ready = false
		j.Entries[id] = e
	}
	for id := range a.Migration.Deletions {
		if _, err := os.Lstat(CronPath(id)); !errors.Is(err, os.ErrNotExist) {
			return errors.New("Scheduler deletion changed during backend publication")
		}
	}
	if err := installDispatcher(); err != nil {
		return err
	}
	j.Migrations[branch] = cloneMigration(a.Migration)
	if err := save(*j); err != nil {
		return err
	}
	for id := range a.Migration.Replacements {
		if err := publishCron(CronPath(id), []byte(CronContent(j.Entries[id]))); err != nil {
			return err
		}
	}
	if len(a.Migration.Deletions) > 0 {
		if err := publishMigrationMarker(branch, a.Migration); err != nil {
			return err
		}
	}
	a.Preparing = false
	a.PublishedAt = time.Now().Unix()
	p.Branches[branch] = a
	return saveBackendProofs(*p)
}

func sameMigrationObligations(a, b legacyMigration) bool {
	if !reflect.DeepEqual(a.Deletions, b.Deletions) || len(a.Replacements) != len(b.Replacements) {
		return false
	}
	for id, t := range a.Replacements {
		u, ok := b.Replacements[id]
		if !ok || t.Nonce != u.Nonce || t.Hash != u.Hash {
			return false
		}
	}
	return a.Marker == b.Marker && a.MarkerPhase == b.MarkerPhase
}
func preparedRegistrationsMatch(j journal, a backendAttempt) bool {
	for id, identity := range a.Bindings {
		e, ok := j.Entries[id]
		if !ok || !e.Enabled || e.Identity != identity {
			return false
		}
	}
	return true
}

// ReconcileBackendMigrations is a bounded background mutation, separate from
// status/discovery. Ordinary dispatch remains available during this transition.
func ReconcileBackendMigrations(ctx context.Context) error {
	if absent() {
		return nil
	}
	u, err := lock(ctx)
	if err != nil {
		return err
	}
	defer u()
	j, err := read()
	if err != nil {
		return err
	}
	if err = recoverBackendProofInitialization(); err != nil {
		return err
	}
	p, err := readBackendProofs()
	if err != nil {
		return err
	}
	if j.Upgrade != nil || len(j.Holds) > 0 {
		return nil
	}
	var issues []error
	for branch, boot := range j.Legacy {
		current, err := bootID()
		if err != nil {
			return err
		}
		if boot != current {
			err = reconcileLegacy(ctx, &j, branch)
			if err == nil {
				err = save(j)
			}
			recordBackendReconcileIssue(branch, err)
			if err != nil {
				issues = append(issues, err)
			}
			// reconcileLegacy durably updates companion retirement evidence.
			// Do not overwrite it with this worker's earlier snapshot on the
			// next branch's save.
			p, err = readBackendProofs()
			if err != nil {
				return err
			}
			continue
		}
		m, ok := j.Migrations[branch]
		if !ok || m.Host != nil {
			continue
		}
		err = reconcileBackendBranch(ctx, &j, &p, branch)
		recordBackendReconcileIssue(branch, err)
		var d *diagnosticError
		waiting := errors.As(err, &d) && (d.code == "waiting_for_reload" || d.code == "waiting_for_legacy_jobs" || d.code == "panel_proof_required")
		if err != nil && !waiting {
			issues = append(issues, err)
		}
	}
	return errors.Join(issues...)
}

func reconcileBackendBranch(ctx context.Context, j *journal, p *backendProofs, branch string) error {
	h, err := qualifyBackendHost(ctx)
	if err != nil {
		return err
	}

	a, exists := p.Branches[branch]
	if exists && !a.Certified && a.Host != h {
		return diagnostic("backend_identity_changed", "The scheduling engine changed during migration. Earlier work still needs verification.")
	}
	if exists && a.Preparing {
		if err = publishBackendAttempt(ctx, j, p, branch); err != nil {
			return err
		}
		return nil
	}
	if !exists || a.Certified || !attemptMatchesCore(*j, branch, a) {
		if err = prepareBackendAttempt(ctx, j, p, branch, h); err != nil {
			return err
		}
		if err = publishBackendAttempt(ctx, j, p, branch); err != nil {
			return err
		}
		return nil
	}
	readinessErr := reconcileBackendAttempt(ctx, j, p, branch)
	if err = save(*j); err != nil {
		return err
	}
	if readinessErr == nil {
		return nil
	}
	a = p.Branches[branch]
	if a.Preparing {
		if err = publishBackendAttempt(ctx, j, p, branch); err != nil {
			return err
		}
		return nil
	}
	// Inotify may coalesce a same-second event, and Cronie clamps mtimes.
	// Re-publish authorized bytes after another boundary, never authorize by
	// a delay. A genuine receipt remains mandatory.
	if time.Now().Unix()-a.PublishedAt >= 5 {
		for id, target := range a.Migration.Replacements {
			if !target.Received {
				raw, e := os.ReadFile(CronPath(id))
				if e != nil || hashBytes(raw) != target.Hash {
					return errors.New("Scheduler trigger changed before reload retry")
				}
				if e = publishCron(CronPath(id), raw); e != nil {
					return e
				}
			}
		}
		if len(a.Migration.Deletions) > 0 && !a.Migration.MarkerReceived {
			if err = publishMigrationMarker(branch, a.Migration); err != nil {
				return err
			}
		}
		a.PublishedAt = time.Now().Unix()
		p.Branches[branch] = a
		if err = saveBackendProofs(*p); err != nil {
			return err
		}
	}
	return readinessErr
}

func receiveBackendReceipt(ctx context.Context, j journal, p *backendProofs, branch, nonce string, id int) error {
	a := p.Branches[branch]
	if a.Certified {
		return nil
	}
	if a.Preparing || !attemptMatchesCore(j, branch, a) {
		return errors.New("Scheduler backend publication is pending reconciliation")
	}
	h, err := qualifyBackendHost(ctx)
	if err != nil {
		return err
	}
	if h != a.Host {
		return diagnostic("backend_identity_changed", "The scheduling engine changed during migration; earlier work needs verification.")
	}
	if err = cronReceiptOrigin(a.Host); err != nil {
		return err
	}
	if id > 0 {
		t, ok := a.Migration.Replacements[id]
		if !ok || t.Nonce != nonce {
			return errors.New("Stale Scheduler backend receipt")
		}
		raw, e := os.ReadFile(CronPath(id))
		if e != nil || hashBytes(raw) != t.Hash {
			return errors.New("Scheduler backend trigger changed")
		}
		t.Received = true
		a.Migration.Replacements[id] = t
	} else {
		if nonce != a.Migration.Marker {
			return errors.New("Stale Scheduler backend deletion receipt")
		}
		raw, e := os.ReadFile(markerPath(branch))
		if e != nil || string(raw) != markerContent(branch, a.Migration) {
			return errors.New("Scheduler backend marker changed")
		}
		a.Migration.MarkerReceived = true
	}
	p.Branches[branch] = a
	return saveBackendProofs(*p)
}

func reconcileBackendAttempt(ctx context.Context, j *journal, p *backendProofs, branch string) error {
	a := p.Branches[branch]
	if a.Preparing || !attemptMatchesCore(*j, branch, a) {
		return diagnostic("waiting_for_reload", "Scheduler setup is reconciling its tracked launchers. PHP has not been changed.")
	}
	h, err := qualifyBackendHost(ctx)
	if err != nil {
		return err
	}
	if h != a.Host {
		return diagnostic("backend_identity_changed", "The scheduling engine changed during migration. Earlier work still needs verification.")
	}
	for id, t := range a.Migration.Replacements {
		raw, e := os.ReadFile(CronPath(id))
		if e != nil || hashBytes(raw) != t.Hash {
			return errors.New("Scheduler backend trigger changed")
		}
		if !t.Received {
			return diagnostic("waiting_for_reload", "Scheduler setup is waiting for the scheduling engine to load its tracked launchers. PHP has not been changed.")
		}
	}
	for id := range a.Migration.Deletions {
		if _, e := os.Lstat(CronPath(id)); !errors.Is(e, os.ErrNotExist) {
			return errors.New("Scheduler deletion is incomplete")
		}
	}
	if len(a.Migration.Deletions) > 0 {
		if !a.Migration.MarkerReceived {
			return diagnostic("waiting_for_reload", "Scheduler setup is waiting for the scheduling engine to retire earlier triggers. PHP has not been changed.")
		}
		if a.Migration.MarkerPhase == 1 {
			a.Migration.PreviousMarker = a.Migration.Marker
			a.Migration.Marker = uuid.NewString()
			a.Migration.MarkerPhase = 2
			a.Migration.MarkerReceived = false
			// Publish later in the worker. The proof journal commits the intent
			// first so a crash cannot turn receipt A into receipt B.
			a.Preparing = true
			a.Previous = cloneMigration(j.Migrations[branch])
			p.Branches[branch] = a
			if err = saveBackendProofs(*p); err != nil {
				return err
			}
			return diagnostic("waiting_for_reload", "Scheduler setup is confirming retired triggers. PHP has not been changed.")
		}
	}
	if err = verifyPanelBoundary(*j); err != nil {
		return err
	}
	if err = legacyDomainDrained(ctx, a.Host); err != nil {
		return err
	}
	a.Certified = true
	p.Branches[branch] = a
	if err = saveBackendProofs(*p); err != nil {
		return err
	}
	if len(a.Migration.Deletions) > 0 {
		raw, e := os.ReadFile(markerPath(branch))
		if !os.IsNotExist(e) {
			if e != nil {
				return e
			}
			if string(raw) != markerContent(branch, a.Migration) {
				return errors.New("Scheduler deletion receipt file changed after certification")
			}
			if err = os.Remove(markerPath(branch)); err != nil {
				return err
			}
		}
	}
	delete(j.Legacy, branch)
	for id, e := range j.Entries {
		if e.Version == branch {
			e.Ready = true
			j.Entries[id] = e
		}
	}
	return nil
}
