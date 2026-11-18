package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"fluxo/internal/services/phpupdate"
)

func TestInstallerRetriesOnlyReadOnlyChecks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	want := ExecutionActivationReport{OperationID: "unchanged-operation", Phase: "committed", FormatActive: true}
	got, err := retryInstallerReadOnlyChecks(ctx, func() (ExecutionActivationReport, error) {
		calls++
		if calls == 1 {
			return ExecutionActivationReport{}, phpupdate.ErrInstallerChecksPending
		}
		return want, nil
	})
	if err != nil || calls != 2 || got.OperationID != want.OperationID {
		t.Fatal("read-only check did not drain before retry", calls, got, err)
	}
	blocked := errors.New("maintenance generation requires recovery")
	calls = 0
	if _, err = retryInstallerReadOnlyChecks(ctx, func() (ExecutionActivationReport, error) { calls++; return want, blocked }); !errors.Is(err, blocked) || calls != 1 {
		t.Fatal("mutation/recovery refusal retried", calls, err)
	}
	cancel()
	if _, err = retryInstallerReadOnlyChecks(ctx, func() (ExecutionActivationReport, error) { return want, phpupdate.ErrInstallerChecksPending }); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled check wait continued", err)
	}
}

func TestInstallerAwaitsGenuineReceiptsBeforeReadinessAdmission(t *testing.T) {
	acknowledged, checked := false, false
	if err := installerExecutionAdmission(true, func() error { acknowledged = true; return nil }, func() error {
		checked = true
		if !acknowledged {
			return errors.New("publication has not been acknowledged")
		}
		return nil
	}); err != nil || !checked {
		t.Fatal("fresh publication failed before genuine acknowledgement", err)
	}
	waitErr := errors.New("receipt wait expired")
	checked = false
	if err := installerExecutionAdmission(true, func() error { return waitErr }, func() error { checked = true; return nil }); !errors.Is(err, waitErr) || checked {
		t.Fatal("failed acknowledgement crossed admission", err)
	}
	liveWork := errors.New("active execution domain")
	if err := installerExecutionAdmission(true, func() error { return nil }, func() error { return liveWork }); !errors.Is(err, liveWork) {
		t.Fatal("receipt relaxed domain admission", err)
	}
	if err := installerExecutionAdmission(false, func() error { t.Fatal("explicit backend review waited on old provider"); return nil }, func() error { return liveWork }); !errors.Is(err, liveWork) {
		t.Fatal("explicit review bypassed admission", err)
	}
}

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

func TestInstallerExecutionIntentRetainsTaskBackendExpectations(t *testing.T) {
	v := installerExecutionIntent{Format: 1, Image: strings.Repeat("a", 64), Version: "0.4.46-bridge.13", InstallationID: strings.Repeat("b", 32), Service: strings.Repeat("c", 64), WasActive: true, Phase: "pending", TaskCore: strings.Repeat("d", 64), TaskBackend: strings.Repeat("e", 64)}
	raw, _ := json.Marshal(v)
	if got, err := decodeInstallerExecutionIntent(raw); err != nil || *got != v {
		t.Fatal(err)
	}
	for _, key := range []string{"task_core_sha256", "task_backend_sha256"} {
		for _, value := range []any{nil, "", "invalid"} {
			var changed map[string]any
			_ = json.Unmarshal(raw, &changed)
			changed[key] = value
			bad, _ := json.Marshal(changed)
			if _, err := decodeInstallerExecutionIntent(bad); err == nil {
				t.Fatal("lost task expectation accepted", key, value)
			}
		}
	}
}
