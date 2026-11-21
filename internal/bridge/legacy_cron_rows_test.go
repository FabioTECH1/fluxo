package bridge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"fluxo/internal/services/maintenancetask"
)

type legacyCronRowsFixture struct {
	db      *sql.DB
	before  []legacyCronRow
	files   map[int][]byte
	backups map[string]legacyCronRowsBackup
	fs      legacyCronRowsIO
}

func newLegacyCronRowsFixture(t *testing.T, permutation []int) *legacyCronRowsFixture {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "fluxo.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`CREATE TABLE crons(id INTEGER PRIMARY KEY,site_id INTEGER,name TEXT,expression TEXT,command TEXT,user TEXT,created_at TEXT DEFAULT 'original');
		CREATE TABLE preservation(value TEXT); INSERT INTO preservation VALUES('credentials, keys and unrelated history');`)
	if err != nil {
		t.Fatal(err)
	}
	f := &legacyCronRowsFixture{db: db, files: map[int][]byte{}, backups: map[string]legacyCronRowsBackup{}, before: []legacyCronRow{
		{ID: 1, Name: "System Cleanup", Expression: "0 0 * * 0", Command: "apt-get autoremove -y && apt-get autoclean", User: "root"},
		{ID: 2, Name: "Renew SSL Certificates", Expression: "0 */12 * * *", Command: maintenancetask.CertificateRenewalCommand, User: "root"},
		{ID: 3, Name: "Update Composer", Expression: "0 0 * * 0", Command: maintenancetask.ComposerUpdateCommand, User: "root"},
	}}
	for i, row := range f.before {
		if _, err := db.Exec("INSERT INTO crons(id,site_id,name,expression,command,user) VALUES(?,?,?,?,?,?)", row.ID, row.SiteID, row.Name, row.Expression, row.Command, row.User); err != nil {
			t.Fatal(err)
		}
		source := f.before[permutation[i]]
		f.files[row.ID] = []byte(maintenancetask.LegacyContent(maintenancetask.Contract{ID: row.ID, Expression: source.Expression, Command: source.Command, User: source.User}))
	}
	_, err = db.Exec(`INSERT INTO crons(id,site_id,name,expression,command,user) VALUES
		(5,22,'Missing custom site job','* * * * *','custom-wrapper','fluxo'),
		(6,22,'Missing custom health job','*/5 * * * *','custom-health','fluxo')`)
	if err != nil {
		t.Fatal(err)
	}
	f.fs = legacyCronRowsIO{
		read: func(id int) ([]byte, error) {
			raw, ok := f.files[id]
			if !ok {
				return nil, os.ErrNotExist
			}
			return append([]byte(nil), raw...), nil
		},
		pending: func(*sql.Conn) (bool, error) { return false, nil },
		unregistered: func(ids []int) error {
			if !reflect.DeepEqual(ids, []int{1, 2, 3}) {
				t.Fatal("unexpected identities", ids)
			}
			return nil
		},
		archive: func(backup legacyCronRowsBackup) error {
			if backup.Format != 1 || !reflect.DeepEqual(backup.Before, f.before) {
				t.Fatal("original records not archived", backup)
			}
			raw, _ := json.Marshal(backup)
			f.backups[digest(raw)] = backup
			return nil
		},
	}
	return f
}

func (f *legacyCronRowsFixture) rows(t *testing.T) []legacyCronRow {
	t.Helper()
	rows, err := readLegacyCronRows(context.Background(), f.db)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestLegacyCronPermutationAlignsOnlyDescriptionsAndIsIdempotent(t *testing.T) {
	for _, permutation := range [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		t.Run(string(rune('a'+permutation[0]*3+permutation[1])), func(t *testing.T) {
			f := newLegacyCronRowsFixture(t, permutation)
			beforeFiles, _ := json.Marshal(f.files)
			ids, err := restoreLegacyCronRows(context.Background(), f.db, f.fs)
			if err != nil {
				t.Fatal(err)
			}
			identity := reflect.DeepEqual(permutation, []int{0, 1, 2})
			if identity && len(ids) != 0 || !identity && !reflect.DeepEqual(ids, []int{1, 2, 3}) {
				t.Fatal("unexpected repaired IDs", ids)
			}
			for i, row := range f.rows(t) {
				want := f.before[permutation[i]]
				want.ID = i + 1
				if row != want {
					t.Fatal("record does not match its existing file", row, want)
				}
			}
			afterFiles, _ := json.Marshal(f.files)
			if string(beforeFiles) != string(afterFiles) {
				t.Fatal("cron execution changed")
			}
			if ids, err = restoreLegacyCronRows(context.Background(), f.db, f.fs); err != nil || len(ids) != 0 {
				t.Fatal("completed alignment not idempotent", ids, err)
			}
			wantBackups := 1
			if identity {
				wantBackups = 0
			}
			if len(f.backups) != wantBackups {
				t.Fatal("original backup not retained exactly once")
			}
			var preserved, missing, dates int
			f.db.QueryRow("SELECT COUNT(*) FROM preservation WHERE value='credentials, keys and unrelated history'").Scan(&preserved)
			f.db.QueryRow("SELECT COUNT(*) FROM crons WHERE id IN (5,6) AND site_id=22").Scan(&missing)
			f.db.QueryRow("SELECT COUNT(*) FROM crons WHERE created_at='original'").Scan(&dates)
			if preserved != 1 || missing != 2 || dates != 5 || len(f.files) != 3 {
				t.Fatal("unrelated state changed")
			}
		})
	}
}

func TestLegacyCronPermutationPreservesUnprovenInputs(t *testing.T) {
	for _, scenario := range []string{"missing", "custom", "duplicate-file", "duplicate-row", "custom-command", "wrong-user", "site-row", "unsafe-read"} {
		t.Run(scenario, func(t *testing.T) {
			f := newLegacyCronRowsFixture(t, []int{2, 0, 1})
			switch scenario {
			case "missing":
				delete(f.files, 2)
			case "custom":
				f.files[1] = append(f.files[1], []byte("# administrator edit\n")...)
			case "duplicate-file":
				source := f.before[2]
				f.files[2] = []byte(maintenancetask.LegacyContent(maintenancetask.Contract{ID: 2, Expression: source.Expression, Command: source.Command, User: source.User}))
			case "duplicate-row":
				f.db.Exec("INSERT INTO crons SELECT 9,site_id,name,expression,command,user,created_at FROM crons WHERE id=1")
			case "custom-command":
				f.db.Exec("UPDATE crons SET command=command || ' && custom-command' WHERE id=1")
			case "wrong-user":
				f.db.Exec("UPDATE crons SET user='fluxo' WHERE id=1")
			case "site-row":
				f.db.Exec("UPDATE crons SET site_id=22 WHERE id=1")
			case "unsafe-read":
				f.fs.read = func(int) ([]byte, error) { return nil, errors.New("unsafe file owner/alias") }
			}
			before := f.rows(t)
			ids, err := restoreLegacyCronRows(context.Background(), f.db, f.fs)
			if len(ids) != 0 || scenario == "unsafe-read" && err == nil {
				t.Fatal("unproven input repaired", ids, err)
			}
			if !reflect.DeepEqual(before, f.rows(t)) || len(f.backups) != 0 {
				t.Fatal("refusal changed state")
			}
		})
	}
}

func TestLegacyCronPermutationAtomicFailuresAndRetry(t *testing.T) {
	for _, scenario := range []string{"pending", "authority", "backup", "second-update", "commit", "canceled", "file-drift", "late-authority"} {
		t.Run(scenario, func(t *testing.T) {
			f := newLegacyCronRowsFixture(t, []int{2, 0, 1})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			good := f.fs
			switch scenario {
			case "pending":
				f.fs.pending = func(*sql.Conn) (bool, error) { return true, nil }
			case "authority":
				f.fs.unregistered = func([]int) error { return errors.New("retained authority") }
			case "backup":
				f.fs.archive = func(legacyCronRowsBackup) error { return errors.New("disk full") }
			case "second-update":
				f.db.Exec("CREATE TRIGGER refuse_second BEFORE UPDATE ON crons WHEN OLD.id=2 BEGIN SELECT RAISE(ABORT,'failed second update'); END")
			case "commit":
				if _, err := f.db.Exec(`PRAGMA foreign_keys=ON;
					CREATE TABLE backup_parent(id INTEGER PRIMARY KEY);
					CREATE TABLE commit_failure(id INTEGER REFERENCES backup_parent(id) DEFERRABLE INITIALLY DEFERRED);
					CREATE TRIGGER refuse_commit AFTER UPDATE ON crons BEGIN INSERT INTO commit_failure VALUES(999); END;`); err != nil {
					t.Fatal(err)
				}
			case "canceled":
				f.fs.archive = func(b legacyCronRowsBackup) error { good.archive(b); cancel(); return nil }
			case "file-drift":
				calls := 0
				f.fs.read = func(id int) ([]byte, error) {
					calls++
					if calls > 3 {
						return []byte("outside edit"), nil
					}
					return good.read(id)
				}
			case "late-authority":
				calls := 0
				f.fs.unregistered = func([]int) error {
					calls++
					if calls > 1 {
						return errors.New("authority changed")
					}
					return nil
				}
			}
			if ids, err := restoreLegacyCronRows(ctx, f.db, f.fs); err == nil || len(ids) != 0 {
				t.Fatal("fault did not refuse", ids, err)
			}
			if !reflect.DeepEqual(f.before, f.rows(t)) {
				t.Fatal("partial database alignment survived rollback")
			}
			f.db.Exec("DROP TRIGGER IF EXISTS refuse_second")
			f.db.Exec("DROP TRIGGER IF EXISTS refuse_commit")
			if ids, err := restoreLegacyCronRows(context.Background(), f.db, good); err != nil || len(ids) != 3 {
				t.Fatal("same-state retry failed", ids, err)
			}
			if len(f.backups) != 1 {
				t.Fatal("retry lost/duplicated original archive")
			}
		})
	}
}

func TestLegacyCronPermutationRejectsAllRetainedTargetAuthority(t *testing.T) {
	for _, raw := range []string{
		`{"entries":{"1":{"enabled":false}},"invocations":{}}`,
		`{"entries":{"01":{"enabled":false}},"invocations":{}}`,
		`{"entries":{"invalid":{"enabled":false}},"invocations":{}}`,
		`{"entries":{},"invocations":{"old":{"registration":{"contract":{"id":2}}}}}`,
		`{"entries":{},"invocations":{"old":{"entry":{"id":3}}}}`,
		`{"entries":{},"invocations":{},"holds":{"8.4":"maintenance"},"upgrade":null}`,
		`{"entries":{},"invocations":{},"holds":{},"upgrade":{"id":"pending"}}`,
		`{"entries":{},"invocations":null}`,
	} {
		if legacyCronSnapshotUnregistered([]byte(raw), []int{1, 2, 3}) == nil {
			t.Fatal("retained/incomplete authority accepted", raw)
		}
	}
	if err := legacyCronSnapshotUnregistered([]byte(`{"entries":{"4":{"enabled":true}},"invocations":{"other":{"entry":{"id":4}}},"holds":{},"upgrade":null}`), []int{1, 2, 3}); err != nil {
		t.Fatal("unrelated history prevented alignment", err)
	}
}
