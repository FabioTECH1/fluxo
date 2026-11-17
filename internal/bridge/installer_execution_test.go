package bridge

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInstallerExecutionIntentRecoveryBoundaries(t *testing.T) {
	v := installerExecutionIntent{Format: 1, Image: strings.Repeat("a", 64), Version: "0.4.46-bridge.8", InstallationID: strings.Repeat("b", 32), Service: strings.Repeat("c", 64), WasActive: true, Phase: "pending"}
	raw, _ := json.Marshal(v)
	if _, err := decodeInstallerExecutionIntent(raw); err != nil {
		t.Fatal(err)
	}
	v.Operation, v.Plan = "2177ef94-161b-4a2e-b37d-7dab78129c9d", strings.Repeat("d", 64)
	for _, phase := range []string{"pending", "committed", "complete"} {
		v.Phase = phase
		raw, _ = json.Marshal(v)
		if got, err := decodeInstallerExecutionIntent(raw); err != nil || *got != v {
			t.Fatalf("phase %s: %v", phase, err)
		}
	}
	raw, _ = json.Marshal(v)
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for key := range fields {
		t.Run("missing-"+key, func(t *testing.T) {
			var clone map[string]any
			json.Unmarshal(raw, &clone)
			delete(clone, key)
			changed, _ := json.Marshal(clone)
			if _, err := decodeInstallerExecutionIntent(changed); err == nil {
				t.Fatal("missing authority accepted")
			}
		})
		t.Run("null-"+key, func(t *testing.T) {
			var clone map[string]any
			json.Unmarshal(raw, &clone)
			clone[key] = nil
			changed, _ := json.Marshal(clone)
			if _, err := decodeInstallerExecutionIntent(changed); err == nil {
				t.Fatal("null authority accepted")
			}
		})
	}
	for _, patch := range []map[string]any{
		{"operation_id": "../../other"}, {"operation_id": ""}, {"plan_sha256": ""}, {"image_sha256": "arbitrary"}, {"phase": "ready"}, {"format": 2}, {"installation_id": "other"}, {"was_active": "true"}, {"extra": true},
	} {
		var clone map[string]any
		json.Unmarshal(raw, &clone)
		for k, value := range patch {
			clone[k] = value
		}
		changed, _ := json.Marshal(clone)
		if _, err := decodeInstallerExecutionIntent(changed); err == nil {
			t.Fatalf("accepted altered record: %v", patch)
		}
	}
	if _, err := decodeInstallerExecutionIntent(append(raw, raw...)); err == nil {
		t.Fatal("trailing authority accepted")
	}
}
