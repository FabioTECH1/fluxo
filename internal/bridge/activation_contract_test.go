package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"fluxo/internal/services/executiondomain"
)

func TestActivationReaderContractsStayFrozenAcrossSameSchemaUpgrade(t *testing.T) {
	for format, support := range map[int]int{2: 5, 3: 6, 4: 7} {
		c, err := activationContract(format)
		if err != nil || c.SchedulerSupport != support || c.SystemTaskSupport != 3 {
			t.Fatal(format, c, err)
		}
		if format < 4 && len(c.RoleContracts[executiondomain.SchedulerHistoricalRetirementRole]) != 0 {
			t.Fatal("old archive acquired retirement semantics")
		}
		if format == 4 && !reflect.DeepEqual(c, executiondomain.Capabilities()) {
			t.Fatal("format4 differs from its advertised compiled contract")
		}
	}
	p := generationUpgradeFixture()
	p.Format, p.Kind = 4, activationReader7Upgrade
	source := prototypeFloorFixture()
	c, _ := activationContract(3)
	source.SchedulerSchema, source.SchedulerSupport, source.RoleContracts = 5, 6, c.RoleContracts
	p.SourceFloor, _ = json.Marshal(source)
	p.SourceFloorSHA256 = digest(p.SourceFloor)
	if err := validateActivationPlan(p); err != nil {
		t.Fatal(err)
	}
	if s, task, err := activationTargetSchemas(p); err != nil || s != 5 || task != 3 {
		t.Fatal(s, task, err)
	}
	// A schema match cannot authorize reuse or reinterpret the archived floor.
	p.Format = 3
	if validateActivationPlan(p) == nil {
		t.Fatal("reader7 operation accepted as a historical format3 archive")
	}
}

func TestReader7UpgradeFencesPriorReadersBeforeEveryWriteAndCrashReplay(t *testing.T) {
	files := []executiondomain.ActivationFile{
		{Path: "/var/lib/fluxo-scheduler/dispatcher", Before: "old", Image: true},
		{Path: "/var/lib/fluxo-scheduler/state.json", Before: "scheduler5", Data: []byte("retirement")},
		{Path: "/var/lib/fluxo-system-tasks/state.json", Before: "task3", Data: []byte("refresh")},
		{Path: "/etc/cron.d/fluxo-cron-1", Before: "old trigger", Cron: true, Data: []byte("fresh nonce")},
	}
	for boundary := 1; boundary <= len(files)+1; boundary++ {
		calls, fenced := 0, false
		crash := errors.New("crash after durable publication")
		step := func() error {
			calls++
			if calls == boundary {
				return crash
			}
			return nil
		}
		write := func(executiondomain.ActivationFile) error {
			if !fenced {
				t.Fatal("write preceded reader7 floor")
			}
			return step()
		}
		prepare := func() error { fenced = true; return step() }
		if err := applyReader7UpgradeTransitions(context.Background(), files, write, prepare); !errors.Is(err, crash) {
			t.Fatal(boundary, err)
		}
		if err := applyReader7UpgradeTransitions(context.Background(), files, write, prepare); err != nil {
			t.Fatal(boundary, err)
		}
	}
}

func TestTaskBackendRequalificationKeepsItsFrozenReader6Contract(t *testing.T) {
	p := taskHelperRefreshFixture()
	p.Format, p.Kind = 4, activationTaskBackendRequalification
	p.TaskBackendSHA256, p.SchedulerSourceSHA256 = p.BinarySHA256, p.BinarySHA256
	if err := validateActivationPlan(p); err != nil {
		t.Fatal(err)
	}
	floor, err := activationTargetFloor(p, []byte("immutable plan"), "prepared")
	if err != nil || floor.SchedulerSupport != 6 || len(floor.RoleContracts[executiondomain.SchedulerHistoricalRetirementRole]) != 0 {
		t.Fatal("task repair acquired reader7 authority", floor, err)
	}
	var source executiondomain.ReaderFloor
	_ = json.Unmarshal(p.SourceFloor, &source)
	c, _ := activationContract(4)
	source.SchedulerSupport, source.RoleContracts = 7, c.RoleContracts
	p.SourceFloor, _ = json.Marshal(source)
	p.SourceFloorSHA256 = digest(p.SourceFloor)
	if err := validateActivationPlan(p); err != nil {
		t.Fatal("reader7 requalification failed to preserve its source contract", err)
	}
	floor, err = activationTargetFloor(p, []byte("immutable reader7 plan"), "prepared")
	if err != nil || floor.SchedulerSupport != 7 || len(floor.RoleContracts[executiondomain.SchedulerHistoricalRetirementRole]) != 1 {
		t.Fatal("task requalification downgraded reader7", floor, err)
	}
	delete(source.RoleContracts, executiondomain.SchedulerHistoricalRetirementRole)
	p.SourceFloor, _ = json.Marshal(source)
	p.SourceFloorSHA256 = digest(p.SourceFloor)
	if validateActivationPlan(p) == nil {
		t.Fatal("task requalification discarded a reader7 source role")
	}
}
