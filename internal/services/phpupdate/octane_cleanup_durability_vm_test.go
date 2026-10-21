package phpupdate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"fluxo/internal/database"
	"fluxo/internal/services/daemon"
	"fluxo/internal/services/octaneruntime"
)

// Fixtures are generated managed wrappers, enabled but never started. No native
// request, application bootstrap, package mutation or ordinary process stop is
// needed to exercise the real systemd namespace durability boundaries.
func vmInactiveOctaneDurabilityFixture(t *testing.T, id, port int) string {
	t.Helper()
	name := unitName(id, 1)
	for _, path := range []string{filepath.Join("/etc/systemd/system", name), filepath.Join("/etc/systemd/system", name+".d"), filepath.Join("/etc/systemd/system/multi-user.target.wants", name)} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("refusing existing durability fixture", path)
		}
	}
	p := unitProperties(vmRun(t, "/usr/bin/systemctl", "show", name, "--property=LoadState,ActiveState"))
	if p["LoadState"] != "not-found" || p["ActiveState"] != "inactive" {
		t.Fatal("durability fixture name already belongs to a service", p)
	}
	if err := octaneruntime.Install(); err != nil {
		t.Fatal(err)
	}
	if err := daemon.GenerateServiceFile(id, octaneruntime.Command("8.4", port), recoveryVMApp, "fluxo", 60, 15, "SIGTERM"); err != nil {
		t.Fatal(err)
	}
	vmRun(t, "/usr/bin/systemctl", "daemon-reload")
	vmRun(t, "/usr/bin/systemctl", "enable", name)
	u, err := observeUnit(context.Background(), name, "")
	if err != nil || u.Active || u.MainPID != 0 || len(u.PIDs) != 0 {
		t.Fatal("durability fixture must remain inactive", err)
	}
	return name
}

func TestVMPreparingBarrierRetriesPublishedOverrideDirectorySync(t *testing.T) {
	requireRecoveryVM(t)
	m := testStore(t)
	const id, port = 9107103, 18923
	name := vmInactiveOctaneDurabilityFixture(t, id, port)
	u, err := unitState(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	g := vmGeneration(t, m, []unitSnapshot{u})
	original := unitProperties(vmRun(t, "/usr/bin/systemctl", "show", name, "--property="+inspectedUnitProperties))
	if err = ensureDurableManagedDropInDirectory(name); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join("/etc/systemd/system", name+".d")
	marker := filepath.Join(dir, "90-fluxo-php-"+g.ID+".hold")
	content, err := barrierContent(marker)
	if err != nil {
		t.Fatal(err)
	}
	b := maintenanceBarrier{Unit: name, GenerationID: g.ID, Path: filepath.Join(dir, "90-fluxo-php-maintenance.conf"), Marker: marker, Content: content, Original: original, WasActive: u.Active, State: "preparing"}
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.DB.Exec("INSERT INTO php_update_barriers(unit_name,generation_id,payload) VALUES (?,?,?)", b.Unit, b.GenerationID, string(raw)); err != nil {
		t.Fatal(err)
	}
	// Recreate the crash-visible boundary: complete published marker/override
	// bytes and a preparing journal, with publication durability unconfirmed.
	// The retry must establish durability even though both files already match.
	for _, file := range []struct {
		path, content string
		mode          os.FileMode
	}{{b.Marker, g.ID + "\n", 0600}, {b.Path, b.Content, 0644}} {
		f, e := os.OpenFile(file.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, file.mode)
		if e != nil {
			t.Fatal(e)
		}
		_, e = f.Write([]byte(file.content))
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e != nil || closeErr != nil {
			t.Fatal("fixture publication failed", e, closeErr)
		}
		// Deliberately omit parent-directory fsync at this injected boundary.
	}
	originalSync := syncMaintenanceNamespace
	t.Cleanup(func() { syncMaintenanceNamespace = originalSync })
	injected := errors.New("acceptance existing override directory fsync interruption")
	directorySyncs := 0
	syncMaintenanceNamespace = func(path string) error {
		if path == dir {
			directorySyncs++
			// The first sync is the existing marker; interrupt only the existing
			// override's directory sync, after both existing files were examined.
			if directorySyncs == 2 {
				return injected
			}
		}
		return originalSync(path)
	}
	// Use the drain entry point so failed preparation must prevent the durable
	// draining transition as well as any consumer signalling.
	if err = m.drainConsumers(context.Background(), &g); !errors.Is(err, injected) || directorySyncs != 2 {
		t.Fatalf("published override retry bypassed fsync: calls=%d err=%v", directorySyncs, err)
	}
	stored, err := getBarrier(name)
	if err != nil || stored.State != "preparing" || stored.GenerationID != g.ID {
		t.Fatal("unsynced override was treated as an established hold", err)
	}
	current, err := getGeneration(g.ID)
	if err != nil || current.State != "prepared" || len(current.Attempt.Signalled) != 0 {
		t.Fatal("failed preparation admitted drainage", err)
	}
	if err = verifyBarrierFile(b.Path, b.Content); err != nil {
		t.Fatal("retry replaced the published override", err)
	}
	if err = verifyBarrierFile(b.Marker, g.ID+"\n"); err != nil {
		t.Fatal("retry replaced the published marker", err)
	}
	directorySyncs = 0
	syncMaintenanceNamespace = func(path string) error {
		if path == dir {
			directorySyncs++
		}
		return originalSync(path)
	}
	if err = m.prepareBarrier(context.Background(), g, u); err != nil || directorySyncs != 2 {
		t.Fatalf("retry did not re-sync both existing publications: calls=%d err=%v", directorySyncs, err)
	}
	stored, err = getBarrier(name)
	if err != nil || stored.State != "held" || verifyHeldBarrier(context.Background(), stored) != nil {
		t.Fatal("successful publication retry did not establish verified hold", err)
	}
	syncMaintenanceNamespace = originalSync
	if err = m.releaseBarriers(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	if err = CleanupManagedOctane(context.Background(), "8.4", id, recoveryVMApp, port); err != nil {
		t.Fatal(err)
	}
	assertCleanupAcceptanceRemoved(t, id)
}

func TestVMOctaneCleanupRetriesAbsentEntryNamespaceSync(t *testing.T) {
	requireRecoveryVM(t)
	for index, boundary := range []string{"unit", "enable-link"} {
		t.Run(boundary, func(t *testing.T) {
			id, port := 9107100+index, 18920+index
			name := vmInactiveOctaneDurabilityFixture(t, id, port)
			unitPath := filepath.Join("/etc/systemd/system", name)
			linkPath := filepath.Join("/etc/systemd/system/multi-user.target.wants", name)
			journal, override, marker := cleanupPaths(name)
			target, namespace := unitPath, filepath.Dir(unitPath)
			if boundary == "enable-link" {
				target, namespace = linkPath, filepath.Dir(linkPath)
			}
			originalSync := syncMaintenanceNamespace
			t.Cleanup(func() { syncMaintenanceNamespace = originalSync })
			injected := errors.New("acceptance namespace fsync interruption")
			intercepted := 0
			syncMaintenanceNamespace = func(path string) error {
				if path == namespace {
					if _, err := os.Lstat(target); os.IsNotExist(err) {
						intercepted++
						return injected
					}
				}
				return originalSync(path)
			}
			err := CleanupManagedOctane(context.Background(), "8.4", id, recoveryVMApp, port)
			if !errors.Is(err, injected) || intercepted != 1 {
				t.Fatalf("did not interrupt after %s unlink: calls=%d err=%v", boundary, intercepted, err)
			}
			if _, err = os.Lstat(target); !os.IsNotExist(err) {
				t.Fatal("injected boundary did not leave the target absent", err)
			}
			j := readCleanupAcceptanceJournal(t, name)
			if j.State != "removing" || j.Delivery != "" {
				t.Fatal("inactive cleanup lost its removal checkpoint or signalled a process")
			}
			content, err := barrierContent(marker)
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range []struct{ path, content string }{{marker, j.ID + "\n"}, {override, content}} {
				if err = verifyBarrierFile(file.path, file.content); err != nil {
					t.Fatal("fsync failure removed a boot protection", err)
				}
			}
			// The first unlink already succeeded. The resumed ENOENT path must
			// still sync the parent while all protection and journal files exist.
			resynced := false
			protectionMissing := false
			syncMaintenanceNamespace = func(path string) error {
				if path == namespace {
					if _, err := os.Lstat(target); os.IsNotExist(err) {
						resynced = true
						for _, kept := range []string{journal, override, marker} {
							if _, err := os.Lstat(kept); err != nil {
								protectionMissing = true
							}
						}
					}
				}
				return originalSync(path)
			}
			if err = CleanupManagedOctane(context.Background(), "8.4", id, recoveryVMApp, port); err != nil {
				t.Fatal(err)
			}
			if !resynced || protectionMissing {
				t.Fatalf("resumed cleanup did not sync absent %s before dropping protection: synced=%v missing=%v", boundary, resynced, protectionMissing)
			}
			syncMaintenanceNamespace = originalSync
			assertCleanupAcceptanceRemoved(t, id)
		})
	}
}

func TestVMBarrierRequiresDurableDropInParentBeforeJournalAndOnResume(t *testing.T) {
	requireRecoveryVM(t)
	m := testStore(t)
	const id, port = 9107102, 18922
	name := vmInactiveOctaneDurabilityFixture(t, id, port)
	u, err := unitState(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	g := vmGeneration(t, m, []unitSnapshot{u})
	originalSync := syncMaintenanceNamespace
	t.Cleanup(func() { syncMaintenanceNamespace = originalSync })
	injected := errors.New("acceptance drop-in parent fsync interruption")
	parentSyncs := 0
	syncMaintenanceNamespace = func(path string) error {
		if path == "/etc/systemd/system" {
			parentSyncs++
			return injected
		}
		return originalSync(path)
	}
	if err = m.prepareBarrier(context.Background(), g, u); !errors.Is(err, injected) || parentSyncs == 0 {
		t.Fatal("new drop-in directory was accepted without durable parent", err)
	}
	if _, err = getBarrier(name); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("barrier journal was created before parent namespace sync", err)
	}
	dropIn := filepath.Join("/etc/systemd/system", name+".d")
	entries, err := os.ReadDir(dropIn)
	if err != nil || len(entries) != 0 {
		t.Fatal("failed parent sync installed protection files prematurely", err)
	}
	// Directory already exists now. A retry must sync it, rather than relying
	// on MkdirAll's success as evidence that the directory entry is durable.
	parentSyncs = 0
	syncMaintenanceNamespace = func(path string) error {
		if path == "/etc/systemd/system" {
			parentSyncs++
		}
		return originalSync(path)
	}
	if err = m.prepareBarrier(context.Background(), g, u); err != nil || parentSyncs == 0 {
		t.Fatal("preparing the existing directory did not retry parent sync", err)
	}
	b, err := getBarrier(name)
	if err != nil || verifyHeldBarrier(context.Background(), b) != nil {
		t.Fatal("retry did not establish a verified barrier", err)
	}
	parentSyncs = 0
	syncMaintenanceNamespace = func(path string) error {
		if path == "/etc/systemd/system" {
			parentSyncs++
			return injected
		}
		return originalSync(path)
	}
	if err = m.prepareBarrier(context.Background(), g, u); !errors.Is(err, injected) || parentSyncs == 0 {
		t.Fatal("resuming a held journal skipped parent namespace durability", err)
	}
	if err = verifyHeldBarrier(context.Background(), b); err != nil {
		t.Fatal("resumed fsync failure removed existing protection", err)
	}
	syncMaintenanceNamespace = originalSync
	if err = m.prepareBarrier(context.Background(), g, u); err != nil {
		t.Fatal(err)
	}
	if err = m.releaseBarriers(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	// The inactive fixture can now use the same native-safe removal helper.
	if err = CleanupManagedOctane(context.Background(), "8.4", id, recoveryVMApp, port); err != nil {
		t.Fatal(err)
	}
	assertCleanupAcceptanceRemoved(t, id)
}
