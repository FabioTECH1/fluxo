package executiondomain

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPersistentAdapterIsExplicitAndDoesNotShareCallerPrivileges(t *testing.T) {
	if _, err := LegacyAdapter(PersistentV1); err == nil {
		t.Fatal("old record acquired persistent meaning")
	}
	for _, tc := range []struct {
		schema, first int
		id            AdapterID
		valid         bool
	}{{2, 3, "", true}, {3, 3, CgroupExitV1, true}, {3, 3, PersistentV1, false}, {4, 3, PersistentV1, true}, {1, 2, "", true}, {2, 2, PersistentV1, false}, {3, 2, PersistentV1, true}, {4, 3, Persistent249CandidateV1, false}, {4, 3, "", false}} {
		if err := ValidateAdapter(tc.schema, tc.first, tc.id); (err == nil) != tc.valid {
			t.Fatal(tc, err)
		}
	}
	args, err := SubmissionArguments(PersistentV1, "own.service")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, "\n")
	for _, forbidden := range []string{"--collect", "ExitType=", "User=", "Group=", "CapabilityBoundingSet=", "WorkingDirectory=", "RootDirectory="} {
		if strings.Contains(joined, forbidden) {
			t.Fatal("lifetime API widened caller-owned privilege policy", forbidden)
		}
	}
	old, _ := SubmissionArguments("", "own.service")
	if !strings.Contains(strings.Join(old, " "), "--collect") || !strings.Contains(strings.Join(old, " "), "ExitType=cgroup") {
		t.Fatal("legacy lifetime interpretation changed")
	}
}

func TestPersistentQuiescenceNeverSubstitutesForHierarchyProof(t *testing.T) {
	p := map[string]string{"LoadState": "loaded", "MainPID": "0", "ControlPID": "0", "ActiveState": "active", "SubState": "exited"}
	if !UnitQuiescent(PersistentV1, p) || UnitQuiescent(CgroupExitV1, p) {
		t.Fatal("retained empty-state metadata interpretation changed")
	}
	populated := errors.New("hierarchy still populated")
	if err := StopNaturallyEmpty(context.Background(), PersistentV1, "fluxo-scheduler-1af605c7-82ca-4245-9fc5-1240cae7d8ab.service", func() error { return populated }); !errors.Is(err, populated) {
		t.Fatal("service state bypassed natural descendant proof", err)
	}

	if err := StopNaturallyEmpty(context.Background(), CgroupExitV1, "owned.service", func() error { return populated }); !errors.Is(err, populated) {
		t.Fatal("legacy no-op retirement bypassed the required natural-empty proof", err)
	}
	p["ControlPID"] = "42"
	if UnitQuiescent(PersistentV1, p) {
		t.Fatal("control process ignored")
	}
}

func TestPersistentRetirementHandlesCollectedUnitOnlyWithFreshEmptyProof(t *testing.T) {
	stopErr := errors.New("unit not loaded")
	populated := errors.New("descendant hierarchy is populated")
	for _, tc := range []struct {
		name, loadState           string
		inspectErr, finalProofErr error
		wantSuccess               bool
	}{
		{name: "collected", loadState: "not-found\n", wantSuccess: true},
		{name: "still loaded", loadState: "loaded"},
		{name: "unknown result", loadState: ""},
		{name: "inspection failed", loadState: "not-found", inspectErr: errors.New("PID1 unavailable")},
		{name: "hierarchy still populated", loadState: "not-found", finalProofErr: populated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proofs, commands := 0, 0
			unit := "fluxo-system-task-1af605c7-82ca-4245-9fc5-1240cae7d8ab.service"
			err := stopNaturallyEmpty(context.Background(), PersistentV1, unit, func() error {
				proofs++
				if proofs > 1 {
					return tc.finalProofErr
				}
				return nil
			}, func(_ context.Context, _ time.Duration, executable string, args ...string) (string, error) {
				commands++
				if executable != "/usr/bin/systemctl" {
					t.Fatal("unexpected executable", executable)
				}
				switch commands {
				case 1:
					if strings.Join(args, " ") != "stop "+unit {
						t.Fatal("unexpected retirement", args)
					}
					return "", stopErr
				case 2:
					if strings.Join(args, " ") != "show "+unit+" --property=LoadState --value" {
						t.Fatal("unexpected absence proof", args)
					}
					return tc.loadState, tc.inspectErr
				default:
					t.Fatal("collected unit acquired further command authority", args)
					return "", errors.New("unexpected command")
				}
			})
			if (err == nil) != tc.wantSuccess || commands != 2 {
				t.Fatal("incorrect collected-unit retirement", err, commands)
			}
			if tc.loadState == "not-found\n" || tc.finalProofErr != nil {
				if proofs != 2 {
					t.Fatal("absence replaced fresh hierarchy proof", proofs)
				}
			} else if proofs != 1 {
				t.Fatal("untrusted absence result accepted", proofs)
			}
			if tc.finalProofErr != nil && !errors.Is(err, tc.finalProofErr) {
				t.Fatal("hierarchy proof failure lost", err)
			}
		})
	}
}

func TestCurrentCoreWriteNeedsBothActualCores(t *testing.T) {
	f := currentFloorFixture()
	r := CompatibilityReport{Compatible: true, Floor: &f, Journals: []JournalCompatibility{{"scheduler", CurrentSchedulerSchema}, {"system-task", CurrentSystemTaskSchema}}}
	if _, err := validateCurrentWriteReport(r); err != nil {
		t.Fatal(err)
	}
	for _, journals := range [][]JournalCompatibility{nil, {{"scheduler", 4}}, {{"scheduler", 3}, {"system-task", 3}}, {{"scheduler", 4}, {"system-task", 2}}} {
		r.Journals = journals
		if _, err := validateCurrentWriteReport(r); err == nil {
			t.Fatal("missing/prototype core granted current identity writes")
		}
	}
}
func currentFloorFixture() ReaderFloor {
	return ReaderFloor{Format: 1, Phase: "committed", SchedulerSupport: SchedulerCompiledReaderSupport, SystemTaskSupport: SystemTaskReaderSupport, SchedulerSchema: CurrentSchedulerSchema, SystemTaskSchema: CurrentSystemTaskSchema, Adapters: Capabilities().Adapters, RoleContracts: Capabilities().RoleContracts, ActivationID: "1af605c7-82ca-4245-9fc5-1240cae7d8ab", InstallationID: strings.Repeat("a", 32), BinarySHA256: strings.Repeat("b", 64), PlanSHA256: strings.Repeat("c", 64)}
}

func TestOperationAnchorOrPartialInitializationBlocksBeforeCoreExists(t *testing.T) {
	for _, path := range []string{"/var/lib/fluxo-scheduler.initialized", "/var/lib/fluxo-system-tasks.initialized", "/var/lib/fluxo-scheduler/gate.lock", "/var/lib/fluxo-system-tasks.lock", "/var/lib/fluxo-scheduler", "/var/lib/fluxo-system-tasks"} {
		for _, raw := range []string{"activation:1af605c7-82ca-4245-9fc5-1240cae7d8ab\n", "1\n", ""} {
			report, err := inspectCompatibility("/var/lib/fluxo", func(current string, _ int64) ([]byte, error) {
				if current == path {
					return []byte(raw), nil
				}
				return nil, os.ErrNotExist
			})
			if err == nil || report.Compatible {
				t.Fatal("partial/ready-lost/coreless initialization permitted ordinary writes", path, raw)
			}
		}
	}
}

func TestPersistentIdentityOnlyAcceptedWithCurrentFloorAndCore(t *testing.T) {
	f := currentFloorFixture()
	floor, _ := json.Marshal(f)
	for _, prototype := range []bool{false, true} {
		schema := CurrentSchedulerSchema
		if prototype {
			schema = 3
		}
		report, err := inspectCompatibility("/var/lib/fluxo", func(path string, _ int64) ([]byte, error) {
			switch path {
			case "/var/lib/fluxo/execution-readers.json":
				return floor, nil
			case "/var/lib/fluxo-scheduler/state.json":
				return []byte(`{"schema":` + string(rune('0'+schema)) + `,"loader_epochs_established":false,"entries":{},"invocations":{"v":{"adapter":"systemd-persistent-v1"}}}`), nil
			case "/var/lib/fluxo-system-tasks/state.json":
				return []byte(`{"schema":3,"entries":{},"invocations":{}}`), nil
			}
			return nil, os.ErrNotExist
		})
		if prototype && err == nil {
			t.Fatal("new identity accepted in prototype core")
		}
		if !prototype && (err != nil || !report.Compatible) {
			t.Fatal(report, err)
		}
	}
}

func TestBackendWriterRequiresExactRoleAndAllCurrentReaderSemantics(t *testing.T) {
	floor := currentFloorFixture()
	contract := "ubuntu-jammy-cron-v1"
	if err := validateBackendWriteContract(floor, contract, Capabilities()); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []func(*ReaderCapabilities){
		func(c *ReaderCapabilities) { delete(c.RoleContracts, "scheduler-backend-reader") },
		func(c *ReaderCapabilities) {
			c.RoleContracts["scheduler-backend-reader"] = []string{"ubuntu-noble-cron-v1"}
		},
		func(c *ReaderCapabilities) { c.Format = 2 },
		func(c *ReaderCapabilities) { c.SchedulerSupport = 4 },
		func(c *ReaderCapabilities) { c.SystemTaskSchemas = []int{1, 2} },
		func(c *ReaderCapabilities) { c.Adapters = []AdapterID{CgroupExitV1} },
	} {
		helper := Capabilities()
		mutation(&helper)
		if validateBackendWriteContract(floor, contract, helper) == nil {
			t.Fatal("stale/unrelated helper capabilities granted new backend write")
		}
	}
	delete(floor.RoleContracts, "scheduler-backend-reader")
	if validateBackendWriteContract(floor, contract, Capabilities()) == nil {
		t.Fatal("new backend inferred from current support integers alone")
	}
}
