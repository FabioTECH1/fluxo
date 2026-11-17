package maintenancetask

import (
	"encoding/json"
	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/scheduler"
	"github.com/google/uuid"
	"path/filepath"
	"strings"
	"testing"
)

func TestActivationRefreshPreservesRegistrationIntent(t *testing.T) {
	c, ok := Qualify(9, 0, "0 0 * * 0", PackageCleanupCommand, "root")
	if !ok {
		t.Fatal("fixture contract")
	}
	identity, nonce, boot := uuid.NewString(), uuid.NewString(), uuid.NewString()
	r := registration{Contract: c, Database: "/var/lib/fluxo/fluxo.db", Identity: identity, Nonce: nonce, Helper: filepath.Join(stateRoot, "helpers", strings.Repeat("a", 64)), Enabled: true, Published: true, Received: true, Boot: boot, Predecessor: LegacyContent(c)}
	before := journal{1, map[int]registration{9: r}, map[string]invocation{}}
	raw, _ := json.Marshal(before)
	r.RefreshFrom = &helperRefresh{Helper: r.Helper, Nonce: r.Nonce}
	r.Helper = filepath.Join(stateRoot, "helpers", strings.Repeat("b", 64))
	r.Nonce = uuid.NewString()
	r.Received = false
	r.Backend = scheduler.CronBackendIdentity{Boot: boot, PID: 40, Start: "400", GroupInode: 1}
	after := journal{executiondomain.CurrentSystemTaskSchema, map[int]registration{9: r}, map[string]invocation{}}
	converted, _ := executiondomain.MergeActivationJSON(raw, after)
	changes := []executiondomain.ActivationFile{{Path: cronPath(9), Data: []byte(cronContent(r)), Cron: true}}
	if err := ValidateExecutionActivationPlan(raw, converted, strings.Repeat("b", 64), changes); err != nil {
		t.Fatal(err)
	}
	changed := r
	changed.Contract.Command = "/bin/true"
	after.Entries[9] = changed
	invalid, _ := executiondomain.MergeActivationJSON(raw, after)
	if err := ValidateExecutionActivationPlan(raw, invalid, strings.Repeat("b", 64), changes); err == nil {
		t.Fatal("helper refresh changed application intent")
	}
	if err := ValidateExecutionActivationPlan(raw, converted, strings.Repeat("b", 64), nil); err == nil {
		t.Fatal("format committed without refreshed trigger publication")
	}
}

func TestCleanAbsencePlanNeverInventsTaskOrCronAuthority(t *testing.T) {
	changes := executionCreationPlan("1af605c7-82ca-4245-9fc5-1240cae7d8ab")
	var core []byte
	for _, change := range changes {
		if change.Path == stateRoot+"/state.json" {
			core = change.Data
		}
		if change.Cron {
			t.Fatal("clean format initialization touched plain legacy cron")
		}
	}
	if err := ValidateExecutionActivationPlan(nil, core, strings.Repeat("a", 64), changes); err != nil {
		t.Fatal(err)
	}
	if err := ValidateExecutionActivationPlan(nil, core, strings.Repeat("a", 64), append(changes, executiondomain.ActivationFile{Path: "/etc/cron.d/fluxo-cron-1", Cron: true})); err == nil {
		t.Fatal("absence of task journal authorized replacing legacy cron")
	}
}
