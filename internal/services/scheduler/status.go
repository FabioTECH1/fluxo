package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type BranchObservation struct {
	Version string `json:"version"`
	State   string `json:"state"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}
type Status struct {
	Backend    BackendObservation  `json:"backend"`
	Branches   []BranchObservation `json:"branches"`
	StateError string              `json:"state_error,omitempty"`
}

// Advisory observations only. Authorization always re-reads both journals and
// the live backend; a cached issue can never certify a branch.
var reconcileIssues = struct {
	sync.Mutex
	branches map[string]BranchObservation
}{branches: map[string]BranchObservation{}}

func recordBackendReconcileIssue(branch string, err error) {
	reconcileIssues.Lock()
	defer reconcileIssues.Unlock()
	if err == nil {
		delete(reconcileIssues.branches, branch)
		return
	}
	issue := BranchObservation{State: "blocked", Code: "reconciliation_failed", Message: err.Error()}
	var d *diagnosticError
	if errors.As(err, &d) {
		issue.Code = d.code
		if d.code == "waiting_for_reload" || d.code == "waiting_for_legacy_jobs" || d.code == "panel_proof_required" {
			issue.State = "pending"
		}
	}
	reconcileIssues.branches[branch] = issue
}

func backendReconcileIssue(branch string) (BranchObservation, bool) {
	reconcileIssues.Lock()
	defer reconcileIssues.Unlock()
	issue, ok := reconcileIssues.branches[branch]
	return issue, ok
}

func SchedulerStatus(ctx context.Context) Status {
	s := Status{Backend: BackendStatus(ctx), Branches: []BranchObservation{}}
	if absent() {
		return s
	}
	u, err := lock(ctx)
	if err != nil {
		s.StateError = err.Error()
		return s
	}
	defer u()
	j, err := read()
	if err != nil {
		s.StateError = err.Error()
		return s
	}
	p, err := readBackendProofs()
	if err != nil {
		s.StateError = err.Error()
		return s
	}
	s.Branches = branchObservations(j, p, s.Backend)
	return s
}

func branchObservations(j journal, p backendProofs, backend BackendObservation) []BranchObservation {
	result := []BranchObservation{}
	branches := map[string]bool{}
	for branch := range j.Legacy {
		branches[branch] = true
	}
	for branch := range j.Holds {
		branches[branch] = true
	}
	for _, e := range j.Entries {
		if e.Enabled {
			branches[e.Version] = true
		}
	}
	for branch, a := range p.Branches {
		if !a.Certified {
			branches[branch] = true
		}
	}
	ordered := make([]string, 0, len(branches))
	for branch := range branches {
		ordered = append(ordered, branch)
	}
	sort.Strings(ordered)
	for _, branch := range ordered {
		b := BranchObservation{Version: branch, State: "ready"}
		if j.Legacy[branch] != "" {
			b.State = "pending"
			b.Code = "waiting_for_reload"
			b.Message = "Scheduler setup is waiting for its tracked launchers to be verified."
		}
		if backend.State != "active" || backend.Qualification != "qualified" {
			b.State = "blocked"
			b.Code = backend.Code
			b.Message = backend.Message
		} else if a, ok := p.Branches[branch]; ok && !a.Certified {
			if j.Legacy[branch] == "" {
				b.State, b.Code = "blocked", "legacy_origin_unproven"
				b.Message = "Scheduler backend history remains unresolved; review its migration state."
			} else if backend.Identity == nil || *backend.Identity != a.Host {
				b.State = "blocked"
				b.Code = "backend_identity_changed"
				b.Message = "The scheduling engine changed during migration. Earlier work still needs verification."
			} else if a.Preparing {
				b.Message = "Scheduler setup is publishing its tracked launchers."
			} else {
				waiting := false
				for _, t := range a.Migration.Replacements {
					waiting = waiting || !t.Received
				}
				waiting = waiting || (len(a.Migration.Deletions) > 0 && (!a.Migration.MarkerReceived || a.Migration.MarkerPhase == 1))
				if !waiting {
					b.Code = "waiting_for_legacy_jobs"
					b.Message = "Scheduler setup is verifying earlier work has finished."
				}
			}
		} else if m, ok := j.Migrations[branch]; ok && m.Host != nil && j.Legacy[branch] != "" {
			if backend.Identity == nil || *backend.Identity != *m.Host {
				b.State = "blocked"
				b.Code = "backend_identity_changed"
				b.Message = "The scheduling engine changed during migration. Earlier work still needs verification."
			}
		} else if m, ok := j.Migrations[branch]; ok && m.Host == nil && j.Legacy[branch] != "" {
			b.Code = "waiting_for_backend_binding"
			b.Message = "Scheduler setup is waiting for its installed engine to be verified."
			contradictory := m.Certified || m.MarkerReceived
			for _, t := range m.Replacements {
				contradictory = contradictory || t.Received
			}
			if contradictory {
				b.State = "blocked"
				b.Code = "legacy_origin_unproven"
				b.Message = "Earlier Scheduler evidence does not identify its execution domain; review migration."
			}
		}
		if b.State == "pending" && j.PanelProof == nil {
			b.Code = "panel_proof_required"
			b.Message = "Earlier manual Scheduler runs need verification; complete the Fluxo installer before PHP maintenance."
		}
		if issue, ok := backendReconcileIssue(branch); ok && b.State != "blocked" && j.Legacy[branch] != "" {
			b.State, b.Code, b.Message = issue.State, issue.Code, issue.Message
		}
		if j.Holds[branch] != "" {
			b.State = "paused"
			b.Code = "maintenance_active"
			b.Message = "Scheduler is paused for PHP maintenance."
		}
		result = append(result, b)
	}
	return result
}

// ConfigurationState is not execution health. An absent trigger stays absent;
// a custom/changed file is never rewritten merely to make its badge green.
func ConfigurationState(ctx context.Context, id int, legacy string) string {
	path := CronPath(id)
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return "missing"
	} else if err != nil {
		return "unknown"
	}
	if err := owned(path, false); err != nil {
		return "modified"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "unknown"
	}
	if string(b) == legacy {
		return "configured"
	}
	if absent() {
		return "modified"
	}
	u, err := lock(ctx)
	if err != nil {
		return "unknown"
	}
	defer u()
	j, err := read()
	if err != nil {
		return "unknown"
	}
	if e, ok := j.Entries[id]; ok && e.Enabled && string(b) == CronContent(e) {
		return "configured"
	}
	return "modified"
}

// DiagnosticBundle is allowlisted and read-only. In particular it never emits
// application commands, credentials, arbitrary unit environments or nonces.
func DiagnosticBundle(ctx context.Context) map[string]any {
	status := SchedulerStatus(ctx)
	boot, _ := bootID()
	result := map[string]any{"format": 1, "captured_at": time.Now().Unix(), "boot": boot, "status": status, "registrations": []any{}, "migrations": []any{}}
	result["current_host"] = currentHostEvidence(ctx, status.Backend)
	if absent() {
		result["journal_state"] = "absent"
		return result
	}
	u, err := lock(ctx)
	if err != nil {
		result["journal_error"] = err.Error()
		return result
	}
	defer u()
	j, err := read()
	if err != nil {
		result["journal_error"] = err.Error()
		return result
	}
	p, err := readBackendProofs()
	if err != nil {
		result["proof_error"] = err.Error()
		return result
	}
	entries := []any{}
	for id, e := range j.Entries {
		_, qualified := Qualify(id, e.SiteID, e.Directory, "* * * * *", e.Command, "fluxo")
		item := map[string]any{"id": id, "branch": e.Version, "identity": e.Identity, "enabled": e.Enabled, "ready": e.Ready, "command_qualified": qualified, "database_path_matches": filepath.Clean(e.Database) == filepath.Clean(databasePath)}
		if err := committed(ctx, e); err == nil {
			item["committed_database_binding"] = "matched"
		} else if errors.Is(err, sql.ErrNoRows) {
			item["committed_database_binding"] = "mismatch"
		} else {
			item["committed_database_binding"] = unknownEvidence(err)
		}
		item["trigger_metadata"] = fileEvidence(CronPath(id))
		if st, err := os.Lstat(CronPath(id)); err == nil {
			item["trigger_mode"] = st.Mode().String()
			if owned(CronPath(id), false) == nil {
				b, err := os.ReadFile(CronPath(id))
				if err == nil {
					item["trigger_digest"] = hashBytes(b)
					item["generated_content_matches"] = string(b) == CronContent(e)
				} else {
					item["generated_content_matches"] = unknownEvidence(err)
				}
			} else {
				item["generated_content_matches"] = unknownEvidence(errors.New("Trigger ownership or file type is unsafe"))
			}
		} else if errors.Is(err, os.ErrNotExist) {
			item["trigger_state"] = "missing"
		} else {
			item["trigger_state"] = "unknown"
			item["trigger_error"] = err.Error()
		}
		entries = append(entries, item)
	}
	migrations := []any{}
	for branch, m := range j.Migrations {
		targets := []any{}
		for id, t := range m.Replacements {
			targets = append(targets, map[string]any{"id": id, "received": t.Received})
		}
		item := map[string]any{"branch": branch, "legacy_boot": j.Legacy[branch], "host_null": m.Host == nil, "certified": m.Certified, "replacements": targets, "deletions": m.Deletions, "marker_phase": m.MarkerPhase, "marker_received": m.MarkerReceived}
		if m.Host != nil {
			item["host"] = m.Host
		}
		if a, ok := p.Branches[branch]; ok {
			item["backend_host"] = a.Host
			item["backend_attempt"] = a.ID
			item["backend_preparing"] = a.Preparing
			item["backend_certified"] = a.Certified
			authoritative := []any{}
			for id, t := range a.Migration.Replacements {
				authoritative = append(authoritative, map[string]any{"id": id, "received": t.Received})
			}
			item["backend_replacements"] = authoritative
			item["backend_deletions"] = a.Migration.Deletions
			item["backend_marker_phase"] = a.Migration.MarkerPhase
			item["backend_marker_received"] = a.Migration.MarkerReceived
		}
		migrations = append(migrations, item)
	}
	result["schema"] = j.Schema
	result["registrations"] = entries
	result["migrations"] = migrations
	result["holds_present"] = len(j.Holds) > 0
	result["outstanding_invocations"] = len(j.Invocations)
	recoveryBranches := map[string]map[string]any{}
	for _, e := range j.Entries {
		recoveryBranches[e.Version] = map[string]any{"branch": e.Version, "hold_present": j.Holds[e.Version] != "", "invocations_total": 0, "submission_unconfirmed": 0, "launch_authorized": 0}
	}
	for branch := range j.Holds {
		if recoveryBranches[branch] == nil {
			recoveryBranches[branch] = map[string]any{"branch": branch, "hold_present": true, "invocations_total": 0, "submission_unconfirmed": 0, "launch_authorized": 0}
		}
	}
	for _, inv := range j.Invocations {
		item := recoveryBranches[inv.Entry.Version]
		if item == nil {
			item = map[string]any{"branch": inv.Entry.Version, "hold_present": j.Holds[inv.Entry.Version] != "", "invocations_total": 0, "submission_unconfirmed": 0, "launch_authorized": 0}
			recoveryBranches[inv.Entry.Version] = item
		}
		item["invocations_total"] = item["invocations_total"].(int) + 1
		if !inv.Submitted {
			item["submission_unconfirmed"] = item["submission_unconfirmed"].(int) + 1
		}
		// Started is durable launch authorization, not observed application start.
		if inv.Started {
			item["launch_authorized"] = item["launch_authorized"].(int) + 1
		}
	}
	result["branch_recovery"] = recoveryBranches
	result["upgrade_active"] = j.Upgrade != nil
	result["panel_proof_present"] = j.PanelProof != nil
	if j.PanelProof != nil {
		result["panel_proof_boot_matches"] = j.PanelProof.Boot == boot
		exe, _ := os.Executable()
		hash, err := fileDigest(exe)
		if err == nil {
			result["panel_proof_binary_matches"] = hash == j.PanelProof.Binary
		} else {
			result["panel_proof_binary_matches"] = unknownEvidence(err)
		}
	}
	return result
}

func StartBackendReconciler(ctx context.Context) {
	go func() {
		delay := time.Duration(0)
		for {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			attempt, cancel := context.WithTimeout(ctx, 20*time.Second)
			err := ReconcileBackendMigrations(attempt)
			cancel()
			if err == nil {
				delay = 30 * time.Second
			} else {
				if delay < 30*time.Second {
					delay = 30 * time.Second
				} else {
					delay *= 2
				}
				if delay > 5*time.Minute {
					delay = 5 * time.Minute
				}
			}
		}
	}()
}
