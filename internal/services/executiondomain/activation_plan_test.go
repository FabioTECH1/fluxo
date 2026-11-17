package executiondomain

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestActivationMergeRetainsOpaqueHistoryAndExactIntegers(t *testing.T) {
	original := []byte(`{"schema":2,"history":{"external_id":9223372036854775807,"custom":"preserve"},"entries":{"9":{"enabled":false,"command":"node custom.js","older_patch":{"keep":true}}}}`)
	next := map[string]any{"schema": 3, "entries": map[string]any{"9": map[string]any{"enabled": false, "command": "node custom.js", "adapter": CgroupExitV1}}}
	merged, err := MergeActivationJSON(original, next)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(merged), `9223372036854775807`) || !strings.Contains(string(merged), `"older_patch":{"keep":true}`) || !strings.Contains(string(merged), `"enabled":false`) {
		t.Fatal("activation changed preserved history", string(merged))
	}
}

func TestVersionedCoreRequiresCommittedFloorAndExplicitAdapter(t *testing.T) {
	floor := ReaderFloor{Format: 1, Phase: "committed", SchedulerSupport: 4, SystemTaskSupport: 2, SchedulerSchema: 3, SystemTaskSchema: 2, Adapters: []AdapterID{CgroupExitV1}, RoleContracts: Capabilities().RoleContracts, ActivationID: "123e4567-e89b-12d3-a456-426614174000", InstallationID: strings.Repeat("a", 32), BinarySHA256: strings.Repeat("b", 64), PlanSHA256: strings.Repeat("c", 64)}
	raw, _ := json.Marshal(floor)
	for _, adapter := range []string{"", `,"adapter":"systemd-cgroup-exit-v1"`} {
		report, err := inspectCompatibility("/var/lib/fluxo", func(path string, limit int64) ([]byte, error) {
			switch path {
			case "/var/lib/fluxo/execution-readers.json":
				return raw, nil
			case "/var/lib/fluxo-scheduler/state.json":
				return []byte(`{"schema":3,"entries":{},"invocations":{"a":{"started":true` + adapter + `}}}`), nil
			}
			return nil, os.ErrNotExist
		})
		if adapter == "" && (err == nil || report.Compatible) {
			t.Fatal("missing execution adapter was accepted")
		}
		if adapter != "" && (err != nil || !report.Compatible) {
			t.Fatal(report, err)
		}
	}
}

func TestProfileCapabilityIsSeparateFromSupportIntegers(t *testing.T) {
	stale := Capabilities()
	stale.RoleContracts["system-task-profile"] = []string{"legacy-host-inputs-v1"}
	if err := RequireCapabilities(stale, Capabilities()); err == nil {
		t.Fatal("stale helper satisfied narrowed execution policy by its support integer")
	}
	if err := RequireCapabilities(Capabilities(), Capabilities()); err != nil {
		t.Fatal(err)
	}
}
