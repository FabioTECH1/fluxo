package maintenancetask

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/scheduler"
)

func historicalInstallerFixture(t *testing.T) ([]byte, []byte, []executiondomain.ActivationFile, []scheduler.CronBackendRetirement) {
	t.Helper()
	original, err := os.ReadFile("testdata/v040-cronie-schema1.json")
	if err != nil {
		t.Fatal(err)
	}
	var j journal
	if err = json.Unmarshal(original, &j); err != nil {
		t.Fatal(err)
	}
	old := j.Entries[9]
	emitted, err := os.ReadFile("testdata/v040-cronie-schema1.cron")
	if err != nil {
		t.Fatal(err)
	}
	if string(emitted) != cronContent(old) {
		t.Fatal("v0.4.40 producer trigger meaning changed")
	}
	r := old
	r.Backend.PID, r.Backend.Start, r.Backend.GroupInode = 961231, "900", 65668
	r.Nonce = "383c861b-86fb-405d-9484-09eada7ce31c"
	r.Received = false
	r.RefreshFrom = &helperRefresh{Helper: old.Helper, Nonce: old.Nonce}
	r.Helper = filepath.Join(stateRoot, "helpers", strings.Repeat("c", 64))
	j.Schema = 3
	j.Entries[9] = r
	intended, err := executiondomain.MergeActivationJSON(original, j)
	if err != nil {
		t.Fatal(err)
	}
	return original, intended, []executiondomain.ActivationFile{{Path: stateRoot + "/state.json", Before: digest(original), Data: intended, Mode: 0600}, {Path: cronPath(9), Before: digest(emitted), Data: []byte(cronContent(r)), Cron: true, Mode: 0644}}, []scheduler.CronBackendRetirement{{Saved: old.Backend, Current: r.Backend}}
}

func TestHistoricalInstallerArchivesOldProducerWithoutGrantingAcknowledgement(t *testing.T) {
	old, next, files, proofs := historicalInstallerFixture(t)
	before := string(old)
	if err := ValidateInstallerHistoricalActivationPlan(old, next, strings.Repeat("c", 64), files, proofs); err != nil {
		t.Fatal(err)
	}
	if string(old) != before {
		t.Fatal("source bytes changed")
	}
	// Ordinary conversion keeps its original same-boot refusal.
	if validateActivationBackendContinuity(1, proofs[0].Saved, proofs[0].Current) == nil {
		t.Fatal("ordinary conversion widened")
	}
	var target journal
	_ = json.Unmarshal(next, &target)
	r := target.Entries[9]
	if r.Received || r.RefreshFrom == nil || r.RefreshFrom.Helper == r.Helper || r.RefreshFrom.Nonce == r.Nonce {
		t.Fatal("new helper acquired old authority")
	}
}

func TestHistoricalInstallerRejectsUncertainOrUnrelatedAuthority(t *testing.T) {
	for _, name := range []string{"missing-witness", "extra-witness", "wrong-generation", "configuration", "no-old-ack", "wrong-old-boot", "current-core", "submitted", "started", "failed", "unknown", "fake-new-ack", "changed-command", "missing-trigger", "rewritten-history"} {
		t.Run(name, func(t *testing.T) {
			old, next, files, proofs := historicalInstallerFixture(t)
			var source, target journal
			_ = json.Unmarshal(old, &source)
			_ = json.Unmarshal(next, &target)
			switch name {
			case "missing-witness":
				proofs = nil
			case "extra-witness":
				proofs = append(proofs, proofs[0])
			case "wrong-generation":
				proofs[0].Current.GroupInode++
			case "configuration":
				proofs[0].Current.Configuration = strings.Repeat("d", 64)
			case "no-old-ack":
				r := source.Entries[9]
				r.Received = false
				source.Entries[9] = r
			case "wrong-old-boot":
				r := source.Entries[9]
				r.Boot = "483c861b-86fb-405d-9484-09eada7ce31c"
				source.Entries[9] = r
			case "current-core":
				source.Schema = 3
			case "submitted", "started", "failed", "unknown":
				source.Invocations["retained"] = invocation{Status: name}
				target.Invocations["retained"] = source.Invocations["retained"]
			case "fake-new-ack":
				r := target.Entries[9]
				r.Received = true
				target.Entries[9] = r
			case "changed-command":
				r := target.Entries[9]
				r.Contract.Command = "/bin/true"
				target.Entries[9] = r
			case "missing-trigger":
				files = files[:1]
			case "rewritten-history":
				target.Invocations["invented"] = invocation{Status: "completed"}
			}
			old, _ = json.Marshal(source)
			next, _ = executiondomain.MergeActivationJSON(old, target)
			if ValidateInstallerHistoricalActivationPlan(old, next, strings.Repeat("c", 64), files, proofs) == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestHistoricalInstallerAdmissionPreservesUnconsumedExecutionAuthority(t *testing.T) {
	for _, status := range []string{"submitted", "started", "failed", "unrecognized"} {
		j := journal{Schema: 1, Invocations: map[string]invocation{"retained": {Status: status}}}
		before, _ := json.Marshal(j)
		if installerHistoricalTasksQuiescent(context.Background(), j) == nil {
			t.Fatal("uncertain invocation admitted", status)
		}
		after, _ := json.Marshal(j)
		if string(before) != string(after) {
			t.Fatal("refusal rewrote evidence")
		}
	}
}
