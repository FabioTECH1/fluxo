package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestUpgradeAdmissionPreservesForeignHoldsAndPendingAuthority(t *testing.T) {
	const boot = "601a9ce3-a22b-4a8b-a61b-33ed7493eaaf"
	const previousBoot = "0ff39179-6a5e-45e3-9e39-f9677ed3b704"
	const owner = "ca9d2817-df04-4faa-a680-92323f0c4c1d"
	const foreign = "077d3c76-02d4-4987-93ba-97349eb9f691"
	for _, tc := range []struct {
		name, oldBoot, observedStart, want string
		probeErr                           error
		holds                              map[string]string
		pending, submitted                 bool
	}{
		{name: "empty"},
		{name: "foreign-without-installer", holds: map[string]string{"8.4": foreign}, want: "PHP maintenance"},
		{name: "live-owner", oldBoot: boot, observedStart: "7", holds: map[string]string{"8.4": owner}, want: "Another installer"},
		{name: "dead-owner", oldBoot: boot, probeErr: os.ErrNotExist, holds: map[string]string{"8.4": owner}},
		{name: "reused-pid", oldBoot: boot, observedStart: "8", holds: map[string]string{"8.4": owner}},
		{name: "previous-boot", oldBoot: previousBoot, holds: map[string]string{"8.4": owner}},
		{name: "unknown-process", oldBoot: boot, probeErr: os.ErrPermission, holds: map[string]string{"8.4": owner}, want: "permission"},
		{name: "mixed-owners", oldBoot: boot, probeErr: os.ErrNotExist, holds: map[string]string{"8.4": owner, "8.5": foreign}, want: "PHP maintenance"},
		{name: "racing-submission", pending: true, want: "outstanding Scheduler launch authority"},
		{name: "submitted-unconsumed", pending: true, submitted: true, want: "outstanding Scheduler launch authority"},
		{name: "stale-unconsumed", oldBoot: boot, probeErr: os.ErrNotExist, holds: map[string]string{"8.4": owner}, pending: true, want: "outstanding Scheduler launch authority"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := journal{Holds: tc.holds, Invocations: make(map[string]invocation)}
			if tc.oldBoot != "" {
				j.Upgrade = &upgradeLease{ID: owner, Boot: tc.oldBoot, PID: 42, Start: "7"}
			}
			if tc.pending {
				v := invocation{Unit: "fluxo-scheduler-7f1d3232-8cb0-4f8c-bc2e-2060851edcf0.service", Guarded: true, Submitted: tc.submitted}
				j.Invocations[v.Unit] = v
			}
			before, _ := json.Marshal(j)
			probeCalls := 0
			err := checkUpgradeAdmission(context.Background(), j, boot, func(pid int) (string, error) {
				probeCalls++
				if pid != 42 {
					t.Fatal("probed an unexpected owner")
				}
				return tc.observedStart, tc.probeErr
			})
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("admission error = %v, want %q", err, tc.want)
			}
			if tc.probeErr == os.ErrPermission && !errors.Is(err, os.ErrPermission) {
				t.Fatal("lost the unknown-owner refusal")
			}
			if tc.oldBoot != boot && probeCalls != 0 {
				t.Fatal("another boot's numeric PID was treated as current ownership")
			}
			after, _ := json.Marshal(j)
			if string(before) != string(after) {
				t.Fatal("admission mutated maintenance or launch authority")
			}
		})
	}
}

func TestUpgradeHoldsFenceEveryRegisteredBranchWithoutChangingEvidence(t *testing.T) {
	const old = "a4c978cb-c4c0-48d3-b6ea-5c23dbb0d2e4"
	const next = "60631b94-b7d9-472b-90d0-b1e1f5951161"
	j := emptyExecutionJournalForSchema(2)
	j.Entries[1] = Entry{ID: 1, Version: "8.4", Enabled: true, Ready: false, MigrationNonce: "retained-receipt"}
	j.Entries[2] = Entry{ID: 2, Version: "8.5", Enabled: false, Identity: "retained-identity"}
	j.Holds["8.3"] = old
	j.Legacy["8.4"] = "retained-boot"
	v := invocation{Unit: "fluxo-scheduler-7f1d3232-8cb0-4f8c-bc2e-2060851edcf0.service", Started: true, Guarded: true}
	j.Invocations[v.Unit] = v
	withoutHolds := func(j journal) string {
		j.Holds = nil
		raw, _ := json.Marshal(j)
		return string(raw)
	}
	before := withoutHolds(j)
	setUpgradeHolds(&j, next)
	for _, e := range j.Entries {
		if j.Holds[e.Version] != next {
			t.Fatalf("legacy dispatcher branch %s is not fenced", e.Version)
		}
	}
	if _, ok := j.Holds["8.3"]; ok {
		t.Fatal("retained a dead installer's stale branch owner")
	}
	if withoutHolds(j) != before {
		t.Fatal("fence publication changed registration, receipt, legacy or invocation evidence")
	}
	for branch, owner := range j.Holds {
		if !branchRE.MatchString(branch) || owner != next {
			t.Fatal("fence is incompatible with the historical hold wire contract")
		}
	}
}

func TestUpgradeHoldsFenceEmptyRegistrationAndReleaseOnlyTheirOwner(t *testing.T) {
	const owner = "4c34a584-1fbe-42df-8ffa-9e624aa8e09a"
	const foreign = "53cacbab-ae3a-45c4-89ee-3c486a5e67a6"
	j := journal{}
	setUpgradeHolds(&j, owner)
	if len(j.Holds) == 0 {
		t.Fatal("an old panel could create its first registration during upgrade")
	}
	for branch, value := range j.Holds {
		if !branchRE.MatchString(branch) || value != owner {
			t.Fatal("invalid denial-only registration fence")
		}
	}
	j.Holds["8.5"] = foreign
	releaseUpgradeHolds(&j, owner)
	if len(j.Holds) != 1 || j.Holds["8.5"] != foreign {
		t.Fatal("installer release removed another maintenance owner's hold")
	}
}
