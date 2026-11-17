package executiondomain

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestLegacyAdapterCannotSelectCandidateFromAnOldRecord(t *testing.T) {
	for _, id := range []AdapterID{"", CgroupExitV1} {
		got, err := LegacyAdapter(id)
		if err != nil || got != CgroupExitV1 {
			t.Fatal("old invocation changed meaning", got, err)
		}
	}
	for _, id := range []AdapterID{Persistent249CandidateV1, "future-adapter", "systemd-cgroup-exit-v2"} {
		if _, err := LegacyAdapter(id); err == nil {
			t.Fatal("unsupported invocation acquired execution semantics", id)
		}
	}
}

func TestCompatibilityRejectsFutureAuthorityBeforeInitialization(t *testing.T) {
	for _, tc := range []struct {
		name, path, document, errorPart string
	}{
		{"future-scheduler", "/var/lib/fluxo-scheduler/state.json", `{"schema":6,"entries":{},"invocations":{}}`, "schema 6"},
		{"future-task", "/var/lib/fluxo-system-tasks/state.json", `{"schema":4,"entries":{},"invocations":{}}`, "schema 4"},
		{"candidate-in-old-core", "/var/lib/fluxo-scheduler/state.json", `{"schema":2,"entries":{},"invocations":{"attempt":{"adapter":"systemd-persistent-249-candidate-v1"}}}`, "unsupported execution adapter"},
		{"null-adapter", "/var/lib/fluxo-system-tasks/state.json", `{"schema":1,"entries":{},"invocations":{"attempt":{"adapter":null}}}`, "cannot be null"},
		{"partial-core", "/var/lib/fluxo-scheduler/state.json", `{"schema":2}`, "incomplete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reads := 0
			r, err := inspectCompatibility("/var/lib/fluxo", func(path string, limit int64) ([]byte, error) {
				reads++
				if path == tc.path {
					return []byte(tc.document), nil
				}
				return nil, os.ErrNotExist
			})
			if err == nil || !strings.Contains(err.Error(), tc.errorPart) || r.Compatible || reads == 0 {
				t.Fatalf("future/corrupt authority passed compatibility: %+v %v", r, err)
			}
		})
	}
}

func TestMinimumReaderRecordNeverBecomesActivationProof(t *testing.T) {
	base := ReaderFloor{Format: 1, Phase: "committed", SchedulerSupport: 3, SystemTaskSupport: 1, SchedulerSchema: 2, SystemTaskSchema: 1, Adapters: []AdapterID{CgroupExitV1}}
	for _, tc := range []struct {
		name   string
		mutate func(*ReaderFloor)
	}{
		{"incomplete-activation", func(f *ReaderFloor) { f.Phase = "prepared" }},
		{"future-reader", func(f *ReaderFloor) {
			f.SchedulerSupport = SchedulerCompiledReaderSupport + 1
			f.ActivationID = "123e4567-e89b-12d3-a456-426614174000"
			f.InstallationID = strings.Repeat("a", 32)
			f.BinarySHA256 = strings.Repeat("b", 64)
			f.PlanSHA256 = strings.Repeat("c", 64)
			f.RoleContracts = Capabilities().RoleContracts
		}},
		{"future-profile", func(f *ReaderFloor) {
			f.RoleContracts = map[string][]string{"system-task-profile": {"isolated-apt-future-v1"}}
		}},
		{"future-adapter", func(f *ReaderFloor) { f.Adapters = []AdapterID{Persistent249CandidateV1} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := base
			tc.mutate(&f)
			raw, _ := json.Marshal(f)
			r, err := inspectCompatibility("/var/lib/fluxo", func(path string, limit int64) ([]byte, error) {
				if path == "/var/lib/fluxo/execution-readers.json" {
					return raw, nil
				}
				return nil, os.ErrNotExist
			})
			if err == nil || r.Compatible || r.Floor == nil {
				t.Fatal("reader check manufactured activation", r, err)
			}
		})
	}
	if _, err := decodeFloor([]byte(`{"format":1,"phase":"committed","scheduler_support":3,"system_task_support":1,"scheduler_schema":2,"system_task_schema":1,"adapters":["systemd-cgroup-exit-v1"],"unknown_authority":true}`)); err == nil {
		t.Fatal("unknown minimum-reader semantics were ignored")
	}
}

func TestCompatibilityRetainsOldRecordInterpretation(t *testing.T) {
	r, err := inspectCompatibility("/var/lib/fluxo", func(path string, limit int64) ([]byte, error) {
		switch path {
		case "/var/lib/fluxo-scheduler/state.json":
			return []byte(`{"schema":1,"entries":{},"invocations":{"attempt":{}}}`), nil
		case "/var/lib/fluxo-system-tasks/state.json":
			return []byte(`{"schema":1,"entries":{},"invocations":{"attempt":{"adapter":"systemd-cgroup-exit-v1"}}}`), nil
		}
		return nil, os.ErrNotExist
	})
	if err != nil || !r.Compatible || len(r.Journals) != 2 {
		t.Fatal(r, err)
	}
}

func TestCompatibilityDoesNotTurnLostActivationEvidenceIntoFreshState(t *testing.T) {
	oldScheduler := []byte(`{"schema":2,"entries":{},"invocations":{}}`)
	oldTask := []byte(`{"schema":1,"entries":{},"invocations":{}}`)
	cases := []struct {
		name  string
		files map[string][]byte
		want  string
	}{
		{"historical", map[string][]byte{"/var/lib/fluxo-scheduler/state.json": oldScheduler, "/var/lib/fluxo-system-tasks/state.json": oldTask}, ""},
		{"scheduler-lost-floor", map[string][]byte{"/var/lib/fluxo-scheduler/state.json": []byte(`{"schema":4,"entries":{},"invocations":{}}`)}, "minimum-reader floor"},
		{"task-lost-floor", map[string][]byte{"/var/lib/fluxo-system-tasks/state.json": []byte(`{"schema":3,"entries":{},"invocations":{}}`)}, "minimum-reader floor"},
		{"archive-before-first-core", map[string][]byte{"/var/lib/fluxo/execution-activations": {}, "/var/lib/fluxo-scheduler/state.json": oldScheduler, "/var/lib/fluxo-system-tasks/state.json": oldTask}, "exact original operation"},
		{"anchor-beside-old-core", map[string][]byte{"/var/lib/fluxo-scheduler/state.json": oldScheduler, "/var/lib/fluxo-scheduler.initialized": []byte("activation:123e4567-e89b-12d3-a456-426614174000\n")}, "initialization anchor"},
		{"task-initializing-beside-old-core", map[string][]byte{"/var/lib/fluxo-system-tasks/state.json": oldTask, "/var/lib/fluxo-system-tasks.lock": []byte("initializing\n")}, "initialization is incomplete"},
		{"historical-ready-markers", map[string][]byte{"/var/lib/fluxo-scheduler/state.json": oldScheduler, "/var/lib/fluxo-system-tasks/state.json": oldTask, "/var/lib/fluxo-scheduler.initialized": []byte("1\n"), "/var/lib/fluxo-system-tasks.initialized": []byte("1\n"), "/var/lib/fluxo-system-tasks.lock": []byte("ready\n")}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := json.Marshal(tc.files)
			r, err := inspectCompatibility("/var/lib/fluxo", func(path string, _ int64) ([]byte, error) {
				if b, ok := tc.files[path]; ok {
					return append([]byte(nil), b...), nil
				}
				return nil, os.ErrNotExist
			})
			if tc.want == "" {
				if err != nil || !r.Compatible {
					t.Fatalf("historical interpretation changed: %+v %v", r, err)
				}
			} else if err == nil || r.Compatible || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("damaged operation admitted: %+v %v", r, err)
			}
			after, _ := json.Marshal(tc.files)
			if string(before) != string(after) {
				t.Fatal("pure inspection rewrote evidence")
			}
		})
	}
}
