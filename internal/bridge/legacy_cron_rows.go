package bridge

// Historical repair only. The clean base retains ordinary registration but
// does not import this writer. Files already control the plaintext cron jobs;
// this repairs only a complete, unique permutation of their database labels.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"fluxo/internal/safeinput"
	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/scheduler"
	"golang.org/x/sys/unix"
)

type legacyCronRow struct {
	ID         int    `json:"id"`
	SiteID     int    `json:"site_id"`
	Name       string `json:"name"`
	Expression string `json:"expression"`
	Command    string `json:"command"`
	User       string `json:"user"`
}

type legacyCronRowsBackup struct {
	Format   int             `json:"format"`
	Before   []legacyCronRow `json:"before"`
	After    []legacyCronRow `json:"after"`
	Triggers map[int]string  `json:"triggers"`
}

type legacyCronRowsIO struct {
	read         func(int) ([]byte, error)
	unregistered func([]int) error
	pending      func(*sql.Conn) (bool, error)
	archive      func(legacyCronRowsBackup) error
}

// RestoreLegacyCronRows preserves every cron file, numeric ID, execution
// command, schedule and enabled intent. It never registers or launches work.
// The protected backup precedes one SQLite transaction; an interruption leaves
// either the entire old row set or the entire aligned row set, with no pending
// authority format or recovery protocol for a future base to adopt.
func RestoreLegacyCronRows(ctx context.Context, db *sql.DB, dataDir string) ([]int, error) {
	if os.Geteuid() != 0 || dataDir != "/var/lib/fluxo" {
		return nil, nil // Do not impose an installer repair on another layout.
	}
	read := func(id int) ([]byte, error) {
		return readProtected(scheduler.CronPath(id), 1<<20)
	}
	// A cheap preflight avoids acquiring host-wide execution gates on healthy
	// installations. The complete snapshot is recomputed under all gates below.
	rows, err := readLegacyCronRows(ctx, db)
	if err != nil {
		return nil, err
	}
	after, _, err := planLegacyCronRows(rows, read)
	if err != nil || len(after) == 0 {
		return nil, err
	}
	var dbPath string
	if err := db.QueryRowContext(ctx, "SELECT file FROM pragma_database_list WHERE name='main'").Scan(&dbPath); err != nil {
		return nil, err
	}
	if dbPath != filepath.Join(dataDir, "fluxo.db") {
		return nil, errors.New("legacy cron record repair database differs from the installed scope")
	}
	if _, err := protectedInfo(dbPath); err != nil {
		return nil, err
	}
	release, err := packages.Lifecycle(ctx, false)
	if err != nil {
		return nil, err
	}
	defer release()
	locks, err := acquireActivationLocks(ctx)
	if err != nil {
		return nil, err
	}
	defer locks.release()
	return restoreLegacyCronRows(ctx, db, legacyCronRowsIO{
		read: read,
		unregistered: func(ids []int) error {
			if err := legacyCronIDsUnregistered(ids); err != nil {
				return err
			}
			return locks.verify()
		},
		pending: func(conn *sql.Conn) (bool, error) {
			intent, err := readInstallerExecutionIntent(filepath.Join(dataDir, installerExecutionRecord))
			if err != nil {
				return false, err
			}
			policy, err := readInstallerSchedulerIntent(dataDir)
			if err != nil {
				return false, err
			}
			if intent != nil && intent.Phase != "complete" || installerSchedulerPending(policy) {
				return true, nil
			}
			// Both journal gates and the SQLite write reservation are held.
			// Do not reacquire Scheduler.Pending's gate or use another pooled
			// connection here. The ordinary reader/holds check is below.
			var pending bool
			err = conn.QueryRowContext(ctx, `SELECT
				EXISTS(SELECT 1 FROM php_update_operations WHERE kind!='check' AND status IN ('queued','running')) OR
				EXISTS(SELECT 1 FROM php_update_generations WHERE state NOT IN ('succeeded','rolled_back')) OR
				EXISTS(SELECT 1 FROM php_update_barriers) OR
				EXISTS(SELECT 1 FROM php_repository_migrations WHERE state='pending') OR
				EXISTS(SELECT 1 FROM node_update_operations WHERE kind!='check' AND status IN ('queued','running')) OR
				EXISTS(SELECT 1 FROM node_update_generations WHERE state NOT IN ('succeeded','rolled_back')) OR
				EXISTS(SELECT 1 FROM node_update_barriers) OR
				EXISTS(SELECT 1 FROM node_update_php_barriers)`).Scan(&pending)
			return pending, err
		},
		archive: func(b legacyCronRowsBackup) error { return archiveLegacyCronRows(dataDir, b) },
	})
}

type legacyCronRowsQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readLegacyCronRows(ctx context.Context, db legacyCronRowsQuery) ([]legacyCronRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,site_id,COALESCE(name,''),expression,command,user FROM crons
		WHERE site_id=0 AND name IN ('System Cleanup','Renew SSL Certificates','Update Composer') ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []legacyCronRow
	for rows.Next() {
		var row legacyCronRow
		if err := rows.Scan(&row.ID, &row.SiteID, &row.Name, &row.Expression, &row.Command, &row.User); err != nil {
			return nil, err
		}
		result = append(result, row)
		if len(result) > 3 {
			return nil, nil // Duplicates are intentional/ambiguous, never merged.
		}
	}
	return result, rows.Err()
}

func planLegacyCronRows(before []legacyCronRow, read func(int) ([]byte, error)) ([]legacyCronRow, map[int]string, error) {
	if len(before) != 3 {
		return nil, nil, nil
	}
	names := map[string]bool{}
	for _, row := range before {
		if row.ID <= 0 || row.SiteID != 0 || row.User != "root" || names[row.Name] || !safeinput.ValidateCronExpression(row.Expression) {
			return nil, nil, nil
		}
		supported := row.Name == "System Cleanup" && (row.Command == "apt-get autoremove -y && apt-get autoclean" || row.Command == maintenancetask.PackageCleanupCommand) ||
			row.Name == "Renew SSL Certificates" && row.Command == maintenancetask.CertificateRenewalCommand ||
			row.Name == "Update Composer" && row.Command == maintenancetask.ComposerUpdateCommand
		if !supported {
			return nil, nil, nil
		}
		names[row.Name] = true
	}
	after := make([]legacyCronRow, 0, 3)
	triggers := map[int]string{}
	used := map[string]bool{}
	changed := false
	for _, destination := range before {
		raw, err := read(destination.ID)
		if os.IsNotExist(err) {
			return nil, nil, nil // Absence does not establish enabled intent.
		}
		if err != nil {
			return nil, nil, err
		}
		var matches []legacyCronRow
		for _, source := range before {
			c := maintenancetask.Contract{ID: destination.ID, Expression: source.Expression, Command: source.Command, User: source.User}
			if string(raw) == maintenancetask.LegacyContent(c) {
				source.ID = destination.ID
				matches = append(matches, source)
			}
		}
		if len(matches) != 1 || used[matches[0].Name] {
			return nil, nil, nil // Custom bytes or a non-bijective match: preserve.
		}
		used[matches[0].Name] = true
		after = append(after, matches[0])
		triggers[destination.ID] = string(raw)
		changed = changed || matches[0] != destination
	}
	if !changed {
		return nil, nil, nil
	}
	return after, triggers, nil
}

func restoreLegacyCronRows(ctx context.Context, db *sql.DB, fs legacyCronRowsIO) ([]int, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return nil, err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	before, err := readLegacyCronRows(ctx, conn)
	if err != nil {
		return nil, err
	}
	after, triggers, err := planLegacyCronRows(before, fs.read)
	if err != nil || len(after) == 0 {
		return nil, err
	}
	ids := make([]int, len(before))
	for i, row := range before {
		ids[i] = row.ID
	}
	if pending, err := fs.pending(conn); err != nil {
		return nil, err
	} else if pending {
		return nil, errors.New("legacy cron record alignment deferred during pending maintenance or installer recovery")
	}
	if err := fs.unregistered(ids); err != nil {
		return nil, err
	}
	if err := fs.archive(legacyCronRowsBackup{Format: 1, Before: before, After: after, Triggers: triggers}); err != nil {
		return nil, err
	}
	for i, row := range after {
		old := before[i]
		result, err := conn.ExecContext(ctx, `UPDATE crons SET name=?,expression=?,command=?,user=?
			WHERE id=? AND site_id=0 AND name=? AND expression=? AND command=? AND user=?`,
			row.Name, row.Expression, row.Command, row.User, old.ID, old.Name, old.Expression, old.Command, old.User)
		if err != nil {
			return nil, err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return nil, errors.New("legacy cron row changed during alignment")
		}
	}
	for id, expected := range triggers {
		actual, err := fs.read(id)
		if err != nil || !bytes.Equal(actual, []byte(expected)) {
			return nil, errors.New("legacy cron file changed during record alignment")
		}
	}
	current, err := readLegacyCronRows(ctx, conn)
	if err != nil || len(current) != len(after) {
		return nil, errors.New("legacy cron rows changed before alignment commit")
	}
	for i := range current {
		if current[i] != after[i] {
			return nil, errors.New("legacy cron rows differ from the exact alignment")
		}
	}
	if err := fs.unregistered(ids); err != nil {
		return nil, err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return nil, err
	}
	return ids, nil
}

// The caller holds both existing coordination gates. Use the ordinary full
// readers first, then reject every target reference, including disabled entries
// and terminal invocations. No authority is initialized, compacted or rewritten.
func legacyCronIDsUnregistered(ids []int) error {
	if _, err := scheduler.BridgeRegisteredIDs(); err != nil {
		return err
	}
	if _, err := maintenancetask.BridgeRegisteredIDs(); err != nil {
		return err
	}
	for _, path := range []string{"/var/lib/fluxo-scheduler/state.json", "/var/lib/fluxo-system-tasks/state.json"} {
		raw, err := readProtected(path, 8<<20)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if err := legacyCronSnapshotUnregistered(raw, ids); err != nil {
			return err
		}
	}
	return nil
}

func legacyCronSnapshotUnregistered(raw []byte, ids []int) error {
	var state struct {
		Holds       map[string]string          `json:"holds"`
		Upgrade     json.RawMessage            `json:"upgrade"`
		Entries     map[string]json.RawMessage `json:"entries"`
		Invocations map[string]struct {
			Entry struct {
				ID int `json:"id"`
			} `json:"entry"`
			Registration struct {
				Contract struct {
					ID int `json:"id"`
				} `json:"contract"`
			} `json:"registration"`
		} `json:"invocations"`
	}
	if json.Unmarshal(raw, &state) != nil || state.Entries == nil || state.Invocations == nil {
		return errors.New("incomplete execution evidence for legacy cron row alignment")
	}
	if len(state.Holds) != 0 || len(state.Upgrade) != 0 && !bytes.Equal(bytes.TrimSpace(state.Upgrade), []byte("null")) {
		return errors.New("legacy cron record alignment deferred during Scheduler maintenance")
	}
	registered := map[int]bool{}
	for key := range state.Entries {
		id, err := strconv.Atoi(key)
		if err != nil || id <= 0 {
			return errors.New("invalid registration identity in legacy cron alignment evidence")
		}
		registered[id] = true
	}
	for _, id := range ids {
		if registered[id] {
			return fmt.Errorf("scheduled job %d already has registration evidence; preserve it for review", id)
		}
		for _, invocation := range state.Invocations {
			if invocation.Entry.ID == id || invocation.Registration.Contract.ID == id {
				return fmt.Errorf("scheduled job %d has retained execution evidence; preserve it for review", id)
			}
		}
	}
	return nil
}

func archiveLegacyCronRows(dataDir string, backup legacyCronRowsBackup) error {
	dir := filepath.Join(dataDir, "cron-migration-backups")
	if err := trustedDirectory(dataDir); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	if err := trustedDirectory(dir); err != nil {
		return err
	}
	raw, err := json.Marshal(backup)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "system-job-rows-"+digest(raw)+".json")
	if existing, err := readProtected(path, 1<<20); err == nil {
		if !bytes.Equal(existing, raw) {
			return errors.New("legacy cron row backup differs")
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		file, err := os.CreateTemp(dir, ".system-job-rows-*")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		if _, err = file.Write(raw); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if err := unix.Renameat2(unix.AT_FDCWD, file.Name(), unix.AT_FDCWD, path, unix.RENAME_NOREPLACE); err != nil {
			return err
		}
	}
	if err := syncActivationDirectory(dir); err != nil {
		return err
	}
	return syncActivationDirectory(dataDir)
}
