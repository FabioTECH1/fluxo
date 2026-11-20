package cron

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/services/maintenancetask"
	_ "modernc.org/sqlite"
)

type cleanupFixture struct {
	db        *sql.DB
	trigger   []byte
	backup    *cleanupBackup
	published int
	fs        cleanupMigrationIO
}

func newCleanupFixture(t *testing.T) *cleanupFixture {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "fluxo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec(`CREATE TABLE crons(id INTEGER PRIMARY KEY,site_id INTEGER,name TEXT,expression TEXT,command TEXT,user TEXT);
	INSERT INTO crons VALUES(17,0,'System Cleanup','13 2 * * 6','apt-get autoremove -y && apt-get autoclean','root')`); err != nil {
		t.Fatal(err)
	}
	c, _ := maintenancetask.Qualify(17, 0, "13 2 * * 6", maintenancetask.PackageCleanupCommand, "root")
	c.Command = legacyCleanupCommand
	f := &cleanupFixture{db: db, trigger: []byte(maintenancetask.LegacyContent(c))}
	f.fs = cleanupMigrationIO{
		read: func(int) ([]byte, error) {
			if f.trigger == nil {
				return nil, os.ErrNotExist
			}
			return append([]byte{}, f.trigger...), nil
		},
		archive: func(b cleanupBackup, requireExisting bool) error {
			if f.backup != nil {
				if *f.backup != b {
					return errors.New("changed backup")
				}
				return nil
			}
			if requireExisting {
				return errors.New("missing prior backup")
			}
			f.backup = &b
			return nil
		},
		publish: func(id int, before, after []byte) error {
			if id != 17 || string(before) != string(f.trigger) {
				return errors.New("trigger changed")
			}
			f.published++
			f.trigger = append([]byte{}, after...)
			return nil
		},
		registered: func(context.Context, int) (bool, error) { return false, nil },
		pending:    func() (bool, error) { return false, nil },
	}
	return f
}

func (f *cleanupFixture) command(t *testing.T) string {
	t.Helper()
	var command string
	if err := f.db.QueryRow("SELECT command FROM crons WHERE id=17").Scan(&command); err != nil {
		t.Fatal(err)
	}
	return command
}

func TestLegacyCleanupPublishesBeforeCommittingCanonicalRow(t *testing.T) {
	f := newCleanupFixture(t)
	original := string(f.trigger)
	publish := f.fs.publish
	f.fs.publish = func(id int, before, after []byte) error {
		if got := f.command(t); got != legacyCleanupCommand {
			t.Fatalf("DB changed before publication: %q", got)
		}
		if f.backup == nil || f.backup.Trigger != original {
			t.Fatal("original trigger was not archived before publication")
		}
		return publish(id, before, after)
	}
	changed, err := normalizeLegacyCleanup(context.Background(), f.db, 17, f.fs)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	if got := f.command(t); got != maintenancetask.PackageCleanupCommand {
		t.Fatal("canonical DB command missing", got)
	}
	if !strings.Contains(string(f.trigger), "13 2 * * 6 root "+maintenancetask.PackageCleanupCommand) || strings.Contains(string(f.trigger), "system-task-receipt") {
		t.Fatal("schedule changed or registration/ACK was invented")
	}
	changed, err = normalizeLegacyCleanup(context.Background(), f.db, 17, f.fs)
	if err != nil || changed || f.published != 1 {
		t.Fatal("completed conversion was not idempotent", changed, err)
	}
}

func TestLegacyCleanupRetryAfterPublishedTriggerAndDatabaseFailure(t *testing.T) {
	for _, point := range []string{"publication return", "database write", "context cancellation"} {
		t.Run(point, func(t *testing.T) {
			f := newCleanupFixture(t)
			publish := f.fs.publish
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if point == "database write" {
				if _, err := f.db.Exec(`CREATE TRIGGER fail_update BEFORE UPDATE ON crons BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			f.fs.publish = func(id int, before, after []byte) error {
				if err := publish(id, before, after); err != nil {
					return err
				}
				if point == "publication return" {
					return errors.New("simulated interruption after rename")
				}
				if point == "context cancellation" {
					cancel()
				}
				return nil
			}
			if changed, err := normalizeLegacyCleanup(ctx, f.db, 17, f.fs); err == nil || changed {
				t.Fatal("expected bounded failure", changed, err)
			}
			if got := f.command(t); got != legacyCleanupCommand {
				t.Fatal("failed transaction changed the DB", got)
			}
			if f.backup == nil || !strings.Contains(string(f.trigger), maintenancetask.PackageCleanupCommand) {
				t.Fatal("recovery pair missing")
			}
			f.fs.publish = publish
			if _, err := f.db.Exec("DROP TRIGGER IF EXISTS fail_update"); err != nil {
				t.Fatal(err)
			}
			if changed, err := normalizeLegacyCleanup(context.Background(), f.db, 17, f.fs); err != nil || !changed {
				t.Fatal("same normal path did not recover", changed, err)
			}
		})
	}
}

func TestLegacyCleanupPreservesMissingModifiedAndGuardedJobs(t *testing.T) {
	cases := map[string]func(*cleanupFixture){
		"missing":     func(f *cleanupFixture) { f.trigger = nil },
		"custom file": func(f *cleanupFixture) { f.trigger = []byte("custom root command\n") },
		"custom row":  func(f *cleanupFixture) { f.db.Exec("UPDATE crons SET command='printf custom'") },
		"other name":  func(f *cleanupFixture) { f.db.Exec("UPDATE crons SET name='Other job'") },
		"site job":    func(f *cleanupFixture) { f.db.Exec("UPDATE crons SET site_id=9") },
		"other user":  func(f *cleanupFixture) { f.db.Exec("UPDATE crons SET user='fluxo'") },
		"guarded": func(f *cleanupFixture) {
			f.fs.registered = func(context.Context, int) (bool, error) { return true, nil }
		},
		"pending": func(f *cleanupFixture) { f.fs.pending = func() (bool, error) { return true, nil } },
		"pending evidence error": func(f *cleanupFixture) {
			f.fs.pending = func() (bool, error) { return false, errors.New("unavailable") }
		},
		"unsafe input": func(f *cleanupFixture) { f.fs.read = func(int) ([]byte, error) { return nil, errors.New("unsafe") } },
		"backup failure": func(f *cleanupFixture) {
			f.fs.archive = func(cleanupBackup, bool) error { return errors.New("disk full") }
		},
		"canonical file without backup": func(f *cleanupFixture) {
			c, _ := maintenancetask.Qualify(17, 0, "13 2 * * 6", maintenancetask.PackageCleanupCommand, "root")
			f.trigger = []byte(maintenancetask.LegacyContent(c))
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newCleanupFixture(t)
			setup(f)
			beforeCommand, beforeFile := f.command(t), string(f.trigger)
			changed, _ := normalizeLegacyCleanup(context.Background(), f.db, 17, f.fs)
			if changed || f.published != 0 || beforeCommand != f.command(t) || beforeFile != string(f.trigger) {
				t.Fatal("preservation failed")
			}
		})
	}
}

func TestCronMutationReservationCanCancelAndSerializesPublication(t *testing.T) {
	unlock, err := reserveMutation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := reserveMutation(ctx); !errors.Is(err, context.DeadlineExceeded) {
		unlock()
		t.Fatal("reservation did not honor deadline", err)
	}
	unlock()
	second, err := reserveMutation(context.Background())
	if err != nil {
		t.Fatal(fmt.Sprint(err))
	}
	second()
}
