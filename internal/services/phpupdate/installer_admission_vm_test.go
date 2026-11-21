package phpupdate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"fluxo/internal/services/packages"
	"fluxo/internal/services/scheduler"
	"fluxo/internal/syscmd"
)

// The external guarded fixture creates its site/registration using the normal
// API and first verifies genuine cron acknowledgement and hosted execution.
// This test holds a real exclusive lifecycle FD until the next real cron launch
// reaches Bootstrap. Only the first acquisition callback retains that already
// owned FD, making the handoff deterministic; every subsequent acquisition and
// all preparation/reconciliation/upgrade operations use production code.
// No invocation, receipt, seal, nonce or service definition is synthesized.
func TestVMInstallerPendingLaunchAdmission(t *testing.T) {
	requireRecoveryVM(t)
	const root = "/root/fluxo-installer-admission-20261121"
	if os.Getenv("FLUXO_INSTALLER_ADMISSION_FIXTURE") != root {
		t.Skip("explicit prepared initial-admission fixture only")
	}
	raw, err := os.ReadFile(filepath.Join(root, "fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		CronID int `json:"cron_id"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil || fixture.CronID <= 0 {
		t.Fatal("fixture lacks its normally created Scheduler entry", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	owned, err := packages.Lifecycle(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	var firstOnce sync.Once
	releaseFirst := func() { firstOnce.Do(owned) }
	defer releaseFirst()
	if err = os.WriteFile(filepath.Join(root, "exclusive-ready.json"), []byte("{\"owned\":true}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	type invocation struct {
		Unit    string `json:"unit"`
		Started bool   `json:"started"`
		Entry   struct {
			ID int `json:"id"`
		} `json:"entry"`
	}
	var observed invocation
	for observed.Unit == "" {
		raw, err = os.ReadFile("/var/lib/fluxo-scheduler/state.json")
		if err != nil {
			t.Fatal(err)
		}
		var j struct {
			Invocations map[string]invocation `json:"invocations"`
		}
		if err = json.Unmarshal(raw, &j); err != nil {
			t.Fatal(err)
		}
		for name, v := range j.Invocations {
			if v.Started || v.Entry.ID != fixture.CronID || name != v.Unit || !strings.HasPrefix(name, "fluxo-scheduler-") || filepath.Base(name) != name {
				continue
			}
			out, e := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", name, "--property=MainPID,ActiveState")
			if e != nil {
				t.Fatal(e)
			}
			active, mainPID := false, 0
			for _, line := range strings.Split(out, "\n") {
				if line == "ActiveState=active" {
					active = true
				}
				if strings.HasPrefix(line, "MainPID=") {
					mainPID, _ = strconv.Atoi(strings.TrimPrefix(line, "MainPID="))
				}
			}
			if active && mainPID > 1 {
				observed = v
				break
			}
		}
		if observed.Unit == "" {
			select {
			case <-ctx.Done():
				t.Fatal("no genuine pending cron launch observed", ctx.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	parent := os.Getppid()
	parentRaw, err := os.ReadFile("/proc/" + strconv.Itoa(parent) + "/stat")
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(parentRaw)[strings.LastIndex(string(parentRaw), ")")+1:])
	if len(fields) < 20 {
		t.Fatal("invalid fixture parent identity")
	}
	parentKey := fields[19]
	acquires, attempts, pending := 0, 0, 0
	waitCtx, stopWaiting := context.WithTimeout(ctx, 90*time.Second)
	defer stopWaiting()
	release, id, err := retryInstallerLeaseAdmission(waitCtx,
		func() bool { return sameInstallerParent(parent, parentKey) },
		func(ctx context.Context) (func(), error) {
			acquires++
			if acquires == 1 {
				return releaseFirst, nil
			}
			return packages.Lifecycle(ctx, false)
		}, func(ctx context.Context) (string, error) {
			attempts++
			id, err := prepareInstallerLease(ctx, "/var/lib/fluxo", parent, parentKey)
			if scheduler.InstallerLaunchPending(err) {
				pending++
			}
			return id, err
		}, scheduler.InstallerLaunchPending,
		func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
				return nil
			}
		})
	if err != nil {
		t.Fatal("real pending-launch admission failed", err)
	}
	defer release()
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := scheduler.EndUpgrade(cleanup, id); err != nil {
			t.Error("could not release this test's own upgrade lease", err)
		}
	}()
	if attempts < 2 || pending == 0 || acquires != attempts {
		t.Fatal("native pending-launch retry was not exercised", attempts, pending, acquires)
	}
	result, _ := json.Marshal(map[string]any{"passed": true, "attempts": attempts, "typed_pending_observations": pending, "normal_prepare_reconciliation_and_upgrade": true, "first_acquisition_uses_fixture_owned_real_exclusive_FD": true, "subsequent_acquisitions_use_production_lifecycle": true, "manual_invocation_nonce_ACK_or_service_writes": false, "application_force_stop": false, "public_installer_or_native_Cronie_claimed": false})
	if err = os.WriteFile(filepath.Join(root, "native-admission-result.json"), append(result, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
