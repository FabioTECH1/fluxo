package phpupdate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/packages"
	"fluxo/internal/syscmd"
	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

func vmArchiveRetentionStore(t *testing.T) *Manager {
	t.Helper()
	requireRecoveryVM(t)
	base := "/var/lib/fluxo-php-retention-acceptance"
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
	t.Setenv("FLUXO_ENV", "prod")
	t.Setenv("FLUXO_DATA_DIR", dir)
	t.Logf("Preserved archive retention evidence: %s", dir)
	return NewManager(dir)
}
func vmPruneArchivePass(t *testing.T, m *Manager) (int, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	release, err := packages.Lifecycle(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	unlock, err := packages.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	return m.pruneArchivesLocked(ctx)
}
func TestVMArchiveRetentionPrunesEmptyHistoryAndPreservesRecoveryDependencies(t *testing.T) {
	m := vmArchiveRetentionStore(t)
	for i := 0; i < 40; i++ {
		item := retentionFixture(t, m, "8.4", uuid.NewString())
		dir, _ := m.archiveDirectory(item.Value.ID)
		for _, a := range append(item.Value.Archives.Before, item.Value.Archives.After...) {
			if err := os.Remove(filepath.Join(dir, a.SHA256+".deb")); err != nil {
				t.Fatal(err)
			}
		}
	}
	old := retentionFixture(t, m, "8.4", "old")
	pinned := retentionFixture(t, m, "8.3", "pinned")
	copyRetentionSnapshot(t, m, "20261021T220000Z-901")
	latest := retentionFixture(t, m, "8.4", "latest")
	retentionFixture(t, m, "8.3", "latest-other")
	// This snapshot pins old; cleanup must leave all its files in place.
	if n, err := vmPruneArchivePass(t, m); err != nil || n != 0 {
		t.Fatalf("snapshot protection failed: %d %v", n, err)
	}
	// Preserve the snapshot itself, outside the installer retention namespace.
	if err := os.Rename(filepath.Join(m.DataDir, "upgrades"), filepath.Join(m.DataDir, "acceptance-preserved-upgrades")); err != nil {
		t.Fatal(err)
	}
	if n, err := vmPruneArchivePass(t, m); err != nil || n != 2 {
		t.Fatalf("empty history starved real cleanup: %d %v", n, err)
	}
	dir, _ := m.archiveDirectory(old.Value.ID)
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	s, err := openArchiveRetentionStore(m.DataDir, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, item := range []archiveGeneration{latest, pinned} {
		set, e := s.inspectGeneration(context.Background(), item.Value, true)
		if e != nil {
			t.Fatal(e)
		}
		set.Close()
	}
	// Unresolved legacy transaction evidence blocks the next branch's cleanup.
	if _, err = database.DB.Exec(`INSERT INTO php_update_operations(id,kind,version,status,checkpoint,created_at,updated_at) VALUES (?,'verify','8.4','interrupted','package_transaction',1,1)`, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if n, e := vmPruneArchivePass(t, m); n != 0 || !errors.Is(e, errArchiveCleanupDeferred) {
		t.Fatalf("unresolved evidence lost protection: %d %v", n, e)
	}
	t.Log("Full root cleanup, snapshot pinning, long empty history, branch isolation and unresolved-transaction protection passed")
}

func TestVMArchiveRetentionRejectsRealBindMount(t *testing.T) {
	m := vmArchiveRetentionStore(t)
	old := retentionFixture(t, m, "8.4", "old")
	retentionFixture(t, m, "8.4", "latest")
	dir, _ := m.archiveDirectory(old.Value.ID)
	path := filepath.Join(dir, old.Value.Archives.Before[0].SHA256+".deb")
	outside := filepath.Join(m.DataDir, "outside-package")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(outside, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err = unix.Mount(outside, path, "", unix.MS_BIND, ""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Unmount(path, 0) })
	if n, e := vmPruneArchivePass(t, m); e == nil || n != 0 {
		t.Fatalf("bind-mounted file entered deletion batch: %d %v", n, e)
	}
	if err = unix.Unmount(path, 0); err != nil {
		t.Fatal(err)
	}
	if n, e := vmPruneArchivePass(t, m); e != nil || n != 2 {
		t.Fatalf("safe retry after unmount: %d %v", n, e)
	}
	if got, e := os.ReadFile(outside); e != nil || string(got) != string(b) {
		t.Fatal("bind-mount target damaged")
	}
}

func TestVMArchiveRetentionInterruptedUnlinkRetriesFromManifest(t *testing.T) {
	m := vmArchiveRetentionStore(t)
	old := retentionFixture(t, m, "8.4", "old")
	retentionFixture(t, m, "8.4", "latest")
	s, err := openArchiveRetentionStore(m.DataDir, 0)
	if err != nil {
		t.Fatal(err)
	}
	set, err := s.inspectGeneration(context.Background(), old.Value, false)
	if err != nil {
		t.Fatal(err)
	}
	if n, _, e := set.UnlinkBatch(context.Background(), 1); e != nil || n != 1 {
		t.Fatalf("interrupted first unlink: %d %v", n, e)
	}
	set.Close()
	s.Close()
	// Reopen both SQLite and descriptors, as after a process interruption.
	database.DB.Close()
	if err = database.InitDB(filepath.Join(m.DataDir, "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	if n, e := vmPruneArchivePass(t, m); e != nil || n != 1 {
		t.Fatalf("resumed cleanup: %d %v", n, e)
	}
	if n, e := vmPruneArchivePass(t, m); e != nil || n != 0 {
		t.Fatalf("cleanup not idempotent: %d %v", n, e)
	}
}

func TestVMArchiveRetentionTwoUpdatesRepairAndOfflineRollback(t *testing.T) {
	requireRecoveryVM(t)
	if os.Getenv("FLUXO_PHP_UPDATE_PACKAGE_VM_TEST") != "1" {
		t.Skip("explicit real package-cycle opt-in required")
	}
	m := vmWorkerStore(t)
	t.Setenv("FLUXO_ENV", "prod")
	t.Setenv("FLUXO_DATA_DIR", m.DataDir)
	worker := "/usr/local/bin/fluxo-php-retention-test"
	if err := rootOwned(worker); err != nil {
		t.Fatal(err)
	}
	m.Launch = func(ctx context.Context, o Operation) error {
		_, err := syscmd.Run(ctx, 15*time.Second, "/usr/bin/systemd-run", "--quiet", "--unit=fluxo-php-update-"+o.ID, "--property=Type=exec", "--property=Restart=no", "--property=TimeoutStopSec=infinity", "--setenv=FLUXO_ENV=prod", "--setenv=FLUXO_DATA_DIR="+m.DataDir, "--", worker, "php-update-worker", o.ID)
		return err
	}
	vmCloneExistingConsumers(t)
	vmRetentionReuseRepositoryEvidence(t, m)
	// A failed acceptance assertion must not leave existing VM consumers held.
	// This cleanup is registered before package restoration so restored packages
	// are in place before the original service definitions are reactivated.
	t.Cleanup(func() {
		rows, e := database.DB.Query("SELECT payload FROM php_update_generations")
		if e != nil {
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
			for _, u := range g.Original.Units {
				if u.Active {
					_, _ = syscmd.Run(context.Background(), 15*time.Second, "/usr/bin/systemctl", "start", "--no-block", u.Name)
				}
			}
		}
	})
	// Restore package revisions/marks using the existing acceptance helper.
	vmRetentionInstallMiddleRevision(t, m)
	firstPlan := vmCheck(t, m)
	first, err := m.Enqueue(context.Background(), "apply", "8.4", firstPlan.ID, uuid.NewString(), "retention acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	if done := vmWaitOperation(t, first.ID); done.Status != "succeeded" {
		t.Fatal(done.Message)
	}
	firstGeneration, err := getGeneration(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	vmRetentionPrepareNextRevision(t, m, first.ID)
	secondPlan := vmCheck(t, m)
	second, err := m.Enqueue(context.Background(), "apply", "8.4", secondPlan.ID, uuid.NewString(), "retention acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	badConfig := filepath.Join("/etc/php/8.4/fpm/pool.d", "zz-fluxo-retention-"+uuid.NewString()+".conf")
	t.Cleanup(func() { _ = os.Remove(badConfig) })
	deadline := time.Now().Add(2 * time.Minute)
	injected := false
	for time.Now().Before(deadline) {
		o, e := GetOperation(second.ID)
		if e != nil {
			t.Fatal(e)
		}
		if o.Checkpoint == "package_transaction" || o.Phase == "applying" {
			if e = os.WriteFile(badConfig, []byte("invalid retention acceptance FPM directive\n"), 0600); e != nil {
				t.Fatal(e)
			}
			injected = true
			break
		}
		if !o.Active() {
			t.Fatalf("second attempt ended before failure injection: %s", o.Message)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !injected {
		t.Fatal("could not inject post-preflight FPM failure")
	}
	if done := vmWaitOperation(t, second.ID); done.Status == "succeeded" {
		t.Fatal("FPM failure fixture did not block completion")
	}
	firstDir, _ := m.archiveDirectory(first.ID)
	for _, a := range append(append([]PackageArchive{}, firstGeneration.Archives.Before...), firstGeneration.Archives.After...) {
		if _, e := os.Stat(filepath.Join(firstDir, a.SHA256+".deb")); e != nil {
			t.Fatalf("failed newer update pruned older recovery: %v", e)
		}
	}
	if err = os.Remove(badConfig); err != nil {
		t.Fatal(err)
	}
	repair, err := m.ReviewRecovery(context.Background(), second.ID, "repair")
	if err != nil {
		t.Fatal(err)
	}
	repaired, err := m.EnqueueRecovery(context.Background(), repair.ID, uuid.NewString(), "retention acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	if done := vmWaitOperation(t, repaired.ID); done.Status != "succeeded" {
		t.Fatal(done.Message)
	}
	// Review obtains the lifecycle lock after the worker's cleanup finishes.
	rollback, err := m.ReviewRecovery(context.Background(), repaired.ID, "rollback")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range append(append([]PackageArchive{}, firstGeneration.Archives.Before...), firstGeneration.Archives.After...) {
		if _, e := os.Lstat(filepath.Join(firstDir, a.SHA256+".deb")); !errors.Is(e, os.ErrNotExist) {
			t.Fatalf("superseded payload retained after successful repair: %v", e)
		}
	}
	for _, path := range []string{filepath.Join(firstDir, "manifest.json"), filepath.Join(filepath.Dir(firstDir), "configuration.tar")} {
		if _, e := os.Stat(path); e != nil {
			t.Fatal(e)
		}
	}
	// Force HTTP(S) acquisition to an unavailable proxy. The scoped recovery
	// repository must use only the latest generation's retained local packages.
	proxy := filepath.Join("/etc/apt/apt.conf.d", "99fluxo-retention-"+uuid.NewString())
	if err = os.WriteFile(proxy, []byte("Acquire::http::Proxy \"http://127.0.0.1:9\";\nAcquire::https::Proxy \"http://127.0.0.1:9\";\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(proxy) })
	rolled, err := m.EnqueueRecovery(context.Background(), rollback.ID, uuid.NewString(), "retention acceptance", "")
	if err != nil {
		t.Fatal(err)
	}
	if done := vmWaitOperation(t, rolled.ID); done.Status != "succeeded" {
		t.Fatal(done.Message)
	}
	if err = os.Remove(proxy); err != nil {
		t.Fatal(err)
	}
	g, err := getGeneration(second.ID)
	if err != nil || g.State != "rolled_back" || g.Pending {
		t.Fatalf("offline rollback did not settle: %+v %v", g, err)
	}
	ob, err := m.observePackages(context.Background(), "", "8.4")
	if err != nil {
		t.Fatal(err)
	}
	matched, err := validateRecoveryObservation(g, ob, "before")
	if err != nil || !matched {
		t.Fatalf("offline rollback packages did not verify: %v", err)
	}
	raw, _ := json.Marshal(map[string]any{"first": first.ID, "second": second.ID, "repair": repaired.ID, "rollback": rolled.ID})
	t.Logf("Two real updates, failed activation retention, successful repair pruning and offline rollback passed: %s", raw)
}
