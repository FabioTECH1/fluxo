package bridge

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInstallerSchedulerIntentBindsExactRecoveryAndRestartIntent(t *testing.T) {
	v := installerSchedulerIntent{Format: 1, Operation: "2177ef94-161b-4a2e-b37d-7dab78129c9d", Plan: strings.Repeat("a", 64), Image: strings.Repeat("b", 64), Version: "0.4.46-bridge.13", InstallationID: strings.Repeat("c", 32), Service: strings.Repeat("d", 64), Floor: strings.Repeat("e", 64), WasActive: true, Phase: "pending"}
	for _, phase := range []string{"pending", "published", "complete"} {
		v.Phase = phase
		raw, _ := json.Marshal(v)
		got, err := decodeInstallerSchedulerIntent(raw)
		if err != nil || *got != v {
			t.Fatal(phase, err)
		}
	}
	raw, _ := json.Marshal(v)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	for key := range fields {
		for _, null := range []bool{false, true} {
			var changed map[string]any
			_ = json.Unmarshal(raw, &changed)
			if null {
				changed[key] = nil
			} else {
				delete(changed, key)
			}
			encoded, _ := json.Marshal(changed)
			if _, err := decodeInstallerSchedulerIntent(encoded); err == nil {
				t.Fatalf("missing/null %s accepted", key)
			}
		}
	}
	for key, value := range map[string]any{"operation_id": "../../other", "plan_sha256": "bad", "image_sha256": "bad", "service_sha256": "bad", "floor_sha256": "bad", "installation_id": "bad", "version": "dev", "phase": "ready", "format": 2, "extra": true} {
		var changed map[string]any
		_ = json.Unmarshal(raw, &changed)
		changed[key] = value
		encoded, _ := json.Marshal(changed)
		if _, err := decodeInstallerSchedulerIntent(encoded); err == nil {
			t.Fatal("invalid field accepted", key)
		}
	}
	if _, err := decodeInstallerSchedulerIntent(append(raw, raw...)); err == nil {
		t.Fatal("trailing intent accepted")
	}
}
