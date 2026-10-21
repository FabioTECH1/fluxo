package phpupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"github.com/google/uuid"
)

func retentionFixture(t *testing.T, m *Manager, branch, label string) archiveGeneration {
	t.Helper()
	if err := os.Chmod(m.DataDir, 0700); err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	if _, err := database.DB.Exec(`INSERT INTO php_update_operations(id,kind,version,status,created_at,updated_at) VALUES (?,'apply',?,'succeeded',1,1)`, id, branch); err != nil {
		t.Fatal(err)
	}
	var order int64
	if err := database.DB.QueryRow("SELECT rowid FROM php_update_operations WHERE id=?", id).Scan(&order); err != nil {
		t.Fatal(err)
	}
	makeArchive := func(version, bytes string) PackageArchive {
		h := sha256.Sum256([]byte(bytes))
		name := "php" + branch + "-fpm"
		return PackageArchive{Name: name, Architecture: "amd64", Version: version, SHA256: hex.EncodeToString(h[:]), Size: int64(len(bytes)), Control: map[string]string{"Package": name, "Version": version, "Architecture": "amd64"}, Origins: []Origin{{Origin: "Ubuntu", Trusted: true}}}
	}
	before := makeArchive(label+"-old", label+" old package")
	after := makeArchive(label+"-new", label+" new package")
	g := recoveryGeneration{ID: id, Head: id, Version: branch, PlanID: uuid.NewString(), State: "succeeded", Goal: "after", VerifiedGoal: "after", Archives: archiveManifest{Schema: 1, Version: branch, RollbackAvailable: true, PolicyHash: "policy", Before: []PackageArchive{before}, After: []PackageArchive{after}, Packages: []Package{{Name: before.Name, Architecture: "amd64", Installed: before.Version, Candidate: after.Version}}}}
	if err := createGeneration(g); err != nil {
		t.Fatal(err)
	}
	dir, err := m.archiveDirectory(id)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		a PackageArchive
		b string
	}{{before, label + " old package"}, {after, label + " new package"}} {
		if err = os.WriteFile(filepath.Join(dir, p.a.SHA256+".deb"), []byte(p.b), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeRetentionManifest(t, m, g)
	if err = os.WriteFile(filepath.Join(filepath.Dir(dir), "configuration.tar"), []byte("configuration evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	return archiveGeneration{Order: order, Value: g, Settled: true}
}
func writeRetentionManifest(t *testing.T, m *Manager, g recoveryGeneration) {
	t.Helper()
	dir, err := m.archiveDirectory(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(g.Archives)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func saveRetentionFixture(t *testing.T, item archiveGeneration) {
	t.Helper()
	if err := saveGeneration(item.Value); err != nil {
		t.Fatal(err)
	}
}
func retentionStore(t *testing.T, m *Manager) *archiveRetentionStore {
	t.Helper()
	s, err := openArchiveRetentionStore(m.DataDir, uint32(os.Geteuid()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestArchiveRetentionPerBranchOrderAndFallback(t *testing.T) {
	m := testStore(t)
	a := retentionFixture(t, m, "8.4", "a")
	b := retentionFixture(t, m, "8.4", "b")
	c := retentionFixture(t, m, "8.3", "c")
	d := retentionFixture(t, m, "8.3", "d")
	items, err := readArchiveGenerations(context.Background(), database.DB, true)
	if err != nil {
		t.Fatal(err)
	}
	candidates := archivePruneCandidates(items, map[string]bool{c.Value.ID: true})
	if len(candidates) != 1 || candidates[0].Old.Value.ID != a.Value.ID || candidates[0].Successor.Value.ID != b.Value.ID {
		t.Fatalf("wrong branch/order/snapshot retention: %+v", candidates)
	}
	b.Value.Archives.RollbackAvailable = false
	b.Value.Archives.Before = nil
	saveRetentionFixture(t, b)
	items, err = readArchiveGenerations(context.Background(), database.DB, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := archivePruneCandidates(items, map[string]bool{c.Value.ID: true}); len(got) != 0 {
		t.Fatalf("last rollback-capable fallback discarded: %+v", got)
	}
	e := retentionFixture(t, m, "8.4", "e")
	items, err = readArchiveGenerations(context.Background(), database.DB, true)
	if err != nil {
		t.Fatal(err)
	}
	got := archivePruneCandidates(items, map[string]bool{c.Value.ID: true})
	if len(got) != 2 || got[0].Successor.Value.ID != e.Value.ID || got[1].Successor.Value.ID != e.Value.ID {
		t.Fatalf("complete later rollback set did not retire old sets: %+v", got)
	}
	_ = d
}

func TestArchiveRetentionLaterRollbackDoesNotDisplaceNoRollbackFallback(t *testing.T) {
	m := testStore(t)
	a := retentionFixture(t, m, "8.4", "fallback")
	b := retentionFixture(t, m, "8.4", "no-rollback")
	b.Value.Archives.RollbackAvailable = false
	b.Value.Archives.Before = nil
	saveRetentionFixture(t, b)
	c := retentionFixture(t, m, "8.4", "later-rolled-back")
	c.Value.State = "rolled_back"
	c.Value.Goal = "before"
	c.Value.VerifiedGoal = "before"
	saveRetentionFixture(t, c)
	items, err := readArchiveGenerations(context.Background(), database.DB, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range archivePruneCandidates(items, nil) {
		if candidate.Old.Value.ID == a.Value.ID {
			t.Fatal("later rollback displaced earlier complete fallback")
		}
	}
}

func TestArchiveRetentionLegacyUnresolvedTransactionDefersCleanup(t *testing.T) {
	m := testStore(t)
	retentionFixture(t, m, "8.4", "old")
	retentionFixture(t, m, "8.4", "new")
	id := uuid.NewString()
	if _, err := database.DB.Exec(`INSERT INTO php_update_operations(id,kind,version,status,checkpoint,created_at,updated_at) VALUES (?,'verify','8.3','interrupted','packages_updated',1,1)`, id); err != nil {
		t.Fatal(err)
	}
	if err := archiveCleanupIdle(context.Background(), database.DB); !errors.Is(err, errArchiveCleanupDeferred) {
		t.Fatalf("legacy unresolved transaction did not protect archives: %v", err)
	}
	retentionFixture(t, m, "8.3", "resolved")
	if err := archiveCleanupIdle(context.Background(), database.DB); err != nil {
		t.Fatalf("resolved historical failure blocked cleanup forever: %v", err)
	}
	if _, err := database.DB.Exec("INSERT INTO php_update_operations(id,kind,version,status,created_at,updated_at) VALUES (?,'apply','8.4','failed',1,1)", uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if err := archiveCleanupIdle(context.Background(), database.DB); err != nil {
		t.Fatalf("ordinary failed preparation blocked cleanup: %v", err)
	}
}

func retentionPreparedBatch(t *testing.T, m *Manager, old, latest archiveGeneration) (*archiveRetentionStore, *archiveRetentionSet, *archiveRetentionSet, []archiveGeneration, string) {
	t.Helper()
	store := retentionStore(t, m)
	set, err := store.inspectGeneration(context.Background(), old.Value, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(set.Close)
	anchor, err := store.inspectGeneration(context.Background(), latest.Value, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(anchor.Close)
	items, err := readArchiveGenerations(context.Background(), database.DB, true)
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := store.snapshotFootprint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return store, set, anchor, items, footprint
}

func TestArchiveRetentionFinalTransactionRechecksEvidence(t *testing.T) {
	for _, change := range []string{"enqueue", "unfinished-success", "snapshot", "candidate-bytes", "successor-bytes"} {
		t.Run(change, func(t *testing.T) {
			m := testStore(t)
			old := retentionFixture(t, m, "8.4", "old")
			latest := retentionFixture(t, m, "8.4", "latest")
			store, set, anchor, items, footprint := retentionPreparedBatch(t, m, old, latest)
			switch change {
			case "enqueue":
				if _, err := database.DB.Exec(`INSERT INTO php_update_operations(id,kind,version,status,created_at,updated_at) VALUES (?,'apply','8.4','queued',1,1)`, uuid.NewString()); err != nil {
					t.Fatal(err)
				}
			case "unfinished-success":
				if _, err := database.DB.Exec("UPDATE php_update_operations SET status='interrupted' WHERE id=?", latest.Value.Head); err != nil {
					t.Fatal(err)
				}
			case "snapshot":
				copyRetentionSnapshot(t, m, "20261021T220000Z-111")
			case "candidate-bytes", "successor-bytes":
				g := old.Value
				if change == "successor-bytes" {
					g = latest.Value
				}
				dir, _ := m.archiveDirectory(g.ID)
				if err := os.WriteFile(filepath.Join(dir, g.Archives.After[0].SHA256+".deb"), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			n, err := m.unlinkObsoleteArchiveBatch(context.Background(), store, set, anchor, archivePruneCandidate{Old: old, Successor: latest}, items, footprint)
			if err == nil || n != 0 {
				t.Fatalf("changed %s evidence permitted deletion: %d %v", change, n, err)
			}
			dir, _ := m.archiveDirectory(old.Value.ID)
			if _, err = os.Lstat(filepath.Join(dir, old.Value.Archives.Before[0].SHA256+".deb")); err != nil {
				t.Fatal("old rollback file lost")
			}
		})
	}
}

func TestArchiveRetentionFinalTransactionBatchesAndAuditsWithoutChangingHistory(t *testing.T) {
	m := testStore(t)
	old := retentionFixture(t, m, "8.4", "old")
	dir, _ := m.archiveDirectory(old.Value.ID)
	for i := 1; i < 33; i++ {
		name := fmt.Sprintf("php8.4-fixture%d", i)
		p := Package{Name: name, Architecture: "amd64", Installed: "1", Candidate: "2"}
		for j := 1; j <= 2; j++ {
			b := []byte(fmt.Sprintf("fixture %d version %d", i, j))
			h := sha256.Sum256(b)
			a := PackageArchive{Name: name, Architecture: "amd64", Version: fmt.Sprint(j), SHA256: hex.EncodeToString(h[:]), Size: int64(len(b)), Control: map[string]string{"Package": name, "Architecture": "amd64", "Version": fmt.Sprint(j)}, Origins: []Origin{{Origin: "Ubuntu", Trusted: true}}}
			if j == 1 {
				old.Value.Archives.Before = append(old.Value.Archives.Before, a)
			} else {
				old.Value.Archives.After = append(old.Value.Archives.After, a)
			}
			if err := os.WriteFile(filepath.Join(dir, a.SHA256+".deb"), b, 0600); err != nil {
				t.Fatal(err)
			}
		}
		old.Value.Archives.Packages = append(old.Value.Archives.Packages, p)
	}
	saveRetentionFixture(t, old)
	writeRetentionManifest(t, m, old.Value)
	latest := retentionFixture(t, m, "8.4", "latest")
	for attempt, want := range []int{64, 2} {
		store, set, anchor, items, footprint := retentionPreparedBatch(t, m, old, latest)
		n, err := m.unlinkObsoleteArchiveBatch(context.Background(), store, set, anchor, archivePruneCandidate{Old: old, Successor: latest}, items, footprint)
		set.Close()
		anchor.Close()
		if err != nil || n != want {
			t.Fatalf("batch %d: got %d want %d: %v", attempt, n, want, err)
		}
	}
	var events int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM php_update_events WHERE operation_id=? AND phase='archives_pruned'", old.Value.Head).Scan(&events); err != nil || events != 2 {
		t.Fatalf("cleanup audit missing: %d %v", events, err)
	}
	g, err := getGeneration(old.Value.ID)
	if err != nil || !reflect.DeepEqual(g, old.Value) {
		t.Fatal("immutable generation changed during cleanup")
	}
	operation, err := GetOperation(latest.Value.Head)
	if err != nil || operation.Status != "succeeded" {
		t.Fatal("successful update changed status")
	}
}

func TestArchiveRetentionWriteBoundarySerializesEnqueue(t *testing.T) {
	m := testStore(t)
	database.DB.SetMaxOpenConns(2)
	defer database.DB.SetMaxOpenConns(1)
	conn, err := database.DB.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	done := make(chan error, 1)
	go func() {
		_, e := m.Enqueue(context.Background(), "check", "", "", uuid.NewString(), "retention fixture", "")
		done <- e
	}()
	select {
	case e := <-done:
		t.Fatalf("admission crossed cleanup's write boundary: %v", e)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err = conn.ExecContext(context.Background(), "COMMIT"); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("enqueue did not resume after cleanup committed")
	}
	var count int
	if err = database.DB.QueryRow("SELECT count(*) FROM php_update_operations WHERE status='queued'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("enqueue was lost: %d %v", count, err)
	}
}

func TestArchiveRetentionRequiresDurableSuccessAndSettledRecovery(t *testing.T) {
	for _, state := range []string{"prepared", "package_transaction", "packages_updated", "activating", "releasing", "succeeded", "rolled_back"} {
		t.Run(state, func(t *testing.T) {
			m := testStore(t)
			retentionFixture(t, m, "8.4", "old")
			latest := retentionFixture(t, m, "8.4", "latest")
			latest.Value.State = state
			latest.Value.Pending = true
			saveRetentionFixture(t, latest)
			if _, err := readArchiveGenerations(context.Background(), database.DB, true); !errors.Is(err, errArchiveCleanupDeferred) {
				t.Fatalf("unsettled %s accepted: %v", state, err)
			}
		})
	}
	m := testStore(t)
	old := retentionFixture(t, m, "8.4", "old")
	latest := retentionFixture(t, m, "8.4", "latest")
	if _, err := database.DB.Exec("UPDATE php_update_operations SET status='interrupted' WHERE id=?", latest.Value.Head); err != nil {
		t.Fatal(err)
	}
	if _, err := readArchiveGenerations(context.Background(), database.DB, true); !errors.Is(err, errArchiveCleanupDeferred) {
		t.Fatalf("unfinished terminal operation authorized cleanup: %v", err)
	}
	// Successful repair advances the head and completes the same generation.
	head := uuid.NewString()
	if _, err := database.DB.Exec(`INSERT INTO php_update_operations(id,kind,version,status,created_at,updated_at) VALUES (?,'repair','8.4','succeeded',1,1)`, head); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("INSERT INTO php_update_recovery_attempts(operation_id,generation_id,parent_id) VALUES (?,?,?)", head, latest.Value.ID, latest.Value.ID); err != nil {
		t.Fatal(err)
	}
	latest.Value.Head = head
	raw, _ := json.Marshal(latest.Value)
	if _, err := database.DB.Exec("UPDATE php_update_generations SET head_operation_id=?,payload=? WHERE id=?", head, string(raw), latest.Value.ID); err != nil {
		t.Fatal(err)
	}
	items, err := readArchiveGenerations(context.Background(), database.DB, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := archivePruneCandidates(items, nil); len(got) != 1 || got[0].Old.Value.ID != old.Value.ID {
		t.Fatalf("successful forward repair did not qualify: %+v", got)
	}
	latest.Value.State = "rolled_back"
	latest.Value.Goal = "before"
	latest.Value.VerifiedGoal = "before"
	saveRetentionFixture(t, latest)
	items, err = readArchiveGenerations(context.Background(), database.DB, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := archivePruneCandidates(items, nil); len(got) != 0 {
		t.Fatalf("rollback authorized retiring previous rollback files: %+v", got)
	}
}

func TestArchiveRetentionRejectsMalformedHistoryAndHolds(t *testing.T) {
	for _, change := range []string{"payload", "column", "head", "barrier", "repository", "active"} {
		t.Run(change, func(t *testing.T) {
			m := testStore(t)
			old := retentionFixture(t, m, "8.4", "old")
			retentionFixture(t, m, "8.4", "latest")
			var err error
			switch change {
			case "payload":
				_, err = database.DB.Exec("UPDATE php_update_generations SET payload='{}' WHERE id=?", old.Value.ID)
			case "column":
				_, err = database.DB.Exec("UPDATE php_update_generations SET version='8.3' WHERE id=?", old.Value.ID)
			case "head":
				_, err = database.DB.Exec("UPDATE php_update_operations SET version='8.3' WHERE id=?", old.Value.Head)
			case "barrier":
				_, err = database.DB.Exec("INSERT INTO php_update_barriers(unit_name,generation_id,payload) VALUES ('fixture',?,'{}')", old.Value.ID)
			case "repository":
				_, err = database.DB.Exec("INSERT INTO php_repository_migrations(plan_id,state,before_policy,created_at) VALUES (?,'pending','old',1)", uuid.NewString())
			case "active":
				_, err = database.DB.Exec("INSERT INTO php_update_operations(id,kind,version,status,created_at,updated_at) VALUES (?,'apply','8.4','queued',1,1)", uuid.NewString())
			}
			if err != nil {
				t.Fatal(err)
			}
			if change == "payload" || change == "column" || change == "head" {
				_, err = readArchiveGenerations(context.Background(), database.DB, true)
			} else {
				err = archiveCleanupIdle(context.Background(), database.DB)
			}
			if err == nil {
				t.Fatalf("unsafe %s accepted", change)
			}
		})
	}
}

func TestArchiveRetentionDeletesOnlyRecordedBytesAndRetries(t *testing.T) {
	m := testStore(t)
	old := retentionFixture(t, m, "8.4", "old")
	latest := retentionFixture(t, m, "8.4", "latest")
	store := retentionStore(t, m)
	dir, _ := m.archiveDirectory(old.Value.ID)
	copyDir := filepath.Join(dir, "recovery-before")
	if err := os.MkdirAll(copyDir, 0700); err != nil {
		t.Fatal(err)
	}
	a := old.Value.Archives.Before[0]
	b, err := os.ReadFile(filepath.Join(dir, a.SHA256+".deb"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(copyDir, a.SHA256+".deb"), b, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(copyDir, "Packages"), []byte("retained APT metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	set, err := store.inspectGeneration(context.Background(), old.Value, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = set.Unchanged(); err != nil {
		t.Fatal(err)
	}
	n, _, err := set.UnlinkBatch(context.Background(), 1)
	set.Close()
	if err != nil || n != 1 {
		t.Fatalf("first interrupted batch: %d %v", n, err)
	}
	set, err = store.inspectGeneration(context.Background(), old.Value, false)
	if err != nil {
		t.Fatal(err)
	}
	n, _, err = set.UnlinkBatch(context.Background(), 64)
	set.Close()
	if err != nil || n != 2 {
		t.Fatalf("resumed batch: %d %v", n, err)
	}
	set, err = store.inspectGeneration(context.Background(), old.Value, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Files) != 0 {
		t.Fatal("cleanup was not idempotent")
	}
	set.Close()
	for _, path := range []string{filepath.Join(dir, "manifest.json"), filepath.Join(filepath.Dir(dir), "configuration.tar"), filepath.Join(copyDir, "Packages")} {
		if _, err = os.Stat(path); err != nil {
			t.Fatalf("audit/configuration metadata removed: %v", err)
		}
	}
	set, err = store.inspectGeneration(context.Background(), latest.Value, true)
	if err != nil {
		t.Fatalf("latest archives damaged: %v", err)
	}
	set.Close()
	stored, err := getGeneration(old.Value.ID)
	if err != nil || !reflect.DeepEqual(stored, old.Value) {
		t.Fatal("historical recovery evidence rewritten")
	}
}

func TestArchiveRetentionUnsafeOrChangedFilesStayUntouched(t *testing.T) {
	for _, kind := range []string{"corrupt", "manifest", "unknown", "symlink", "hardlink", "permissions", "directory-symlink", "changed-after-inspection", "incomplete-successor"} {
		t.Run(kind, func(t *testing.T) {
			m := testStore(t)
			item := retentionFixture(t, m, "8.4", "fixture")
			store := retentionStore(t, m)
			dir, _ := m.archiveDirectory(item.Value.ID)
			file := filepath.Join(dir, item.Value.Archives.Before[0].SHA256+".deb")
			outside := filepath.Join(m.DataDir, "unrelated")
			if err := os.WriteFile(outside, []byte("outside sentinel"), 0600); err != nil {
				t.Fatal(err)
			}
			var original *archiveRetentionSet
			if kind == "changed-after-inspection" {
				var err error
				original, err = store.inspectGeneration(context.Background(), item.Value, false)
				if err != nil {
					t.Fatal(err)
				}
				defer original.Close()
			}
			switch kind {
			case "corrupt", "changed-after-inspection":
				if err := os.WriteFile(file, []byte("corrupt"), 0600); err != nil {
					t.Fatal(err)
				}
			case "manifest":
				if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "unknown":
				if err := os.WriteFile(filepath.Join(dir, "unrecorded.deb"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, file); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(file, filepath.Join(m.DataDir, "linked-package")); err != nil {
					t.Fatal(err)
				}
			case "permissions":
				if err := os.Chmod(file, 0666); err != nil {
					t.Fatal(err)
				}
			case "directory-symlink":
				moved := dir + "-moved"
				if err := os.Rename(dir, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, dir); err != nil {
					t.Fatal(err)
				}
			case "incomplete-successor":
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
			}
			if original != nil {
				if err := original.Unchanged(); err == nil {
					t.Fatal("changed package accepted after authentication")
				}
			} else {
				set, err := store.inspectGeneration(context.Background(), item.Value, kind == "incomplete-successor")
				if err == nil {
					set.Close()
					t.Fatalf("unsafe %s accepted", kind)
				}
			}
			b, err := os.ReadFile(outside)
			if err != nil || string(b) != "outside sentinel" {
				t.Fatal("outside file changed")
			}
		})
	}
}

func copyRetentionSnapshot(t *testing.T, m *Manager, name string) string {
	t.Helper()
	dir := filepath.Join(m.DataDir, "upgrades", name)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	var sequence int
	var dbname, path string
	if err := database.DB.QueryRow("PRAGMA database_list").Scan(&sequence, &dbname, &path); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		b, err := os.ReadFile(path + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "fluxo.db"+suffix), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "database-present"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}
func TestArchiveRetentionInstallerSnapshotsPinOnlyRequiredHistoryWithWAL(t *testing.T) {
	m := testStore(t)
	if _, err := database.DB.Exec("PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	old := retentionFixture(t, m, "8.4", "old")
	required := retentionFixture(t, m, "8.4", "snapshot-current")
	dir := copyRetentionSnapshot(t, m, "20261021T220000Z-101")
	wal, err := os.ReadFile(filepath.Join(dir, "fluxo.db-wal"))
	if err != nil || len(wal) == 0 {
		t.Fatalf("fixture needs real WAL evidence: %v", err)
	}
	latest := retentionFixture(t, m, "8.4", "latest")
	store := retentionStore(t, m)
	pins, footprint, err := store.snapshotPins(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !pins[required.Value.ID] || pins[old.Value.ID] || pins[latest.Value.ID] {
		t.Fatalf("snapshot pinned wrong history: %+v", pins)
	}
	after, err := os.ReadFile(filepath.Join(dir, "fluxo.db-wal"))
	if err != nil || !reflect.DeepEqual(wal, after) {
		t.Fatal("snapshot WAL mutated")
	}
	items, err := readArchiveGenerations(context.Background(), database.DB, true)
	if err != nil {
		t.Fatal(err)
	}
	candidates := archivePruneCandidates(items, pins)
	if len(candidates) != 1 || candidates[0].Old.Value.ID != old.Value.ID {
		t.Fatalf("historical rows pinned forever: %+v", candidates)
	}
	if err = os.WriteFile(filepath.Join(dir, "fluxo.db-wal"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	now, err := store.snapshotFootprint(context.Background())
	if err != nil || now == footprint {
		t.Fatal("snapshot replacement was not detected")
	}
}

func TestArchiveRetentionRejectsUnsafeInstallerSnapshots(t *testing.T) {
	for _, kind := range []string{"unknown-name", "missing-database", "corrupt", "symlink", "both-markers"} {
		t.Run(kind, func(t *testing.T) {
			m := testStore(t)
			retentionFixture(t, m, "8.4", "current")
			dir := copyRetentionSnapshot(t, m, "20261021T220000Z-101")
			store := retentionStore(t, m)
			switch kind {
			case "unknown-name":
				if err := os.Rename(dir, filepath.Join(filepath.Dir(dir), "unknown")); err != nil {
					t.Fatal(err)
				}
			case "missing-database":
				if err := os.Remove(filepath.Join(dir, "fluxo.db")); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				for _, suffix := range []string{"-wal", "-shm"} {
					_ = os.Remove(filepath.Join(dir, "fluxo.db"+suffix))
				}
				if err := os.WriteFile(filepath.Join(dir, "fluxo.db"), []byte(strings.Repeat("x", 128)), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				path := filepath.Join(dir, "fluxo.db")
				if err := os.Rename(path, path+"-moved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+"-moved", path); err != nil {
					t.Fatal(err)
				}
			case "both-markers":
				if err := os.WriteFile(filepath.Join(dir, "database-absent"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := store.snapshotPins(context.Background()); err == nil {
				t.Fatalf("unsafe snapshot %s accepted", kind)
			}
		})
	}
}
