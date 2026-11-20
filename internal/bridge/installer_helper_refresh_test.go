package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/executiondomain/testfixture"
	"fluxo/internal/services/maintenancetask"
)

func installerTaskIntentFixture(mode string) installerExecutionIntent {
	v := installerExecutionIntent{Format: 2, TaskTransition: mode, TaskCore: strings.Repeat("d", 64),
		Image: strings.Repeat("a", 64), Version: "0.4.53-test.1", InstallationID: strings.Repeat("b", 32),
		Service: strings.Repeat("c", 64), WasActive: true, Phase: "pending"}
	if mode == installerBackendRenewal {
		v.TaskBackend = strings.Repeat("e", 64)
	}
	return v
}

func TestInstallerTaskSelectionPrioritizesBackendAndPreservesNoop(t *testing.T) {
	for _, mode := range []string{"", installerBackendRenewal, installerHelperRefresh, "damaged-backend", "damaged-helper"} {
		t.Run(mode, func(t *testing.T) {
			helperCalls := 0
			got, err := selectInstallerTaskTransition(t.Context(), func(context.Context) (*maintenancetask.RegistrationBackendReview, error) {
				if mode == "damaged-backend" {
					return nil, errors.New("unreadable registration")
				}
				if mode == installerBackendRenewal {
					return &maintenancetask.RegistrationBackendReview{TaskCoreSHA256: strings.Repeat("a", 64), CurrentBackendSHA256: strings.Repeat("b", 64)}, nil
				}
				return nil, nil
			}, func(context.Context) (*maintenancetask.HelperRefreshReview, error) {
				helperCalls++
				if mode == "damaged-helper" {
					return &maintenancetask.HelperRefreshReview{TaskCoreSHA256: "invalid"}, nil
				}
				if mode == installerHelperRefresh {
					return &maintenancetask.HelperRefreshReview{TaskCoreSHA256: strings.Repeat("c", 64)}, nil
				}
				return nil, nil
			})
			if strings.HasPrefix(mode, "damaged-") {
				if err == nil {
					t.Fatal("integrity error became a no-op")
				}
				return
			}
			if err != nil || got.mode != mode || mode == installerBackendRenewal && helperCalls != 0 {
				t.Fatal(mode, got, helperCalls, err)
			}
		})
	}
}

func TestInstallerHelperWaitReleasesGateAndNeverWaitsOnObsoleteBackend(t *testing.T) {
	for _, needsBackend := range []bool{false, true} {
		held, waited, inspected := false, false, false
		coordinate := func(fn func() error) error {
			if held {
				t.Fatal("nested coordination gate")
			}
			held = true
			defer func() { held = false }()
			return fn()
		}
		got, err := reviewInstallerTaskWithWait(t.Context(), coordinate,
			func(context.Context) (*maintenancetask.RegistrationBackendReview, error) {
				if !held {
					t.Fatal("backend inspected outside its gates")
				}
				if needsBackend {
					return &maintenancetask.RegistrationBackendReview{TaskCoreSHA256: strings.Repeat("a", 64), CurrentBackendSHA256: strings.Repeat("b", 64)}, nil
				}
				return nil, nil
			}, func(context.Context) (*maintenancetask.HelperRefreshReview, error) {
				if !held || !waited {
					t.Fatal("helper inspected before genuine ACK wait or without gates")
				}
				inspected = true
				return &maintenancetask.HelperRefreshReview{TaskCoreSHA256: strings.Repeat("c", 64)}, nil
			}, func() error {
				if held {
					t.Fatal("receipt cannot acquire the held task gate")
				}
				waited = true
				return nil
			})
		if err != nil || needsBackend && (waited || inspected || got.mode != installerBackendRenewal) || !needsBackend && (!waited || !inspected || got.mode != installerHelperRefresh) {
			t.Fatal(needsBackend, got, err)
		}
	}
}

func TestInstallerTaskIntentVersionedModesAndExactOperationBinding(t *testing.T) {
	for _, mode := range []string{installerBackendRenewal, installerHelperRefresh} {
		v := installerTaskIntentFixture(mode)
		for _, phase := range []string{"pending", "committed", "complete"} {
			v.Phase = phase
			if phase != "pending" {
				v.Operation = "2177ef94-161b-4a2e-b37d-7dab78129c9d"
				v.Plan = strings.Repeat("f", 64)
			}
			raw, _ := json.Marshal(v)
			got, err := decodeInstallerExecutionIntent(raw)
			if err != nil || *got != v {
				t.Fatal(mode, phase, err)
			}
		}
		original := v
		if bindInstallerTaskSelection(&v, installerTaskSelection{mode: installerHelperRefresh, core: strings.Repeat("e", 64)}) == nil || v != original {
			t.Fatal("replanned UUID-bound operation")
		}
	}
	v := installerTaskIntentFixture(installerHelperRefresh)
	raw, _ := json.Marshal(v)
	for _, change := range []map[string]any{
		{"format": 1}, {"format": 3}, {"task_transition": nil}, {"task_transition": ""}, {"task_transition": "arbitrary"},
		{"task_core_sha256": ""}, {"task_transition": installerBackendRenewal}, {"task_backend_sha256": strings.Repeat("a", 64)},
	} {
		var fields map[string]any
		json.Unmarshal(raw, &fields)
		for key, value := range change {
			fields[key] = value
		}
		bad, _ := json.Marshal(fields)
		if _, err := decodeInstallerExecutionIntent(bad); err == nil {
			t.Fatal("accepted incompatible mode", change)
		}
	}
	before := v
	if err := bindInstallerTaskSelection(&v, installerTaskSelection{mode: installerBackendRenewal, core: strings.Repeat("e", 64), backend: strings.Repeat("f", 64)}); err != nil {
		t.Fatal(err)
	}
	before.TaskCore, before.TaskBackend, before.TaskTransition = v.TaskCore, v.TaskBackend, v.TaskTransition
	if before != v {
		t.Fatal("preparation review changed image, service, installation or restart intent")
	}
}

func TestInstallerFormatOneRejectsEveryTransitionFieldSpelling(t *testing.T) {
	v := installerTaskIntentFixture(installerHelperRefresh)
	v.Format, v.TaskTransition, v.TaskCore, v.TaskBackend = 1, "", "", ""
	raw, _ := json.Marshal(v)
	if _, err := decodeInstallerExecutionIntent(raw); err != nil {
		t.Fatal("valid historical intent refused", err)
	}
	for _, name := range []string{"task_transition", "TASK_TRANSITION", "Task_Transition", "tAsK_tRaNsItIoN"} {
		for _, value := range []any{nil, "", installerHelperRefresh} {
			var fields map[string]any
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			fields[name] = value
			bad, _ := json.Marshal(fields)
			if _, err := decodeInstallerExecutionIntent(bad); err == nil {
				t.Fatal("historical format acquired a new transition field", name, value)
			}
		}
	}
}

func installerHelperArchiveFixture() executionActivation {
	p := taskHelperRefreshFixture()
	p.Format = 4
	var floor executiondomain.ReaderFloor
	json.Unmarshal(p.SourceFloor, &floor)
	c, _ := activationContract(4)
	floor.SchedulerSupport, floor.SystemTaskSupport, floor.RoleContracts, floor.Adapters = c.SchedulerSupport, c.SystemTaskSupport, c.RoleContracts, c.Adapters
	p.SourceFloor, _ = json.Marshal(floor)
	p.SourceFloorSHA256 = digest(p.SourceFloor)
	return p
}

func TestInstallerHelperArchiveCannotAdoptBackendHistoricalOrAnotherSource(t *testing.T) {
	p := installerHelperArchiveFixture()
	raw, _ := json.Marshal(p)
	v := installerTaskIntentFixture(installerHelperRefresh)
	v.Operation, v.Plan, v.Image, v.Version, v.InstallationID = p.ID, digest(raw), p.BinarySHA256, p.Version, p.InstallationID
	v.TaskCore = p.Files[0].Before
	if err := validateInstallerPlan(v, raw); err != nil {
		t.Fatal("exact helper archive refused", err)
	}
	for _, change := range []func(*installerExecutionIntent){
		func(v *installerExecutionIntent) { v.TaskCore = strings.Repeat("1", 64) },
		func(v *installerExecutionIntent) { v.Image = strings.Repeat("1", 64) },
		func(v *installerExecutionIntent) {
			v.TaskTransition = installerBackendRenewal
			v.TaskBackend = strings.Repeat("1", 64)
		},
		func(v *installerExecutionIntent) {
			v.Format = 1
			v.TaskTransition = ""
			v.TaskBackend = strings.Repeat("1", 64)
		},
	} {
		other := v
		change(&other)
		if validateInstallerPlan(other, raw) == nil {
			t.Fatal("adopted unrelated task archive")
		}
	}
}

func TestUnpreparedInstallerTaskRetirementPreservesEvidence(t *testing.T) {
	if !testfixture.InRoot(t, "") {
		return
	}
	const root = "/var/lib/fluxo"
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	v := installerTaskIntentFixture(installerHelperRefresh)
	path := filepath.Join(root, installerExecutionRecord)
	if err := writeActivationJSON(path, v); err != nil {
		t.Fatal(err)
	}
	raw, err := readProtected(path, 16<<10)
	if err != nil {
		t.Fatal(err)
	}
	wrong := v
	wrong.Image = strings.Repeat("e", 64)
	if archiveUnpreparedInstallerIntent(root, wrong) == nil {
		t.Fatal("retired another image's preparation")
	}
	if err = archiveUnpreparedInstallerIntent(root, v); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("preparation pointer retained", err)
	}
	saved, err := readProtected(filepath.Join(root, "execution-installer-preparations", digest(raw)+".json"), 16<<10)
	if err != nil || !bytes.Equal(saved, raw) {
		t.Fatal("lost original preparation", err)
	}
	v.Operation = "2177ef94-161b-4a2e-b37d-7dab78129c9d"
	v.Plan = strings.Repeat("f", 64)
	if err = writeActivationJSON(path, v); err != nil {
		t.Fatal(err)
	}
	if archiveUnpreparedInstallerIntent(root, v) == nil {
		t.Fatal("discarded a published exact operation")
	}
	if _, err = readInstallerExecutionIntent(path); err != nil {
		t.Fatal("durable operation lost", err)
	}
}

func TestInstallerPreparationRetryDoesNotReplayPublishedMutation(t *testing.T) {
	calls := 0
	_, err := retryInstallerReadOnlyChecks(t.Context(), func() (ExecutionActivationReport, error) {
		calls++
		if calls == 1 {
			return ExecutionActivationReport{}, errInstallerPreparationChanged
		}
		return ExecutionActivationReport{Phase: "committed"}, nil
	})
	if err != nil || calls != 2 {
		t.Fatal(calls, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = retryInstallerReadOnlyChecks(ctx, func() (ExecutionActivationReport, error) {
		return ExecutionActivationReport{}, errInstallerPreparationChanged
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
