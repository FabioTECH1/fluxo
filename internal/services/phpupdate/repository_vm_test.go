package phpupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fluxo/internal/database"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func vmRepositoryWait(t *testing.T, id string) Operation {
	t.Helper()
	deadline := time.Now().Add(12 * time.Minute)
	phase := ""
	for time.Now().Before(deadline) {
		o, err := GetOperation(id)
		if err != nil {
			t.Fatal(err)
		}
		if o.Phase != phase {
			t.Log(o.Phase, o.Message)
			phase = o.Phase
		}
		if !o.Active() {
			if o.Status != "succeeded" {
				t.Log(o.Output)
				t.Fatal(o.Message)
			}
			return o
		}
		time.Sleep(time.Second)
	}
	t.Fatal("repository operation timed out")
	return Operation{}
}

// Explicit acceptance: source migration, real upstream security packages,
// retained offline rollback, then restoring the fixed revision. Test VM only.
func TestVMOfficialSuryRepositoryMigrationAndSecurityUpdateRecovery(t *testing.T) {
	requireRecoveryVM(t)
	m := vmWorkerStore(t)
	before := vmRun(t, "/usr/bin/dpkg-query", "-W", "php8.4-cli", "php8.4-fpm", "php8.3-cli", "php8.3-fpm")
	fpmStart := vmRun(t, "/usr/bin/systemctl", "show", "php8.4-fpm", "--property=MainPID,ExecMainStartTimestampMonotonic")
	o, err := m.EnqueueRepository(context.Background(), "repository_review", "", uuid.NewString(), "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	reviewed := vmRepositoryWait(t, o.ID)
	var review RepositoryReview
	if err = json.Unmarshal(reviewed.Result, &review); err != nil {
		t.Fatal(err)
	}
	t.Logf("Repository review: %+v", review)
	if review.ID == "" || review.Codename != "noble" || review.KeyFingerprint != "15058500A0235D97F5D10063B188E2B695BD4743" {
		t.Fatal("incomplete official repository review")
	}
	if after := vmRun(t, "/usr/bin/dpkg-query", "-W", "php8.4-cli", "php8.4-fpm", "php8.3-cli", "php8.3-fpm"); after != before {
		t.Fatal("review installed packages")
	}
	o, err = m.EnqueueRepository(context.Background(), "repository_apply", review.ID, uuid.NewString(), "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	vmRepositoryWait(t, o.ID)
	if after := vmRun(t, "/usr/bin/dpkg-query", "-W", "php8.4-cli", "php8.4-fpm", "php8.3-cli", "php8.3-fpm"); after != before {
		t.Fatal("repository switch installed packages")
	}
	if after := vmRun(t, "/usr/bin/systemctl", "show", "php8.4-fpm", "--property=MainPID,ExecMainStartTimestampMonotonic"); after != fpmStart {
		t.Fatal("repository switch restarted FPM")
	}
	if status := m.RepositoryStatus(context.Background()); status.State != "current" {
		t.Fatalf("repository status: %+v", status)
	}
	o, err = m.Enqueue(context.Background(), "check", "8.4", "", "", "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	vmRepositoryWait(t, o.ID)
	snapshots, err := Snapshots()
	if err != nil {
		t.Fatal(err)
	}
	var discovery Plan
	for _, s := range snapshots {
		if s.Version == "8.4" {
			if !s.UpdateAvailable || s.Plan == nil {
				t.Fatalf("official security update unavailable: %+v", s)
			}
			discovery = *s.Plan
		}
	}
	if discovery.ID == "" {
		t.Fatal("no 8.4 discovery")
	}
	for _, p := range discovery.Inventory.Packages {
		if strings.HasPrefix(p.Name, "php8.4-") && p.Source == "php8.4" && !strings.HasPrefix(p.Candidate, "8.4.27-") {
			t.Fatalf("unexpected security candidate %s %s", p.Name, p.Candidate)
		}
	}
	t.Logf("Security update: %d changes, rollback=%+v", len(discovery.Inventory.Changes), discovery.Rollback)
	if !discovery.Rollback.Available {
		t.Fatal("VM fixture old packages were not retained across migration", discovery.Rollback.Reason)
	}
	plan, err := m.ReviewPlan(context.Background(), discovery.ID)
	if err != nil || len(plan.Impact.BlockedReasons) > 0 {
		t.Fatalf("review: %v %v", err, plan.Impact.BlockedReasons)
	}
	o, err = m.Enqueue(context.Background(), "apply", "8.4", plan.ID, uuid.NewString(), "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	vmRepositoryWait(t, o.ID)
	installed := vmRun(t, "/usr/bin/php8.4", "-r", "echo PHP_VERSION;")
	if installed != "8.4.27" {
		t.Fatal("security runtime not activated", installed)
	}
	sourceAfter, err := os.ReadFile("/etc/apt/sources.list.d/fluxo-sury-php.sources")
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := m.ReviewRecovery(context.Background(), o.ID, "rollback")
	if err != nil {
		t.Fatal(err)
	}
	rollback, err := m.EnqueueRecovery(context.Background(), recovery.ID, uuid.NewString(), "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	vmRepositoryWait(t, rollback.ID)
	if got := vmRun(t, "/usr/bin/php8.4", "-r", "echo PHP_VERSION;"); got != "8.4.26" {
		t.Fatal("retained rollback failed", got)
	}
	if current, _ := os.ReadFile("/etc/apt/sources.list.d/fluxo-sury-php.sources"); string(current) != string(sourceAfter) {
		t.Fatal("package rollback rewound repository migration")
	}
	if _, err := m.ReviewRecovery(context.Background(), rollback.ID, "repair"); !errors.Is(err, ErrUnsupported) {
		t.Fatal("successful rollback unexpectedly offered repair", err)
	}
	vmSuryForwardActivationRepair(t, m)
	if got := vmRun(t, "/usr/bin/php8.4", "-r", "echo PHP_VERSION;"); got != "8.4.27" {
		t.Fatal("retained fixed package repair failed", got)
	}
	t.Log("Fixed PHP runtime restored; repositories and all acceptance evidence preserved")
}

// Resume the preserved successful migration/update/rollback acceptance without
// replaying repository migration or downgrading any unrelated fixture package.
func vmSuryPreservedStore(t *testing.T) *Manager {
	requireRecoveryVM(t)
	dir := os.Getenv("FLUXO_SURY_RESUME_DIR")
	if dir == "" {
		t.Skip("explicit preserved acceptance directory required")
	}
	if filepath.Dir(dir) != "/var/lib/fluxo-php-recovery-acceptance-runs" {
		t.Fatal("unsupported acceptance directory")
	}
	if err := rootOwned(filepath.Join(dir, "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	if err := database.InitDB(filepath.Join(dir, "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.DB.Close() })
	m := NewManager(dir)
	m.Launch = func(ctx context.Context, o Operation) error {
		_, err := syscmd.Run(ctx, 15*time.Second, "/usr/bin/systemd-run", "--quiet", "--unit=fluxo-php-update-"+o.ID, "--property=Type=exec", "--property=Restart=no", "--property=TimeoutStopSec=infinity", "--setenv=FLUXO_ENV=prod", "--setenv=FLUXO_DATA_DIR="+dir, "--", recoveryVMWorker, "php-update-worker", o.ID)
		return err
	}
	return m
}

func TestVMSuryResumeForwardActivationRepair(t *testing.T) {
	m := vmSuryPreservedStore(t)
	if vmRun(t, "/usr/bin/php8.4", "-r", "echo PHP_VERSION;") == "8.4.27" {
		var head string
		if err := database.DB.QueryRow("SELECT head_operation_id FROM php_update_generations WHERE version='8.4' ORDER BY created_at DESC LIMIT 1").Scan(&head); err != nil {
			t.Fatal(err)
		}
		p, err := m.ReviewRecovery(context.Background(), head, "rollback")
		if err != nil {
			t.Fatal(err)
		}
		o, err := m.EnqueueRecovery(context.Background(), p.ID, uuid.NewString(), "acceptance", "")
		if err != nil {
			t.Fatal(err)
		}
		vmRepositoryWait(t, o.ID)
	}
	vmSuryForwardActivationRepair(t, m)
}

func vmSuryForwardActivationRepair(t *testing.T, m *Manager) {
	t.Helper()
	plan := vmCheck(t, m)
	next, err := m.Enqueue(context.Background(), "apply", "8.4", plan.ID, uuid.NewString(), "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	unit := "fluxo-php-update-" + next.ID + ".service"
	frozen := false
	defer func() {
		if frozen {
			_, _ = syscmd.Run(context.Background(), 5*time.Second, "/usr/bin/systemctl", "kill", "--kill-whom=main", "--signal=SIGCONT", unit)
		}
	}()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		current, e := GetOperation(next.ID)
		if e != nil {
			t.Fatal(e)
		}
		if current.Phase == "applying" && strings.Contains(current.Output, "Unpacking php8.4-") {
			vmRun(t, "/usr/bin/systemctl", "kill", "--kill-whom=main", "--signal=SIGSTOP", unit)
			frozen = true
			break
		}
		if !current.Active() {
			t.Fatalf("forward transaction ended before fault injection: %s", current.Message)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !frozen {
		t.Fatal("package-child boundary not observed")
	}
	g, err := getGeneration(next.ID)
	if err != nil {
		t.Fatal(err)
	}
	ob, err := m.observePackages(context.Background(), "", "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if matched, e := validateRecoveryObservation(g, ob, "after"); e != nil || !matched {
		t.Fatalf("independent package transaction: %v %v", matched, e)
	}
	bad := "/etc/php/8.4/fpm/pool.d/zz-fluxo-sury-acceptance.conf"
	if err = writeDurableExclusive(bad, []byte("invalid acceptance FPM configuration\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(bad) })
	vmRun(t, "/usr/bin/systemctl", "kill", "--kill-whom=main", "--signal=SIGCONT", unit)
	frozen = false
	if done := vmWaitOperation(t, next.ID); done.Status == "succeeded" || !strings.Contains(done.Message, "FPM configuration") {
		t.Fatalf("activation failure was hidden: %+v", done)
	}
	if err = os.Remove(bad); err != nil {
		t.Fatal(err)
	}
	repair, err := m.ReviewRecovery(context.Background(), next.ID, "repair")
	if err != nil {
		t.Fatal(err)
	}
	o, err := m.EnqueueRecovery(context.Background(), repair.ID, uuid.NewString(), "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	vmRepositoryWait(t, o.ID)
	if got := vmRun(t, "/usr/bin/php8.4", "-r", "echo PHP_VERSION;"); got != "8.4.27" {
		t.Fatal("fixed runtime not restored", got)
	}
	if status := m.RepositoryStatus(context.Background()); status.State != "current" {
		t.Fatal("repair changed repository", status)
	}
	t.Log("Real forward update, honest FPM activation failure and manual repair verified with Sury repository retained")
}

// Proves that a recovery generation created before a reviewed automatic source
// policy migration remains usable, while retaining the new repository policy.
func TestVMSuryExistingGenerationRecoveryAfterAutomaticSetup(t *testing.T) {
	m := vmSuryPreservedStore(t)
	var head string
	if err := database.DB.QueryRow("SELECT head_operation_id FROM php_update_generations WHERE version='8.4' ORDER BY created_at DESC LIMIT 1").Scan(&head); err != nil {
		t.Fatal(err)
	}
	g, err := currentGeneration(head)
	if err != nil {
		t.Fatal(err)
	}
	ob, err := m.observePackages(context.Background(), "", "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if ob.PolicyHash == g.Archives.PolicyHash || !recoveryPolicyAuthorized(g, ob.PolicyHash) {
		t.Fatal("fixture did not establish exact migrated recovery authorization")
	}
	policy := ob.PolicyHash
	p, err := m.ReviewRecovery(context.Background(), head, "rollback")
	if err != nil {
		t.Fatal(err)
	}
	o, err := m.EnqueueRecovery(context.Background(), p.ID, uuid.NewString(), "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	vmRepositoryWait(t, o.ID)
	if got := vmRun(t, "/usr/bin/php8.4", "-r", "echo PHP_VERSION;"); got != "8.4.26" {
		t.Fatal("old generation rollback failed", got)
	}
	plan := vmCheck(t, m)
	o, err = m.Enqueue(context.Background(), "apply", "8.4", plan.ID, uuid.NewString(), "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	vmRepositoryWait(t, o.ID)
	ob, err = m.observePackages(context.Background(), "", "8.4")
	if err != nil || ob.PolicyHash != policy {
		t.Fatal("package recovery changed migrated sources", err)
	}
	if got := vmRun(t, "/usr/bin/php8.4", "-r", "echo PHP_VERSION;"); got != "8.4.27" {
		t.Fatal("fixed runtime not restored", got)
	}
	t.Log("PASS: pre-migration recovery generation rolled back using exact policy authorization; new source policy preserved; fixed runtime restored")
}

// Forced worker loss after key publication must keep its original intent and
// reconcile that exact journal on retry, without installing PHP or restarting FPM.
func TestVMSuryInterruptedRepositoryPublicationResumesOriginalPlan(t *testing.T) {
	m := vmSuryPreservedStore(t)
	fpm := vmRun(t, "/usr/bin/systemctl", "show", "php8.4-fpm", "--property=MainPID,ExecMainStartTimestampMonotonic")
	inventory := vmRun(t, "/usr/bin/dpkg-query", "-W", "php8.4-cli", "php8.4-fpm")
	var pending string
	_ = database.DB.QueryRow("SELECT plan_id FROM php_repository_migrations WHERE state='pending' LIMIT 1").Scan(&pending)
	if pending != "" {
		vmSuryResumePublication(t, m, pending)
		if got := vmRun(t, "/usr/bin/dpkg-query", "-W", "php8.4-cli", "php8.4-fpm"); got != inventory {
			t.Fatal("resume changed PHP inventory")
		}
		if got := vmRun(t, "/usr/bin/systemctl", "show", "php8.4-fpm", "--property=MainPID,ExecMainStartTimestampMonotonic"); got != fpm {
			t.Fatal("resume restarted FPM")
		}
		t.Log("PASS: preserved actual killed-publication intent resumed with no PHP/FPM change")
		return
	}
	key := "/usr/share/keyrings/fluxo-sury-archive.gpg"
	before, err := os.ReadFile(key)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(m.DataDir, "acceptance-key-before-interruption.gpg")
	if err = os.Rename(key, backup); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, e := os.Stat(key); os.IsNotExist(e) {
			_ = os.Rename(backup, key)
		}
	})
	op, err := m.EnqueueRepository(context.Background(), "repository_review", "", uuid.NewString(), "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	done := vmRepositoryWait(t, op.ID)
	var review RepositoryReview
	if err = json.Unmarshal(done.Result, &review); err != nil {
		t.Fatal(err)
	}
	op, err = m.EnqueueRepository(context.Background(), "repository_apply", review.ID, uuid.NewString(), "acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	killed := false
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if current, e := os.ReadFile(key); e == nil && string(current) == string(before) {
			unit := "fluxo-php-update-" + op.ID + ".service"
			vmRun(t, "/usr/bin/systemctl", "kill", "--kill-whom=main", "--signal=SIGSTOP", unit)
			t.Cleanup(func() {
				_, _ = syscmd.Run(context.Background(), 5*time.Second, "/usr/bin/systemctl", "kill", "--kill-whom=main", "--signal=SIGCONT", unit)
			})
			var countBefore, countAfter int
			_ = database.DB.QueryRow("SELECT count(*) FROM php_update_operations").Scan(&countBefore)
			started := time.Now()
			_, liveErr := syscmd.RunEnv(context.Background(), 3*time.Second, []string{"FLUXO_ENV=prod", "FLUXO_DATA_DIR=" + m.DataDir}, recoveryVMWorker, "php-repository-setup")
			_ = database.DB.QueryRow("SELECT count(*) FROM php_update_operations").Scan(&countAfter)
			if liveErr == nil || time.Since(started) < 2*time.Second || countAfter != countBefore {
				t.Fatalf("installer did not wait for live supervisor without duplicating work: %v %d %d", liveErr, countBefore, countAfter)
			}
			_, _ = syscmd.Run(context.Background(), 10*time.Second, "/usr/bin/systemctl", "kill", "--kill-whom=all", "--signal=SIGKILL", "fluxo-php-update-"+op.ID+".service")
			killed = true
			break
		}
		current, e := GetOperation(op.ID)
		if e != nil || !current.Active() {
			t.Fatalf("publication ended before interruption: %v %+v", e, current)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !killed {
		t.Fatal("publication boundary not observed")
	}
	vmSuryResumePublication(t, m, review.ID)
	if got := vmRun(t, "/usr/bin/dpkg-query", "-W", "php8.4-cli", "php8.4-fpm"); got != inventory {
		t.Fatal("repository resume changed PHP packages")
	}
	if got := vmRun(t, "/usr/bin/systemctl", "show", "php8.4-fpm", "--property=MainPID,ExecMainStartTimestampMonotonic"); got != fpm {
		t.Fatal("repository resume restarted FPM")
	}
	t.Log("PASS: missing signing key triggered repair; killed publication retained original intent; exact journal resumed with no PHP/FPM change")
}

func vmSuryResumePublication(t *testing.T, m *Manager, planID string) {
	t.Helper()
	// Exercise the real installer entry point with the daemon stopped. There is
	// deliberately no test-side Manager.reconcile call here.
	output, err := syscmd.RunEnv(context.Background(), 5*time.Minute, []string{"FLUXO_ENV=prod", "FLUXO_DATA_DIR=" + m.DataDir}, recoveryVMWorker, "php-repository-setup")
	if err != nil {
		t.Fatalf("actual installer retry: %v\n%s", err, output)
	}
	t.Log(output)
	var state string
	if err = database.DB.QueryRow("SELECT state FROM php_repository_migrations WHERE plan_id=?", planID).Scan(&state); err != nil || state != "committed" {
		t.Fatalf("original intent not committed: %s %v", state, err)
	}
	if status := m.RepositoryStatus(context.Background()); status.State != "current" {
		t.Fatal(status)
	}
}
