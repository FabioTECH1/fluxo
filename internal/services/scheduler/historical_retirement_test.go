package scheduler

import (
	"encoding/json"
	"strings"
	"testing"

	"fluxo/internal/services/executiondomain"
)

func TestHistoricalActivationPreservesIntentAndRejectsPrematureAuthority(t *testing.T) {
	j := emptyExecutionJournalForSchema(2)
	j.Legacy["8.4"] = "2974294a-9e46-46d1-ac43-f66d7f6b75aa"
	j.Entries[1] = Entry{ID: 1, Enabled: true, Ready: false, MigrationNonce: "old-nonce"}
	j.Migrations["8.4"] = legacyMigration{Replacements: map[int]legacyTrigger{}, Deletions: map[int]bool{}}
	original, _ := json.Marshal(j)
	op := "4406908b-29af-47d7-a714-c52b5557a94b"
	j.Schema = 5
	j.HistoricalRetirement = &historicalRetirement{Contract: executiondomain.SchedulerHistoricalRetirementContract, Operation: op, SourceSHA256: hashBytes(original), Phase: "pending"}
	intended, _ := executiondomain.MergeActivationJSON(original, j)
	if err := ValidateHistoricalExecutionActivationPlan(original, intended, op); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*journal){
		func(j *journal) { e := j.Entries[1]; e.Ready = true; j.Entries[1] = e },
		func(j *journal) { e := j.Entries[1]; e.MigrationNonce = "reset"; j.Entries[1] = e },
		func(j *journal) { j.Legacy = map[string]string{} },
		func(j *journal) { j.HistoricalRetirement.SourceSHA256 = strings.Repeat("b", 64) },
		func(j *journal) { j.HistoricalRetirement.Phase = "complete" },
		func(j *journal) { j.LoaderEpochEstablished = true },
	} {
		var next journal
		_ = json.Unmarshal(intended, &next)
		change(&next)
		raw, _ := json.Marshal(next)
		var object map[string]any
		_ = json.Unmarshal(raw, &object)
		bad, _ := json.Marshal(object)
		if ValidateHistoricalExecutionActivationPlan(original, bad, op) == nil {
			t.Fatal("historical conversion changed readiness, nonce, history or source")
		}
	}
}

func TestHistoricalCompletionRequiresAllBackendAndDeletionObligations(t *testing.T) {
	j := emptyExecutionJournalForSchema(5)
	j.Migrations["8.4"] = legacyMigration{Certified: false, Replacements: map[int]legacyTrigger{}, Deletions: map[int]bool{}}
	p := backendProofs{Branches: map[string]backendAttempt{}}
	if historicalRetirementResolved(j, p) {
		t.Fatal("missing sidecar treated as certification")
	}
	p.Branches["8.4"] = backendAttempt{Certified: true, Migration: legacyMigration{Replacements: map[int]legacyTrigger{}, Deletions: map[int]bool{}}}
	if !historicalRetirementResolved(j, p) {
		t.Fatal("genuine sidecar cannot resolve legacy core without rewriting its Certified history")
	}
	a := p.Branches["8.4"]
	a.Preparing = true
	p.Branches["8.4"] = a
	if historicalRetirementResolved(j, p) {
		t.Fatal("preparing attempt treated as drained")
	}
	a.Preparing = false
	a.Certified = false
	p.Branches["8.4"] = a
	if historicalRetirementResolved(j, p) {
		t.Fatal("unacknowledged attempt treated as drained")
	}
	a.Certified = true
	p.Branches["8.4"] = a
	j.Legacy["8.4"] = "2974294a-9e46-46d1-ac43-f66d7f6b75aa"
	if historicalRetirementResolved(j, p) {
		t.Fatal("remaining historical branch ignored")
	}
}

func TestHistoricalCompletionConsumesImmutableEvidenceWithoutReplayingCore(t *testing.T) {
	j := emptyExecutionJournalForSchema(5)
	j.HistoricalRetirement = &historicalRetirement{Contract: executiondomain.SchedulerHistoricalRetirementContract, Operation: "4406908b-29af-47d7-a714-c52b5557a94b", SourceSHA256: strings.Repeat("a", 64), Phase: "pending"}
	j.Migrations["8.4"] = legacyMigration{Certified: true}
	j.Entries[1] = Entry{ID: 1, Enabled: true, Ready: true}
	core, _ := json.Marshal(j)
	record := historicalCompletion{Contract: j.HistoricalRetirement.Contract, Operation: j.HistoricalRetirement.Operation, SourceSHA256: j.HistoricalRetirement.SourceSHA256, Core: core}
	raw, _ := json.Marshal(record)
	completed := *j.HistoricalRetirement
	completed.Phase = "complete"
	completed.CompletionSHA256 = hashBytes(raw)
	if err := validateHistoricalCompletionBytes(raw, &completed); err != nil {
		t.Fatal(err)
	}
	// A retry consumes the archived proof while retaining a later sweep or
	// acknowledgement. It does not require or restore the archived core bytes.
	j.LastSweep["8.4"] = 123
	j.HistoricalRetirement = &completed
	if err := validateHistoricalCompletionBytes(raw, j.HistoricalRetirement); err != nil {
		t.Fatal("harmless pending history evolution wedged completion", err)
	}
	for _, change := range []func(*historicalCompletion){
		func(r *historicalCompletion) { r.Operation = "f5edfc94-6880-4f03-b3ab-71db97046e65" },
		func(r *historicalCompletion) { r.SourceSHA256 = strings.Repeat("b", 64) },
		func(r *historicalCompletion) {
			var old journal
			_ = json.Unmarshal(r.Core, &old)
			old.Legacy["8.4"] = "2974294a-9e46-46d1-ac43-f66d7f6b75aa"
			r.Core, _ = json.Marshal(old)
		},
		func(r *historicalCompletion) {
			var old journal
			_ = json.Unmarshal(r.Core, &old)
			e := old.Entries[1]
			e.Ready = false
			old.Entries[1] = e
			r.Core, _ = json.Marshal(old)
		},
	} {
		bad := record
		change(&bad)
		encoded, _ := json.Marshal(bad)
		h := completed
		h.CompletionSHA256 = hashBytes(encoded)
		if validateHistoricalCompletionBytes(encoded, &h) == nil {
			t.Fatal("unresolved or foreign completion evidence accepted")
		}
	}
}

func TestHistoricalRetirementContractDoesNotClaimReadiness(t *testing.T) {
	fixture := func() journal {
		j := emptyExecutionJournalForSchema(executiondomain.SchedulerLoaderSchema)
		j.HistoricalRetirement = &historicalRetirement{Contract: executiondomain.SchedulerHistoricalRetirementContract, Operation: "4406908b-29af-47d7-a714-c52b5557a94b", SourceSHA256: strings.Repeat("a", 64), Phase: "pending"}
		j.Legacy["8.4"] = "2974294a-9e46-46d1-ac43-f66d7f6b75aa"
		j.Entries[1] = Entry{ID: 1, Enabled: true, Ready: false}
		j.Migrations["8.4"] = legacyMigration{Certified: false}
		return j
	}
	j := fixture()
	if err := validateHistoricalRetirement(j); err != nil || !historicalRetirementPending(j) {
		t.Fatal("pending retirement refused or interpreted as complete", err)
	}
	for _, change := range []func(*journal){
		func(j *journal) { j.Schema = 4 },
		func(j *journal) { j.HistoricalRetirement.Operation = "" },
		func(j *journal) { j.HistoricalRetirement.SourceSHA256 = "" },
		func(j *journal) { j.HistoricalRetirement.Contract = "unknown" },
		func(j *journal) { j.HistoricalRetirement.Phase = "" },
		func(j *journal) { j.LoaderEpochEstablished = true },
		func(j *journal) { e := j.Entries[1]; e.LoaderNonce = "premature"; j.Entries[1] = e },
		func(j *journal) { j.HistoricalRetirement.Phase = "complete" },
	} {
		j := fixture()
		change(&j)
		if validateHistoricalRetirement(j) == nil {
			t.Fatal("invalid or premature retirement state accepted")
		}
	}
	delete(j.Legacy, "8.4")
	j.HistoricalRetirement.Phase = "complete"
	j.HistoricalRetirement.CompletionSHA256 = strings.Repeat("b", 64)
	if validateHistoricalRetirement(j) == nil {
		t.Fatal("unready migration accepted")
	}
	j.Migrations["8.4"] = legacyMigration{Certified: true}
	if validateHistoricalRetirement(j) == nil {
		t.Fatal("unready registration accepted")
	}
	e := j.Entries[1]
	e.Ready = true
	j.Entries[1] = e
	if err := validateHistoricalRetirement(j); err != nil || historicalRetirementPending(j) {
		t.Fatal("completed evidence refused", err)
	}
}
