package phpupdate

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

const recoveryVMApp = "/home/fluxo/php-recovery-acceptance"
const recoveryVMWorker = "/usr/local/bin/fluxo-php-recovery-test"

func vmWorkerStore(t *testing.T) *Manager {
	t.Helper()
	requireRecoveryVM(t)
	base := "/var/lib/fluxo-php-recovery-acceptance-runs"
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(base, "run-")
	if err != nil {
		t.Fatal(err)
	}
	if err = database.InitDB(filepath.Join(dir, "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.DB.Close() })
	if err = rootOwned(recoveryVMWorker); err != nil {
		t.Fatal(err)
	}
	m := NewManager(dir)
	m.Launch = func(ctx context.Context, o Operation) error {
		_, err := syscmd.Run(ctx, 15*time.Second, "/usr/bin/systemd-run", "--quiet", "--unit=fluxo-php-update-"+o.ID, "--property=Type=exec", "--property=Restart=no", "--property=TimeoutStopSec=infinity", "--setenv=FLUXO_ENV=prod", "--setenv=FLUXO_DATA_DIR="+dir, "--", recoveryVMWorker, "php-update-worker", o.ID)
		return err
	}
	t.Logf("Preserved acceptance state: %s", dir)
	return m
}
func vmWaitOperation(t *testing.T, id string) Operation {
	t.Helper()
	deadline := time.Now().Add(4 * time.Minute)
	last := ""
	for time.Now().Before(deadline) {
		o, err := GetOperation(id)
		if err != nil {
			t.Fatal(err)
		}
		if o.Phase != last {
			t.Logf("%s %s: %s", o.Kind, o.Phase, o.Message)
			last = o.Phase
		}
		if !o.Active() {
			if o.Status != "succeeded" {
				output := o.Output
				if len(output) > 3000 {
					output = output[len(output)-3000:]
				}
				t.Logf("Diagnostic output tail: %s", output)
			}
			return o
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("operation %s exceeded acceptance deadline", id)
	return Operation{}
}
func vmCheck(t *testing.T, m *Manager) Plan {
	t.Helper()
	o, err := m.Enqueue(context.Background(), "check", "8.4", "", "", "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	if done := vmWaitOperation(t, o.ID); done.Status != "succeeded" {
		t.Fatal(done.Message)
	}
	snapshots, err := Snapshots()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range snapshots {
		if s.Version == "8.4" {
			if !s.UpdateAvailable || s.Plan == nil {
				t.Fatalf("expected update availability; state=%s reasons=%v", s.State, s.BlockedReasons)
			}
			review, err := m.ReviewPlan(context.Background(), s.Plan.ID)
			if err != nil || len(review.Impact.BlockedReasons) > 0 {
				t.Fatalf("installation review: %v %v", err, review.Impact.BlockedReasons)
			}
			return review
		}
	}
	t.Fatal("no PHP 8.4 snapshot")
	return Plan{}
}
func vmPHP(t *testing.T, code string, args ...string) string {
	t.Helper()
	argv := []string{"-r", `require 'vendor/autoload.php';$app=require 'bootstrap/app.php';$app->make(Illuminate\Contracts\Console\Kernel::class)->bootstrap();` + code, "--"}
	argv = append(argv, args...)
	out, err := syscmd.RunApplicationProbe(context.Background(), 20*time.Second, "fluxo", recoveryVMApp, "/usr/bin/php8.4", argv...)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out)
}
func vmFixtureEnv(t *testing.T, values map[string]string) {
	t.Helper()
	path := filepath.Join(recoveryVMApp, ".env")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(string(raw), "\n") {
		key, _, _ := strings.Cut(line, "=")
		if _, replace := values[key]; !replace {
			lines = append(lines, line)
		}
	}
	for key, value := range values {
		lines = append(lines, key+"="+value)
	}
	if err = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	vmPHP(t, `$app->make(Illuminate\Contracts\Console\Kernel::class)->call('config:clear');`)
}
func vmCloneExistingConsumers(t *testing.T) {
	t.Helper()
	old, err := sql.Open("sqlite", "/var/lib/fluxo-php-patch-test/fluxo.db")
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	rows, err := old.Query(`SELECT id,domain,path,php_version,COALESCE(deployment_strategy,'standard'),COALESCE(app_type,'laravel'),COALESCE(app_port,0) FROM sites WHERE id IN (SELECT site_id FROM daemons)`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, port int
		var domain, path, php, strategy, kind string
		if err = rows.Scan(&id, &domain, &path, &php, &strategy, &kind, &port); err != nil {
			t.Fatal(err)
		}
		if _, err = database.DB.Exec("INSERT INTO sites(id,domain,path,php_version,deployment_strategy,app_type,app_port) VALUES (?,?,?,?,?,?,?)", id, domain, path, php, strategy, kind, port); err != nil {
			t.Fatal(err)
		}
	}
	rows.Close()
	rows, err = old.Query(`SELECT id,site_id,name,managed_kind,command,directory,user,instances,stop_seconds FROM daemons`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, sid, instances, stop int
		var name, kind, command, directory, user string
		if err = rows.Scan(&id, &sid, &name, &kind, &command, &directory, &user, &instances, &stop); err != nil {
			t.Fatal(err)
		}
		if _, err = database.DB.Exec("INSERT INTO daemons(id,site_id,name,managed_kind,command,directory,user,instances,stop_seconds) VALUES (?,?,?,?,?,?,?,?,?)", id, sid, name, kind, command, directory, user, instances, stop); err != nil {
			t.Fatal(err)
		}
	}
	rows.Close()
}
func vmHorizon(t *testing.T, queue string) Consumer {
	t.Helper()
	vmFixtureEnv(t, map[string]string{"PHP_RECOVERY_QUEUE": queue, "RECOVERY_FAST_TERMINATION": "true"})
	c := Consumer{ID: 9101000, SiteID: 9101000, Name: "Recovery Horizon", Kind: "laravel_horizon", Command: "php8.4 artisan horizon", Directory: recoveryVMApp, Instances: 2, StopSeconds: 2}
	if _, err := database.DB.Exec("INSERT INTO sites(id,domain,path,php_version,app_type,deployment_strategy) VALUES (?,? ,?,'8.4','laravel','standard')", c.SiteID, "recovery-acceptance.test", c.Directory); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("INSERT INTO daemons(id,site_id,name,managed_kind,command,directory,user,instances,stop_seconds) VALUES (?,?,?,?,?,?,'fluxo',?,?)", c.ID, c.SiteID, c.Name, c.Kind, c.Command, c.Directory, c.Instances, c.StopSeconds); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		vmUnit(t, unitName(c.ID, i), "/usr/bin/"+c.Command, c.Directory)
	}
	vmRun(t, "/usr/bin/systemctl", "start", unitName(c.ID, 1))
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		u, e := unitState(context.Background(), unitName(c.ID, 1))
		if e == nil {
			if e = validateUnitProcesses(context.Background(), "8.4", &c, u); e == nil && len(u.PIDs) >= 3 {
				return c
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	u, err := unitState(context.Background(), unitName(c.ID, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err = validateUnitProcesses(context.Background(), "8.4", &c, u); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("Horizon workers not ready: %+v", u)
	return c
}
func vmJobEvent(t *testing.T, label, event string) bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(recoveryVMApp, "storage/app/recovery-events.jsonl"))
	if os.IsNotExist(err) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		var item struct {
			Label string `json:"label"`
			Event string `json:"event"`
		}
		if json.Unmarshal([]byte(line), &item) == nil && item.Label == label && item.Event == event {
			return true
		}
	}
	return false
}
func vmApt(t *testing.T, specs []string) {
	t.Helper()
	args := []string{"-o", "DPkg::Lock::Timeout=120", "-o", "Dpkg::Options::=--force-confold", "--allow-downgrades", "--no-install-recommends", "-y", "install"}
	args = append(args, specs...)
	var output strings.Builder
	err := syscmd.RunMaintenance(context.Background(), "", &output, []string{"DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_MODE=l", "APT_LISTCHANGES_FRONTEND=none"}, "/usr/bin/apt-get", args...)
	if err != nil {
		t.Fatalf("fixture package setup failed: %v\n%s", err, output.String())
	}
}
func vmRestoreMarks(t *testing.T, packages []Package) {
	t.Helper()
	var automatic, manual []string
	for _, p := range packages {
		if p.Automatic {
			automatic = append(automatic, p.Name)
		} else {
			manual = append(manual, p.Name)
		}
	}
	if len(automatic) > 0 {
		vmRun(t, "/usr/bin/apt-mark", append([]string{"auto"}, automatic...)...)
	}
	if len(manual) > 0 {
		vmRun(t, "/usr/bin/apt-mark", append([]string{"manual"}, manual...)...)
	}
}
func vmDowngradeFixture(t *testing.T, m *Manager, retain ...bool) []Package {
	t.Helper()
	inventories, err := bridge(context.Background(), "", map[string]any{"action": "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	var inventory Inventory
	for _, inv := range inventories {
		if inv.Version == "8.4" {
			inventory = inv
		}
	}
	if len(inventory.Packages) == 0 {
		t.Fatal("missing real PHP package inventory")
	}
	var old, restore []string
	for _, p := range inventory.Packages {
		if p.Held || !p.Configured {
			t.Fatalf("fixture has held or damaged package %s", p.Name)
		}
		if p.Source != "php8.4" {
			if p.Candidate != p.Installed {
				t.Fatalf("refresh extension fixture first: %s", p.Name)
			}
			continue
		}
		version := strings.TrimSpace(vmRun(t, "/usr/bin/dpkg-deb", "-f", filepath.Join("/var/lib/fluxo-php-recovery-fixture-repo", p.Name+"-old.deb"), "Version"))
		old = append(old, p.Name+"="+version)
		restore = append(restore, p.Name+"="+p.Installed)
	}
	if len(old) < 3 {
		t.Fatal("incomplete authenticated downgrade fixture")
	}
	if len(retain) == 0 || !retain[0] {
		t.Cleanup(func() { vmApt(t, restore); vmRestoreMarks(t, inventory.Packages) })
	}
	vmApt(t, old)
	vmRestoreMarks(t, inventory.Packages)
	return inventory.Packages
}

func TestVMPHPUpdateRollbackAndRepeatedRepairWithHorizon(t *testing.T) {
	requireRecoveryVM(t)
	if os.Getenv("FLUXO_PHP_UPDATE_PACKAGE_VM_TEST") != "1" {
		t.Skip("explicit real package-cycle opt-in required")
	}
	m := vmWorkerStore(t)
	vmCloneExistingConsumers(t)
	// Restore the pre-existing disposable queue fixtures even on a failed
	// acceptance assertion. This is test cleanup, never production recovery.
	t.Cleanup(func() {
		rows, err := database.DB.Query("SELECT payload FROM php_update_generations")
		if err != nil {
			return
		}
		var generations []recoveryGeneration
		for rows.Next() {
			var raw string
			var g recoveryGeneration
			if rows.Scan(&raw) == nil && json.Unmarshal([]byte(raw), &g) == nil {
				generations = append(generations, g)
			}
		}
		rows.Close()
		for _, g := range generations {
			_ = m.releaseBarriers(context.Background(), g)
		}
	})
	vmDowngradeFixture(t, m)
	queue := "fluxo-recovery-" + uuid.NewString()
	consumer := vmHorizon(t, queue)
	plan := vmCheck(t, m)
	if !plan.Rollback.Available {
		t.Fatalf("authenticated prior package set unavailable: %s", plan.Rollback.Reason)
	}
	label := "long-" + uuid.NewString()
	vmPHP(t, `App\Jobs\RecoveryDrainJob::dispatch(45,$argv[1])->onQueue($argv[2]);`, label, queue)
	deadline := time.Now().Add(10 * time.Second)
	for !vmJobEvent(t, label, "started") {
		if time.Now().After(deadline) {
			t.Fatal("long Horizon job did not start")
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Prepare a normal Laravel payload, then enqueue additional arrivals without
	// launching unmanaged PHP processes during package maintenance.
	vmPHP(t, `App\Jobs\RecoveryDrainJob::dispatch(1,$argv[1])->onQueue($argv[2]);`, "backlog-"+uuid.NewString(), queue)
	key := "fluxo_php_recovery_queues:" + queue
	payload := strings.TrimSpace(vmRun(t, "/usr/bin/redis-cli", "--raw", "LINDEX", key, "0"))
	if payload == "" {
		t.Fatal("missing queued fixture payload")
	}
	stopProducer := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(stopProducer) }) }
	t.Cleanup(stop)
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stopProducer:
				return
			case <-tick.C:
				_, _ = syscmd.Run(context.Background(), 2*time.Second, "/usr/bin/redis-cli", "LPUSH", key, payload)
			}
		}
	}()
	op, err := m.Enqueue(context.Background(), "apply", "8.4", plan.ID, uuid.NewString(), "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Minute)
	seenPackages := false
	for time.Now().Before(deadline) {
		current, e := GetOperation(op.ID)
		if e != nil {
			t.Fatal(e)
		}
		if current.Phase == "applying" || current.Checkpoint == "package_transaction" || current.Checkpoint == "packages_updated" {
			seenPackages = true
			break
		}
		if !current.Active() {
			t.Fatalf("update failed before package mutation: %s\n%s", current.Message, current.Output)
		}
		time.Sleep(50 * time.Millisecond)
	}
	stop()
	<-producerDone
	if !seenPackages || !vmJobEvent(t, label, "finished") {
		t.Fatal("package mutation began before in-flight Horizon job finished")
	}
	if length := strings.TrimSpace(vmRun(t, "/usr/bin/redis-cli", "LLEN", key)); length == "0" {
		t.Fatal("test did not retain incoming queue backlog")
	}
	if done := vmWaitOperation(t, op.ID); done.Status != "succeeded" {
		t.Fatal(done.Message)
	}
	if state, e := unitState(context.Background(), unitName(consumer.ID, 2)); e != nil || state.Active {
		t.Fatalf("stopped Horizon instance changed state: %+v %v", state, e)
	}
	g, err := getGeneration(op.ID)
	if err != nil || !g.Archives.RollbackAvailable || g.Pending {
		t.Fatalf("verified update lacks complete recovery evidence: %+v %v", g, err)
	}
	// Corruption must be caught during review, before a consumer is stopped.
	directory, err := m.archiveDirectory(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(directory, g.Archives.Before[0].SHA256+".deb")
	originalBytes, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(archive, []byte("interrupted/corrupt archive fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	_, reviewErr := m.ReviewRecovery(context.Background(), op.ID, "rollback")
	if err = os.WriteFile(archive, originalBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if reviewErr == nil {
		t.Fatal("corrupt retained archive was accepted for rollback")
	}
	var barriers int
	if err = database.DB.QueryRow("SELECT count(*) FROM php_update_barriers").Scan(&barriers); err != nil || barriers != 0 {
		t.Fatalf("corrupt archive review drained consumers: %d %v", barriers, err)
	}
	// An abandoned partial copy is unreferenced. It must not become the digest
	// destination or poison a later reviewed rollback.
	partialDir := filepath.Join(directory, "recovery-before")
	if err = os.MkdirAll(partialDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(partialDir, ".archive-interrupted-fixture"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	rollback, err := m.ReviewRecovery(context.Background(), op.ID, "rollback")
	if err != nil {
		t.Fatal(err)
	}
	rollbackOp, err := m.EnqueueRecovery(context.Background(), rollback.ID, uuid.NewString(), "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	if done := vmWaitOperation(t, rollbackOp.ID); done.Status != "succeeded" {
		t.Fatal(done.Message)
	}
	g, err = getGeneration(op.ID)
	if err != nil || g.State != "rolled_back" || g.Pending {
		t.Fatalf("rollback did not verify: %+v %v", g, err)
	}
	// Reapply after rollback, but freeze only the outer supervisor after its
	// independent package child starts. This safely injects an activation fault.
	plan = vmCheck(t, m)
	next, err := m.Enqueue(context.Background(), "apply", "8.4", plan.ID, uuid.NewString(), "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	name := "fluxo-php-update-" + next.ID + ".service"
	frozen := false
	defer func() {
		if frozen {
			_, _ = syscmd.Run(context.Background(), 5*time.Second, "/usr/bin/systemctl", "kill", "--kill-whom=main", "--signal=SIGCONT", name)
		}
	}()
	deadline = time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		current, e := GetOperation(next.ID)
		if e != nil {
			t.Fatal(e)
		}
		if current.Phase == "applying" && strings.Contains(current.Output, "Unpacking php8.4-") {
			vmRun(t, "/usr/bin/systemctl", "kill", "--kill-whom=main", "--signal=SIGSTOP", name)
			frozen = true
			break
		}
		if !current.Active() {
			t.Fatalf("forward update failed before fault injection: %s\n%s", current.Message, current.Output)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !frozen {
		t.Fatal("could not pause outer supervisor at the package-child boundary")
	}
	g, err = getGeneration(next.ID)
	if err != nil {
		t.Fatal(err)
	}
	ob, err := m.observePackages(context.Background(), "", "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if matched, e := validateRecoveryObservation(g, ob, "after"); e != nil || !matched {
		t.Fatalf("independent package child did not finish: %v %v", matched, e)
	}
	badConfig := "/etc/php/8.4/fpm/pool.d/zz-fluxo-recovery-acceptance.conf"
	if err = writeDurableExclusive(badConfig, []byte("invalid acceptance FPM configuration\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(badConfig) })
	vmRun(t, "/usr/bin/systemctl", "kill", "--kill-whom=main", "--signal=SIGCONT", name)
	frozen = false
	if done := vmWaitOperation(t, next.ID); done.Status == "succeeded" || !strings.Contains(done.Message, "FPM configuration") {
		t.Fatalf("FPM failure was not reported honestly: %+v", done)
	}
	// A clean dpkg unpack produces a real, branch-local partial configuration.
	// Repair may replay only the authenticated retained set, never fix unrelated
	// packages or run broad dpkg configuration commands.
	var cli string
	for _, p := range g.Archives.After {
		if p.Name == "php8.4-cli" {
			directory, e := m.archiveDirectory(g.ID)
			if e != nil {
				t.Fatal(e)
			}
			cli = filepath.Join(directory, p.SHA256+".deb")
		}
	}
	if cli == "" {
		t.Fatal("retained CLI archive missing")
	}
	vmRun(t, "/usr/bin/dpkg", "--unpack", cli)
	repair, err := m.ReviewRecovery(context.Background(), next.ID, "repair")
	if err != nil {
		t.Fatal(err)
	}
	repairOp, err := m.EnqueueRecovery(context.Background(), repair.ID, uuid.NewString(), "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	if done := vmWaitOperation(t, repairOp.ID); done.Status == "succeeded" || !strings.Contains(done.Message, "FPM configuration") {
		t.Fatalf("invalid FPM config was hidden during package repair: %+v", done)
	}
	ob, err = m.observePackages(context.Background(), "", "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if matched, e := validateRecoveryObservation(g, ob, "after"); e != nil || !matched {
		t.Fatalf("scoped package repair did not finish: %v %v", matched, e)
	}
	if err = os.Remove(badConfig); err != nil {
		t.Fatal(err)
	}
	repair, err = m.ReviewRecovery(context.Background(), repairOp.ID, "repair")
	if err != nil {
		t.Fatal(err)
	}
	requestID := uuid.NewString()
	final, err := m.EnqueueRecovery(context.Background(), repair.ID, requestID, "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	if done := vmWaitOperation(t, final.ID); done.Status != "succeeded" {
		t.Fatal(done.Message)
	}
	duplicate, err := m.EnqueueRecovery(context.Background(), repair.ID, requestID, "acceptance", "")
	if err != nil || duplicate.ID != final.ID {
		t.Fatalf("completed recovery retry was not idempotent: %v", err)
	}
	if busy, e := Applying(); e != nil || busy {
		t.Fatalf("successful repeated repair left maintenance blocked: %v %v", busy, e)
	}
	t.Logf("Verified real Horizon drain with continued arrivals, authenticated forward/rollback/forward, scoped unpacked-package repair and repeated activation repair; final operation %s", final.ID)
}
