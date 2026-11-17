package bridge

import (
	"encoding/json"
	"strings"
	"testing"

	"fluxo/internal/services/executiondomain"
)

func prototypeFloorFixture() executiondomain.ReaderFloor {
	c, _ := activationContract(2)
	roles := c.RoleContracts
	delete(roles, "scheduler-loader-reader")
	roles["scheduler-execute"] = []string{executiondomain.CgroupExitV1, executiondomain.PersistentV1}
	return executiondomain.ReaderFloor{Format: 1, Phase: "committed", SchedulerSchema: 4, SystemTaskSchema: 3, SchedulerSupport: 5, SystemTaskSupport: 3, Adapters: executiondomain.Capabilities().Adapters, RoleContracts: roles, ActivationID: "2af605c7-82ca-4245-9fc5-1240cae7d8ab", InstallationID: strings.Repeat("a", 32), BinarySHA256: strings.Repeat("e", 64), PlanSHA256: strings.Repeat("f", 64)}
}
func generationUpgradeFixture() executionActivation {
	p := activationPlanFixture()
	p.Format, p.Kind, p.TargetSchedulerSchema, p.TargetSystemTaskSchema = 3, activationReaderUpgrade, 5, 3
	p.SourceFloor, _ = json.Marshal(prototypeFloorFixture())
	p.SourceFloorSHA256 = digest(p.SourceFloor)
	p.Files = append(p.Files, executiondomain.ActivationFile{Path: "/var/lib/fluxo-scheduler/helpers/" + p.BinarySHA256, Before: "absent", Image: true, Mode: 0700})
	return p
}
func TestGenerationUpgradeDoesNotReinterpretHistoricalArchive(t *testing.T) {
	old := activationPlanFixture()
	if s, task, e := activationTargetSchemas(old); e != nil || s != 4 || task != 3 {
		t.Fatal(s, task, e)
	}
	p := generationUpgradeFixture()
	if err := validateActivationPlan(p); err != nil {
		t.Fatal(err)
	}
	if s, task, e := activationTargetSchemas(p); e != nil || s != 5 || task != 3 {
		t.Fatal(s, task, e)
	}
	for name, change := range map[string]func(*executionActivation){
		"missing source":            func(p *executionActivation) { p.SourceFloor = nil },
		"source hash changed":       func(p *executionActivation) { p.SourceFloor = append(p.SourceFloor, ' ') },
		"fresh overwrite":           func(p *executionActivation) { p.Kind = activationFresh },
		"unknown generation":        func(p *executionActivation) { p.TargetSchedulerSchema = 6 },
		"old archive new authority": func(p *executionActivation) { p.Format = 2 },
		"missing immutable helper":  func(p *executionActivation) { p.Files = p.Files[:len(p.Files)-1] },
		"other installation":        func(p *executionActivation) { p.InstallationID = strings.Repeat("b", 32) },
	} {
		t.Run(name, func(t *testing.T) {
			p := generationUpgradeFixture()
			change(&p)
			if validateActivationPlan(p) == nil {
				t.Fatal("invalid immutable generation transition accepted")
			}
		})
	}
}
func TestPrototypeSourceRejectsUnreviewedExecutionAuthority(t *testing.T) {
	for _, role := range []string{"scheduler-loader-reader", "scheduler-execute"} {
		f := prototypeFloorFixture()
		label := executiondomain.SchedulerLoaderReaderContract
		if role == "scheduler-execute" {
			label = executiondomain.SchedulerBootstrapContract
		}
		f.RoleContracts[role] = append(f.RoleContracts[role], label)
		if validatePrototypeSourceFloor(f) == nil {
			t.Fatal("prototype claimed new generation authority", role)
		}
	}
}

func TestGenerationFloorReplacementUsesExactArchivedBytes(t *testing.T) {
	p := generationUpgradeFixture()
	source := prototypeFloorFixture()
	altered := prototypeFloorFixture()
	delete(altered.RoleContracts, "system-task-profile-reader")
	alteredRaw, _ := json.Marshal(altered)
	planBytes, _ := json.Marshal(p)
	for _, tc := range []struct {
		name     string
		raw      []byte
		floor    *executiondomain.ReaderFloor
		accepted bool
	}{
		{"exact source", p.SourceFloor, &source, true},
		{"same scalars changed role contract", alteredRaw, &altered, false},
		{"exact semantic floor changed bytes", append(p.SourceFloor, ' '), &source, false},
		{"lost source", nil, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			err := validateActivationFloorTransitionWithRead("/var/lib/fluxo", p, planBytes, tc.floor, func(path string, limit int64) ([]byte, error) {
				reads++
				if path != "/var/lib/fluxo/execution-readers.json" || limit != 64<<10 {
					t.Fatal("wrong protected read scope")
				}
				return tc.raw, nil
			})
			if (err == nil) != tc.accepted {
				t.Fatal("floor replacement admission", err)
			}
			if tc.floor != nil && reads != 1 {
				t.Fatal("source checksum not actually re-read")
			}
		})
	}
	fresh := p
	fresh.Kind = activationFresh
	fresh.SourceFloor = nil
	fresh.SourceFloorSHA256 = ""
	if validateActivationFloorTransitionWithRead("/var/lib/fluxo", fresh, planBytes, &source, nil) == nil {
		t.Fatal("fresh activation replaced committed source floor")
	}
}
