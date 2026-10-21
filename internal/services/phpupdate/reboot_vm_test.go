package phpupdate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

const rebootSpecPath = "/var/lib/fluxo-php-recovery-reboot.json"

type rebootSpec struct {
	DataDir     string
	OperationID string
	Boot        string
	Units       []string
	Packages    []Package
}

func requireRebootVM(t *testing.T) {
	requireRecoveryVM(t)
	if os.Getenv("FLUXO_PHP_UPDATE_REBOOT_VM_TEST") != "1" {
		t.Skip("explicit isolated VM reboot opt-in required")
	}
}

// Stage intentionally retains its independent worker and fixture across reboot.
// The paired Resume test verifies admission before any operator cleanup.
func TestVMStageRebootDuringPackageTransaction(t *testing.T) {
	requireRebootVM(t)
	if _, err := os.Stat(rebootSpecPath); !os.IsNotExist(err) {
		t.Fatal("an unfinished reboot acceptance spec already exists")
	}
	m := vmWorkerStore(t)
	vmCloneExistingConsumers(t)
	originalPackages := vmDowngradeFixture(t, m, true)
	var units []string
	for index := 0; index < 2; index++ {
		id := 9105000 + index
		command := "php8.4 artisan queue:work redis --queue=fluxo-reboot-" + uuid.NewString() + " --sleep=1 --tries=1 --timeout=120"
		if _, err := database.DB.Exec("INSERT INTO sites(id,domain,path,php_version,app_type,deployment_strategy) VALUES (?,?,?,'8.4','laravel','standard')", id, "reboot-"+uuid.NewString()+".test", recoveryVMApp); err != nil {
			t.Fatal(err)
		}
		if _, err := database.DB.Exec("INSERT INTO daemons(id,site_id,name,managed_kind,command,directory,user,instances,stop_seconds) VALUES (?,?,?,'laravel_queue',?,?,'fluxo',1,2)", id, id, "Reboot queue", command, recoveryVMApp); err != nil {
			t.Fatal(err)
		}
		name := unitName(id, 1)
		units = append(units, name)
		definition := "[Unit]\nDescription=Fluxo disposable reboot queue fixture\n[Service]\nType=simple\nUser=fluxo\nWorkingDirectory=" + recoveryVMApp + "\nExecStart=/usr/bin/" + command + "\nRestart=always\nRestartSec=1\nTimeoutStopSec=2\nKillSignal=SIGTERM\n[Install]\nWantedBy=multi-user.target\n"
		if err := writeDurableExclusive(filepath.Join("/etc/systemd/system", name), []byte(definition), 0644); err != nil {
			t.Fatal(err)
		}
	}
	vmRun(t, "/usr/bin/systemctl", "daemon-reload")
	for _, name := range units {
		vmRun(t, "/usr/bin/systemctl", "enable", name)
	}
	vmRun(t, "/usr/bin/systemctl", "start", units[0])
	time.Sleep(time.Second)
	plan := vmCheck(t, m)
	if !plan.Rollback.Available {
		t.Fatal("reboot acceptance requires complete authenticated archives")
	}
	op, err := m.Enqueue(context.Background(), "apply", "8.4", plan.ID, uuid.NewString(), "reboot-acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		t.Fatal(err)
	}
	spec := rebootSpec{DataDir: m.DataDir, OperationID: op.ID, Boot: strings.TrimSpace(string(boot)), Units: units, Packages: originalPackages}
	raw, _ := json.Marshal(spec)
	if err = writeDurableExclusive(rebootSpecPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		o, e := GetOperation(op.ID)
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(o.Output, "Unpacking php8.4") {
			g, e := getGeneration(op.ID)
			if e != nil {
				t.Fatal(e)
			}
			for _, unit := range g.Attempt.Units {
				if managedUnitPattern.MatchString(unit.Name) {
					b, e := getBarrier(unit.Name)
					if e != nil || verifyHeldBarrier(context.Background(), b) != nil {
						t.Fatal("reboot attempted without a verified hold")
					}
				}
			}
			t.Logf("REBOOT_REQUESTED during real dpkg work; preserved %s", rebootSpecPath)
			if _, e = syscmd.Run(context.Background(), 10*time.Second, "/usr/bin/systemctl", "reboot"); e != nil {
				t.Fatal(e)
			}
			return
		}
		if !o.Active() {
			t.Fatalf("update ended before reboot boundary: %s", o.Message)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("could not reach package mutation reboot boundary")
}

func TestVMResumeAfterRealPackageReboot(t *testing.T) {
	requireRebootVM(t)
	raw, err := os.ReadFile(rebootSpecPath)
	if err != nil {
		t.Fatal(err)
	}
	var spec rebootSpec
	if err = json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil || strings.TrimSpace(string(boot)) == spec.Boot {
		t.Fatal("the VM did not actually reboot")
	}
	if err = database.InitDB(filepath.Join(spec.DataDir, "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	defer database.DB.Close()
	m := NewManager(spec.DataDir)
	m.Launch = func(context.Context, Operation) error {
		t.Fatal("reconciliation attempted to replay package work")
		return nil
	}
	deadline := time.Now().Add(75 * time.Second)
	for {
		m.reconcile(context.Background())
		current, e := GetOperation(spec.OperationID)
		if e != nil {
			t.Fatal(e)
		}
		if !current.Active() {
			break
		}
		if busy, e := Applying(); e != nil || !busy {
			t.Fatal("admission was released while interruption reconciliation was pending", e)
		}
		if time.Now().After(deadline) {
			t.Fatal("supervisor liveness grace did not reconcile the reboot")
		}
		time.Sleep(time.Second)
	}
	o, err := GetOperation(spec.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if o.Active() || o.Status == "succeeded" {
		t.Fatalf("reboot was falsely treated as successful/live: %+v", o)
	}
	if busy, e := Applying(); e != nil || !busy {
		t.Fatalf("reboot lost maintenance admission: %v %v", busy, e)
	}
	g, err := getGeneration(spec.OperationID)
	if err != nil || !g.Pending {
		t.Fatal("reboot lost pending generation", err)
	}
	for _, name := range spec.Units {
		b, e := getBarrier(name)
		if e != nil || verifyHeldBarrier(context.Background(), b) != nil {
			t.Fatal("boot did not preserve effective consumer hold", name, e)
		}
		vmRun(t, "/usr/bin/systemctl", "start", name)
		u, e := unitState(context.Background(), name)
		if e != nil || u.Active || len(u.PIDs) != 0 {
			t.Fatal("boot/manual start bypassed maintenance condition", name, e)
		}
	}
	ob, err := m.observePackages(context.Background(), "", "8.4")
	journalRefused := err != nil && strings.Contains(err.Error(), "interrupted package journal requires operator repair")
	if err != nil && !journalRefused {
		t.Fatal(err)
	}
	review, reviewErr := m.ReviewRecovery(context.Background(), o.ID, "repair")
	if journalRefused || ob.JournalDirty || len(ob.UnrelatedPending) > 0 {
		if reviewErr == nil {
			t.Fatal("reboot left unrelated/journal damage but scoped Repair was admitted")
		}
		t.Log("Reboot interruption preserved; dirty journal/unrelated state correctly requires operator repair")
	} else {
		if reviewErr != nil {
			t.Fatal(reviewErr)
		}
		m.Launch = func(ctx context.Context, o Operation) error {
			_, err := syscmd.Run(ctx, 15*time.Second, "/usr/bin/systemd-run", "--quiet", "--unit=fluxo-php-update-"+o.ID, "--property=Type=exec", "--property=Restart=no", "--property=TimeoutStopSec=infinity", "--setenv=FLUXO_ENV=prod", "--setenv=FLUXO_DATA_DIR="+spec.DataDir, "--", recoveryVMWorker, "php-update-worker", o.ID)
			return err
		}
		repair, e := m.EnqueueRecovery(context.Background(), review.ID, uuid.NewString(), "reboot-acceptance", "")
		if e != nil {
			t.Fatal(e)
		}
		if done := vmWaitOperation(t, repair.ID); done.Status != "succeeded" {
			t.Fatal(done.Message)
		}
		t.Log("Reviewed scoped Repair recovered the real reboot interruption")
	}
	// Explicit isolated-fixture cleanup: authenticated old archives only, exact
	// recorded branch packages; never broad dpkg --configure -a/fix-broken.
	goal := "before"
	matches := func(targets []PackageArchive) bool {
		if len(targets) != len(spec.Packages) {
			return false
		}
		for _, p := range spec.Packages {
			found := false
			for _, a := range targets {
				if p.Name == a.Name && p.Architecture == a.Architecture && p.Installed == a.Version {
					found = true
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	if matches(g.Archives.After) {
		goal = "after"
	} else if !matches(g.Archives.Before) {
		t.Fatal("the retained fixture does not match the original VM package revisions")
	}
	if err = m.verifyRetainedArchives(context.Background(), "", g, goal); err != nil {
		t.Fatal(err)
	}
	dir, err := m.archiveDirectory(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"--unpack"}
	var names []string
	targets := g.Archives.Before
	if goal == "after" {
		targets = g.Archives.After
	}
	for _, p := range targets {
		args = append(args, filepath.Join(dir, p.SHA256+".deb"))
		names = append(names, p.Name)
	}
	vmRun(t, "/usr/bin/dpkg", args...)
	vmRun(t, "/usr/bin/dpkg", append([]string{"--configure"}, names...)...)
	vmRestoreMarks(t, spec.Packages)
	if err = m.releaseBarriers(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	for _, name := range spec.Units {
		vmRun(t, "/usr/bin/systemctl", "disable", "--now", name)
		if err = os.Remove(filepath.Join("/etc/systemd/system", name)); err != nil {
			t.Fatal(err)
		}
	}
	vmRun(t, "/usr/bin/systemctl", "daemon-reload")
	if err = os.Rename(rebootSpecPath, rebootSpecPath+".completed-"+uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	t.Log("Actual reboot, persistent holds/admission, honest interruption and scoped recovery verified; forensic DB retained")
}
