package bridge

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
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

func preparingSchedulerFixture() installerSchedulerIntent {
	return installerSchedulerIntent{Format: 1, Image: strings.Repeat("b", 64), Version: "0.4.46-bridge.15", InstallationID: strings.Repeat("c", 32), Service: strings.Repeat("d", 64), Floor: strings.Repeat("e", 64), WasActive: true, Phase: "preparing"}
}

func TestInstallerSchedulerUnplannedStagesCannotClaimPublication(t *testing.T) {
	for _, phase := range []string{"preparing", "unchanged"} {
		v := preparingSchedulerFixture()
		v.Phase = phase
		raw, _ := json.Marshal(v)
		got, err := decodeInstallerSchedulerIntent(raw)
		if err != nil || *got != v || installerSchedulerPending(got) != (phase == "preparing") {
			t.Fatal(phase, got, err)
		}
		for _, field := range []string{"operation", "plan"} {
			bad := v
			if field == "operation" {
				bad.Operation = "2177ef94-161b-4a2e-b37d-7dab78129c9d"
			} else {
				bad.Plan = strings.Repeat("a", 64)
			}
			raw, _ = json.Marshal(bad)
			if _, err = decodeInstallerSchedulerIntent(raw); err == nil {
				t.Fatal("unplanned stage claimed publication", phase, field)
			}
		}
	}
	v := preparingSchedulerFixture()
	v.Phase = "complete"
	raw, _ := json.Marshal(v)
	if _, err := decodeInstallerSchedulerIntent(raw); err == nil {
		t.Fatal("original complete meaning lost its plan binding")
	}
}

func persistSchedulerStopFixture(root string, intent installerSchedulerIntent) error {
	raw, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(root, installerSchedulerRecord), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closed := f.Close()
	if err != nil {
		return err
	}
	if closed != nil {
		return closed
	}
	return syncActivationDirectory(root)
}

// Kill the subprocess at the real durable-before-stop boundary. Its fake host
// stop writes only a private marker, then dies without running any Go defers.
// No systemd service, production directory or privileged host action is used.
func TestInstallerSchedulerProcessDeathAfterStopRetainsRestartIntent(t *testing.T) {
	if root := os.Getenv("FLUXO_SCHEDULER_STOP_CRASH_TEST"); root != "" {
		err := pauseInstallerSchedulerPanel(preparingSchedulerFixture(), true, func() error { return nil }, func() error { return persistSchedulerStopFixture(root, preparingSchedulerFixture()) }, func() error {
			if err := os.WriteFile(filepath.Join(root, "stopped"), []byte("stopped"), 0600); err != nil {
				return err
			}
			return syscall.Kill(os.Getpid(), syscall.SIGKILL)
		})
		t.Fatalf("crash boundary returned: %v", err)
	}
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestInstallerSchedulerProcessDeathAfterStopRetainsRestartIntent$")
	cmd.Env = append(os.Environ(), "FLUXO_SCHEDULER_STOP_CRASH_TEST="+root)
	err := cmd.Run()
	var exited *exec.ExitError
	if !errors.As(err, &exited) {
		t.Fatalf("child was not killed: %v", err)
	}
	status, ok := exited.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("expected SIGKILL, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "stopped")); err != nil {
		t.Fatal("stop boundary not reached", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, installerSchedulerRecord))
	if err != nil {
		t.Fatal(err)
	}
	intent, err := decodeInstallerSchedulerIntent(raw)
	if err != nil || *intent != preparingSchedulerFixture() || !installerSchedulerPending(intent) {
		t.Fatal("restart binding lost on process death", intent, err)
	}
	stops := 0
	if err := pauseInstallerSchedulerPanel(*intent, false, func() error { t.Fatal("inactive panel must not drain"); return nil }, func() error { return persistSchedulerStopFixture(root, *intent) }, func() error { stops++; return nil }); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(root, installerSchedulerRecord))
	if stops != 0 || string(after) != string(raw) {
		t.Fatal("inactive retry lost original restart intent")
	}
}

func TestInstallerSchedulerEveryUnpublishedStopDrains(t *testing.T) {
	busy := errors.New("panel command still running")
	for _, phase := range []string{"preparing", "pending"} {
		v := preparingSchedulerFixture()
		v.Phase = phase
		if phase == "pending" {
			v.Operation = "2177ef94-161b-4a2e-b37d-7dab78129c9d"
			v.Plan = strings.Repeat("a", 64)
		}
		root := t.TempDir()
		stopped := false
		err := pauseInstallerSchedulerPanel(v, true, func() error { return busy }, func() error { t.Fatal("refused drain persisted a record"); return nil }, func() error { stopped = true; return nil })
		if !errors.Is(err, busy) || stopped {
			t.Fatal(phase, "active work was stopped", err)
		}
		if _, err := os.Stat(filepath.Join(root, installerSchedulerRecord)); !os.IsNotExist(err) {
			t.Fatal("refused drain changed recovery record", err)
		}
	}
}

func TestInstallerSchedulerPublishedRetryReusesRunningPanel(t *testing.T) {
	for _, active := range []bool{true, false} {
		for _, complete := range []bool{true, false} {
			v := preparingSchedulerFixture()
			v.Phase = "published"
			calls := []string{}
			step := func(name string) func() error { return func() error { calls = append(calls, name); return nil } }
			if err := finishPublishedInstallerScheduler(&v, active, complete, step("verify"), step("start"), step("ack"), step("persist")); err != nil {
				t.Fatal(err)
			}
			want := []string{"verify"}
			if !active {
				want = append(want, "start")
			}
			want = append(want, "ack")
			if complete {
				want = append(want, "persist")
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatal(active, complete, calls, want)
			}
			if complete != (v.Phase == "complete") {
				t.Fatal("completion phase drift")
			}
		}
	}
}
