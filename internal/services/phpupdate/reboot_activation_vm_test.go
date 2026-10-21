package phpupdate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

const activationRebootSpecPath = "/var/lib/fluxo-php-recovery-activation-reboot.json"

type activationRebootSpec struct {
	DataDir, GenerationID, Boot string
	Units                       []string
	Definitions                 []string
}

// This pair injects only the post-package activation/release checkpoint. Package
// versions are observed, never changed: the separate package-reboot pair covers
// real dpkg interruption. Stage deliberately retains all evidence for Resume.
func TestVMStageRebootDuringPartialActivationRelease(t *testing.T) {
	requireRebootVM(t)
	for _, path := range []string{activationRebootSpecPath, rebootSpecPath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("finish the existing reboot acceptance before staging another", path)
		}
	}
	m := vmWorkerStore(t)
	m.Launch = func(context.Context, Operation) error { return nil }
	vmCloneExistingConsumers(t)
	spec := activationRebootSpec{DataDir: m.DataDir}
	for index := 0; index < 2; index++ {
		id := 9106000 + index
		name := unitName(id, 1)
		// Refuse all pre-existing unit definitions, drop-ins and enablement links.
		for _, path := range []string{filepath.Join("/etc/systemd/system", name), filepath.Join("/etc/systemd/system", name+".d"), filepath.Join("/etc/systemd/system/multi-user.target.wants", name)} {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatal("fixture path already exists; refusing to replace it", path)
			}
		}
		properties := unitProperties(vmRun(t, "/usr/bin/systemctl", "show", name, "--property=LoadState,ActiveState"))
		if properties["LoadState"] != "not-found" || properties["ActiveState"] != "inactive" {
			t.Fatal("fixture name already belongs to a service", name, properties)
		}
		command := "php8.4 artisan queue:work redis --queue=fluxo-activation-reboot-" + uuid.NewString() + " --sleep=1 --tries=1 --timeout=120"
		if _, err := database.DB.Exec("INSERT INTO sites(id,domain,path,php_version,app_type,deployment_strategy) VALUES (?,?,?,'8.4','laravel','standard')", id, "activation-"+uuid.NewString()+".test", recoveryVMApp); err != nil {
			t.Fatal(err)
		}
		if _, err := database.DB.Exec("INSERT INTO daemons(id,site_id,name,managed_kind,command,directory,user,instances,stop_seconds) VALUES (?,?,?,'laravel_queue',?,?,'fluxo',1,120)", id, id, "Activation reboot queue", command, recoveryVMApp); err != nil {
			t.Fatal(err)
		}
		definition := "[Unit]\nDescription=Fluxo disposable activation reboot queue\n[Service]\nType=simple\nUser=fluxo\nWorkingDirectory=" + recoveryVMApp + "\nExecStart=/usr/bin/" + command + "\nRestart=always\nRestartSec=1\nTimeoutStopSec=infinity\nSendSIGKILL=no\nKillSignal=SIGTERM\n[Install]\nWantedBy=multi-user.target\n"
		if err := writeDurableExclusive(filepath.Join("/etc/systemd/system", name), []byte(definition), 0644); err != nil {
			t.Fatal(err)
		}
		spec.Units = append(spec.Units, name)
		spec.Definitions = append(spec.Definitions, definition)
	}
	vmRun(t, "/usr/bin/systemctl", "daemon-reload")
	for _, name := range spec.Units {
		vmRun(t, "/usr/bin/systemctl", "enable", name)
	}
	vmRun(t, "/usr/bin/systemctl", "start", spec.Units[0])
	active := activationRebootReady(t, spec.Units[0])
	stopped, err := unitState(context.Background(), spec.Units[1])
	if err != nil || stopped.Active || len(stopped.PIDs) != 0 {
		t.Fatal("original stopped fixture is not empty", err)
	}
	impact, err := inspectImpact(context.Background(), "8.4")
	if err != nil || len(impact.BlockedReasons) != 0 {
		t.Fatal("fixture impact is not supported", err, impact.BlockedReasons)
	}
	ob, err := m.observePackages(context.Background(), "", "8.4")
	if err != nil || ob.JournalDirty || len(ob.UnrelatedPending) != 0 {
		t.Fatal("activation fixture requires clean existing packages", err)
	}
	manifest := archiveManifest{Schema: 1, Version: "8.4", PolicyHash: ob.PolicyHash}
	for _, p := range ob.Packages {
		if p.State != 6 {
			t.Fatal("activation fixture requires fully configured packages", p.Name)
		}
		manifest.Packages = append(manifest.Packages, Package{Name: p.Name, Architecture: p.Architecture, Installed: p.Installed, Candidate: p.Installed, Automatic: p.Automatic, Held: p.Held, Configured: true})
		manifest.After = append(manifest.After, PackageArchive{Name: p.Name, Architecture: p.Architecture, Version: p.Installed})
	}
	plan := Plan{ID: uuid.NewString(), Version: "8.4", CreatedAt: time.Now().Unix(), Impact: impact, Inventory: Inventory{Version: "8.4", Packages: manifest.Packages}}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.DB.Exec("INSERT INTO php_update_plans(id,version,payload,created_at) VALUES (?,?,?,?)", plan.ID, plan.Version, string(raw), plan.CreatedAt); err != nil {
		t.Fatal(err)
	}
	g := vmGeneration(t, m, []unitSnapshot{active, stopped})
	g.PlanID, g.Archives = plan.ID, manifest
	g.Original.DefaultCLI, err = filepath.EvalSymlinks("/usr/bin/php")
	if err != nil {
		t.Fatal(err)
	}
	g.Attempt = g.Original
	if _, err = database.DB.Exec("UPDATE php_update_operations SET version='8.4',plan_id=? WHERE id=?", plan.ID, g.Head); err != nil {
		t.Fatal(err)
	}
	if err = saveGeneration(g); err != nil {
		t.Fatal(err)
	}
	if err = m.drainConsumers(context.Background(), &g); err != nil {
		t.Fatal(err)
	}
	if err = m.startConsumers(context.Background(), &g); err != nil {
		t.Fatal(err)
	}
	activationRebootReady(t, spec.Units[0])
	// Inject a crash after the active unit's files were removed, but before
	// daemon-reload/journal deletion and before releasing the stopped unit.
	g.State, g.VerifiedGoal = "releasing", "after"
	if err = saveGeneration(g); err != nil {
		t.Fatal(err)
	}
	b, err := getBarrier(spec.Units[0])
	if err != nil || b.State != "started" {
		t.Fatal("active fixture did not reach started checkpoint", err)
	}
	b.State = "releasing"
	if err = saveBarrier(b); err != nil {
		t.Fatal(err)
	}
	if err = verifyBarrierFile(b.Path, b.Content); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(b.Path); err != nil {
		t.Fatal(err)
	}
	if err = syncDirectory(filepath.Dir(b.Path)); err != nil {
		t.Fatal(err)
	}
	if err = finish(g.Head, "interrupted", "activation reboot fixture: partial maintenance release"); err != nil {
		t.Fatal(err)
	}
	spec.GenerationID = g.ID
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		t.Fatal(err)
	}
	spec.Boot = strings.TrimSpace(string(boot))
	raw, err = json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err = writeDurableExclusive(activationRebootSpecPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if busy, e := Applying(); e != nil || !busy {
		t.Fatal("partial release lost admission before reboot", e)
	}
	t.Log("REBOOT_REQUESTED at partial activation release; retained", activationRebootSpecPath)
	vmRun(t, "/usr/bin/systemctl", "reboot")
}

func activationRebootReady(t *testing.T, name string) unitSnapshot {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		u, err := unitState(context.Background(), name)
		if err == nil && u.Active && u.MainPID > 0 {
			status, e := os.ReadFile(filepath.Join("/proc", strconv.Itoa(u.MainPID), "status"))
			if e == nil && queueHasTerminationHandler(string(status)) {
				return u
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("queue fixture did not become ready", name)
	return unitSnapshot{}
}

func TestVMResumeAfterRealPartialActivationReboot(t *testing.T) {
	requireRebootVM(t)
	if err := rootOwned(activationRebootSpecPath); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(activationRebootSpecPath)
	if err != nil {
		t.Fatal(err)
	}
	var spec activationRebootSpec
	if err = json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	if len(spec.Units) != 2 || len(spec.Definitions) != 2 || spec.Units[0] != unitName(9106000, 1) || spec.Units[1] != unitName(9106001, 1) || filepath.Dir(spec.DataDir) != "/var/lib/fluxo-php-recovery-acceptance-runs" {
		t.Fatal("unexpected activation reboot fixture spec")
	}
	if _, err = uuid.Parse(spec.GenerationID); err != nil {
		t.Fatal(err)
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil || spec.Boot == "" || strings.TrimSpace(string(boot)) == spec.Boot {
		t.Fatal("the VM did not actually reboot")
	}
	for i, name := range spec.Units {
		if err = verifyBarrierFile(filepath.Join("/etc/systemd/system", name), spec.Definitions[i]); err != nil {
			t.Fatal("fixture definition changed; refusing recovery/cleanup", err)
		}
	}
	if err = database.InitDB(filepath.Join(spec.DataDir, "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	defer database.DB.Close()
	m := NewManager(spec.DataDir)
	m.Launch = func(context.Context, Operation) error { t.Fatal("reconciliation replayed work"); return nil }
	m.reconcile(context.Background())
	g, err := getGeneration(spec.GenerationID)
	if err != nil || !g.Pending || g.State != "releasing" {
		t.Fatal("reboot lost partial-release generation", err)
	}
	if busy, e := Applying(); e != nil || !busy {
		t.Fatal("reboot released global maintenance admission", e)
	}
	b, err := getBarrier(spec.Units[1])
	if err != nil || verifyHeldBarrier(context.Background(), b) != nil {
		t.Fatal("original stopped unit lost its persistent boot hold", err)
	}
	vmRun(t, "/usr/bin/systemctl", "start", spec.Units[1])
	stopped, err := unitState(context.Background(), spec.Units[1])
	if err != nil || stopped.Active || len(stopped.PIDs) > 0 {
		t.Fatal("boot/manual activation started the original stopped unit", err)
	}
	review, err := m.ReviewRecovery(context.Background(), g.Head, "repair")
	if err != nil {
		t.Fatal(err)
	}
	m.Launch = func(ctx context.Context, o Operation) error {
		_, e := syscmd.Run(ctx, 15*time.Second, "/usr/bin/systemd-run", "--quiet", "--unit=fluxo-php-update-"+o.ID, "--property=Type=exec", "--property=Restart=no", "--property=TimeoutStopSec=infinity", "--setenv=FLUXO_ENV=prod", "--setenv=FLUXO_DATA_DIR="+spec.DataDir, "--", recoveryVMWorker, "php-update-worker", o.ID)
		return e
	}
	op, err := m.EnqueueRecovery(context.Background(), review.ID, uuid.NewString(), "activation-reboot-acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	if done := vmWaitOperation(t, op.ID); done.Status != "succeeded" {
		t.Fatal(done.Message)
	}
	activationRebootReady(t, spec.Units[0])
	stopped, err = unitState(context.Background(), spec.Units[1])
	if err != nil || stopped.Active || len(stopped.PIDs) > 0 {
		t.Fatal("Repair did not preserve the stopped consumer", err)
	}
	if busy, e := Applying(); e != nil || busy {
		t.Fatal("successful Repair left global admission held", e)
	}
	g, err = getGeneration(g.ID)
	if err != nil || g.Pending || g.State != "succeeded" {
		t.Fatal("Repair did not finish the generation", err)
	}
	// Stop only our empty-queue fixtures. Their unit definitions prohibit forced
	// killing, and no package or pre-existing unit cleanup is performed.
	for i, name := range spec.Units {
		if err = verifyBarrierFile(filepath.Join("/etc/systemd/system", name), spec.Definitions[i]); err != nil {
			t.Fatal(err)
		}
		vmRun(t, "/usr/bin/systemctl", "disable", "--now", name)
		u, e := observeUnit(context.Background(), name, "")
		if e != nil || u.Active || len(u.PIDs) > 0 {
			t.Fatal("fixture is not empty; retaining files", name, e)
		}
		if err = os.Remove(filepath.Join("/etc/systemd/system", name)); err != nil {
			t.Fatal(err)
		}
		// Remove the exact now-empty fixture directory; never recursively delete.
		if err = os.Remove(filepath.Join("/etc/systemd/system", name+".d")); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	vmRun(t, "/usr/bin/systemctl", "daemon-reload")
	if err = os.Rename(activationRebootSpecPath, activationRebootSpecPath+".completed-"+uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	t.Log("Real activation reboot and reviewed Repair verified; forensic database retained; packages unchanged")
}
