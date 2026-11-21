package bridge

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/scheduler"
)

func TestVMLegacyCronPermutationPreservesExecutionFiles(t *testing.T) {
	host, _ := os.Hostname()
	if os.Geteuid() != 0 || host != "fluxo-php-patch-test" || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("guarded reusable VM only")
	}
	if time.Now().Month() == time.February && time.Now().Day() == 29 {
		t.Skip("fixture schedules must not be due")
	}
	root, err := os.MkdirTemp("/var/lib", "fluxo-cron-records-acceptance-")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", root)
	f := newLegacyCronRowsFixture(t, []int{2, 0, 1})
	if _, err := f.db.Exec("UPDATE crons SET id=id+9288700 WHERE id IN (1,2,3)"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	release, err := packages.Lifecycle(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	locks, err := acquireActivationLocks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer locks.release()
	files := map[int][]byte{}
	for i := range f.before {
		f.before[i].ID += 9288700
		f.before[i].Expression = "17 3 29 2 *"
		if _, err := f.db.Exec("UPDATE crons SET expression=? WHERE id=?", f.before[i].Expression, f.before[i].ID); err != nil {
			t.Fatal(err)
		}
	}
	for i, sourceIndex := range []int{2, 0, 1} {
		id := f.before[i].ID
		path := scheduler.CronPath(id)
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatal("refusing existing fixture path", path, err)
		}
		source := f.before[sourceIndex]
		raw := []byte(maintenancetask.LegacyContent(maintenancetask.Contract{ID: id, Expression: source.Expression, Command: source.Command, User: source.User}))
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			t.Fatal(err)
		}
		defer os.Remove(path)
		if _, err := file.Write(raw); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		files[id] = raw
	}
	fs := legacyCronRowsIO{
		read:    func(id int) ([]byte, error) { return readProtected(scheduler.CronPath(id), 1<<20) },
		pending: func(*sql.Conn) (bool, error) { return false, nil }, // Isolated test DB has no maintenance operations.
		unregistered: func(ids []int) error {
			if err := legacyCronIDsUnregistered(ids); err != nil {
				return err
			}
			return locks.verify()
		},
		archive: func(b legacyCronRowsBackup) error { return archiveLegacyCronRows(root, b) },
	}
	ids, err := restoreLegacyCronRows(ctx, f.db, fs)
	if err != nil || !reflect.DeepEqual(ids, []int{9288701, 9288702, 9288703}) {
		t.Fatal("actual-gate record alignment", ids, err)
	}
	for id, before := range files {
		after, err := fs.read(id)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("execution file changed", id, err)
		}
	}
	if ids, err = restoreLegacyCronRows(ctx, f.db, fs); err != nil || len(ids) != 0 {
		t.Fatal("native idempotent retry", ids, err)
	}
	archives, err := filepath.Glob(filepath.Join(root, "cron-migration-backups", "system-job-rows-*.json"))
	if err != nil || len(archives) != 1 {
		t.Fatal("protected backup missing", archives, err)
	}
	raw, err := readProtected(archives[0], 1<<20)
	var backup legacyCronRowsBackup
	if err != nil || json.Unmarshal(raw, &backup) != nil || !reflect.DeepEqual(backup.Before, f.before) || len(backup.After) != 3 || len(backup.Triggers) != 3 {
		t.Fatal("original native backup did not verify", err)
	}
	for _, id := range []int{9288701, 9288702, 9288703} {
		if err := os.Remove(scheduler.CronPath(id)); err != nil {
			t.Fatal("fixture cleanup", err)
		}
	}
	t.Log("PASS actual lifecycle/journal gates, protected cron reads and backup, atomic SQLite alignment, unchanged execution files, idempotent retry and fixture cleanup. No job/hook executed. Backup retained:", archives[0])
}
