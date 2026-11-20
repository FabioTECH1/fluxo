package cron

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/scheduler"
	"golang.org/x/sys/unix"
)

func TestVMLegacyCleanupNormalizationPreservesIntent(t *testing.T) {
	host, _ := os.Hostname()
	if os.Geteuid() != 0 || host != "fluxo-php-patch-test" || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("guarded reusable VM only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root, err := os.MkdirTemp("/var/lib", "fluxo-cleanup-normalization-")
	if err != nil {
		t.Fatal(err)
	}
	previous := database.DB
	if err = database.InitDB(filepath.Join(root, "fluxo.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.DB.Close(); database.DB = previous })
	const expression = "17 3 29 2 *" // Never runs during this November acceptance.
	contracts := make(map[int]maintenancetask.Contract)
	for _, id := range []int{9288301, 9288302, 9288303} {
		if _, err = os.Lstat(scheduler.CronPath(id)); !os.IsNotExist(err) {
			t.Fatal("refusing existing fixture path", id, err)
		}
		if _, err = database.DB.Exec(`INSERT INTO crons(id,site_id,name,expression,command,user) VALUES(?,0,'System Cleanup',?,?,'root')`, id, expression, legacyCleanupCommand); err != nil {
			t.Fatal(err)
		}
		c, _ := maintenancetask.Qualify(id, 0, expression, maintenancetask.PackageCleanupCommand, "root")
		contracts[id] = c
		t.Cleanup(func() { os.Remove(scheduler.CronPath(id)) })
	}
	old := contracts[9288301]
	old.Command = legacyCleanupCommand
	if err = os.WriteFile(scheduler.CronPath(old.ID), []byte(maintenancetask.LegacyContent(old)), 0644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(scheduler.CronPath(9288303), []byte("# Administrator custom job; preserved\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if changed, err := NormalizeLegacyPackageCleanup(ctx, old.ID); err != nil || !changed {
		t.Fatal("exact generated cleanup conversion", changed, err)
	}
	after, err := readCleanupFile(scheduler.CronPath(old.ID))
	if err != nil || string(after) != maintenancetask.LegacyContent(contracts[old.ID]) {
		t.Fatal("canonical trigger mismatch", err)
	}
	var command string
	if err = database.DB.QueryRow("SELECT command FROM crons WHERE id=?", old.ID).Scan(&command); err != nil || command != maintenancetask.PackageCleanupCommand {
		t.Fatal("canonical DB row mismatch", err)
	}
	if changed, err := NormalizeLegacyPackageCleanup(ctx, old.ID); err != nil || changed {
		t.Fatal("completed conversion replay", changed, err)
	}
	if changed, err := NormalizeLegacyPackageCleanup(ctx, 9288302); err != nil || changed {
		t.Fatal("missing job was changed", changed, err)
	}
	if _, err = os.Lstat(scheduler.CronPath(9288302)); !os.IsNotExist(err) {
		t.Fatal("missing job was enabled", err)
	}
	if changed, err := NormalizeLegacyPackageCleanup(ctx, 9288303); err == nil || changed {
		t.Fatal("custom file was accepted", changed, err)
	}
	if raw, err := os.ReadFile(scheduler.CronPath(9288303)); err != nil || string(raw) != "# Administrator custom job; preserved\n" {
		t.Fatal("custom file changed", err)
	}
	backups, err := filepath.Glob(filepath.Join(root, "cron-migration-backups", "*.json"))
	if err != nil || len(backups) != 1 {
		t.Fatal("expected one protected original backup", backups, err)
	}
	b, err := readCleanupFile(backups[0])
	var backup cleanupBackup
	if err != nil || json.Unmarshal(b, &backup) != nil || backup.Command != legacyCleanupCommand || backup.Trigger != maintenancetask.LegacyContent(old) || strings.Contains(backup.Trigger, "system-task-receipt") {
		t.Fatal("original evidence missing or receipt fabricated", err)
	}
	// Exercise the actual protected-file reader, without following links or
	// blocking on a FIFO. These checks precede every publication and backup use.
	target := filepath.Join(root, "protected-input")
	if err = os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"symlink", "hardlink", "fifo", "writable"} {
		path := filepath.Join(root, kind)
		switch kind {
		case "symlink":
			err = os.Symlink(target, path)
		case "hardlink":
			err = os.Link(target, path)
		case "fifo":
			err = unix.Mkfifo(path, 0600)
		case "writable":
			err = os.WriteFile(path, []byte("unsafe"), 0666)
			if err == nil {
				err = os.Chmod(path, 0666)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err = readCleanupFile(path); err == nil {
			t.Fatal("accepted unsafe", kind)
		}
		if err = os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("PASS: real protected cron publication and SQLite command match; missing/custom jobs preserved; no guarded journal or ACK changed. Evidence:", root)
}
