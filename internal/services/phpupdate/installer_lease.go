package phpupdate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/scheduler"
)

// VerifySchedulerPanel uses the installer's admission and drain contract without
// changing packages, stopping the panel, or resetting any maintenance history.
func VerifySchedulerPanel(ctx context.Context, dataDir string) error {
	if os.Geteuid() != 0 || !filepath.IsAbs(dataDir) {
		return errors.New("Scheduler verification requires root and an absolute Fluxo data directory")
	}
	release, err := packages.Lifecycle(ctx, false)
	if err != nil {
		return err
	}
	defer release()
	if err = CheckInstallerDatabase(ctx, dataDir); err != nil {
		return err
	}
	return scheduler.VerifyPanel(ctx)
}

// HoldInstallerLease closes admission before the installer stops the HTTP
// daemon or snapshots SQLite/cron. A verified root signal releases it after
// successful health checks. A killed helper leaves a durable marker for retry.
func HoldInstallerLease(dataDir string, parentPID int, output io.Writer) error {
	if os.Geteuid() != 0 || !filepath.IsAbs(dataDir) {
		return ErrSupervisor
	}
	if parentPID <= 0 {
		return errors.New("Invalid installer parent")
	}
	parentRaw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", parentPID))
	if err != nil {
		return err
	}
	parentFields := strings.Fields(string(parentRaw)[strings.LastIndex(string(parentRaw), ")")+1:])
	if len(parentFields) < 20 {
		return errors.New("Invalid installer parent identity")
	}
	parentKey := parentFields[19]
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	release, err := packages.Lifecycle(ctx, false)
	if err != nil {
		return err
	}
	defer release()
	if err = scheduler.ReconcileInstallerDomains(ctx); err != nil {
		return err
	}
	if err = CheckInstallerUpgradeDatabase(ctx, dataDir); err != nil {
		return err
	}
	proof, err := scheduler.PanelBeforeStop(ctx)
	if err != nil {
		return err
	}
	id, err := scheduler.BeginUpgrade(ctx)
	if err != nil {
		return err
	}
	if err = scheduler.SavePanelProof(ctx, proof); err != nil {
		_ = scheduler.EndUpgrade(context.Background(), id)
		return err
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGUSR1)
	defer signal.Stop(signals)
	if _, err = fmt.Fprintln(output, "READY "+id); err != nil {
		return err
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-signals:
			return scheduler.EndUpgrade(context.Background(), id)
		case <-tick.C:
			raw, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", parentPID))
			if e != nil {
				return errors.New("Installer exited before releasing its lease; retry the installer")
			}
			fields := strings.Fields(string(raw)[strings.LastIndex(string(raw), ")")+1:])
			if len(fields) < 20 || fields[19] != parentKey {
				return errors.New("Installer identity changed")
			}
		}
	}
}

// Rechecked after the old panel stops: older releases can still enqueue checks
// while the exclusive lease blocks their execution. Never snapshot that race.
func CheckInstallerDatabase(ctx context.Context, dataDir string) error {
	return checkInstallerDatabase(ctx, dataDir, false)
}

// Only the authenticated installer lease and installed orchestration phase use
// this admission. Ordinary runtime maintenance still requires exact old
// registration readiness through CheckInstallerDatabase.
func CheckInstallerUpgradeDatabase(ctx context.Context, dataDir string) error {
	return checkInstallerDatabase(ctx, dataDir, true)
}

// Only read-only check workers may finish during an automatic installer retry.
// Runtime callers still refuse this error; mutations, recovery generations and
// barriers retain the ordinary explicit-maintenance refusal.
var ErrInstallerChecksPending = errors.New("Finish PHP or Node.js checks and maintenance before upgrading Fluxo")

func installerOnlyChecks(ctx context.Context, db *sql.DB, table string) (bool, error) {
	if table != "php_update_operations" && table != "node_update_operations" {
		return false, nil
	}
	var mutations int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE status IN ('queued','running') AND (kind IS NULL OR kind <> 'check')").Scan(&mutations)
	return mutations == 0, err
}

func checkInstallerDatabase(ctx context.Context, dataDir string, installer bool) error {
	if err := scheduler.Quiescent(ctx); err != nil {
		return err
	}
	var taskErr error
	if installer {
		taskErr = maintenancetask.InstallerQuiescent(ctx)
	} else {
		taskErr = maintenancetask.Quiescent(ctx)
	}
	if taskErr != nil {
		return taskErr
	}

	if !filepath.IsAbs(dataDir) {
		return errors.New("Installer maintenance checks require an absolute data directory")
	}
	path := filepath.Join(dataDir, "fluxo.db")
	if err := rootOwned(path); err != nil {
		return err
	}
	uri := url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite", uri.String()+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return err
	}
	admissionErr := checkInstallerMaintenanceDatabase(ctx, db)
	if err = db.Close(); err != nil {
		return err
	}
	return admissionErr
}

func checkInstallerMaintenanceDatabase(ctx context.Context, db *sql.DB) error {
	checksPending := false
	// Older releases without these tables cannot own a maintenance generation.
	for _, check := range []struct{ table, predicate string }{
		{"php_update_operations", "status IN ('queued','running')"},
		{"php_update_generations", "state NOT IN ('succeeded','rolled_back')"},
		{"php_update_barriers", "1"},
		{"php_repository_migrations", "state='pending'"},
		{"node_update_operations", "status IN ('queued','running')"},
		{"node_update_generations", "state NOT IN ('succeeded','rolled_back')"},
		{"node_update_barriers", "1"},
		{"node_update_php_barriers", "1"},
	} {
		table, predicate := check.table, check.predicate
		var exists, count int
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			continue
		}
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE "+predicate).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			checks, checkErr := installerOnlyChecks(ctx, db, table)
			if checkErr != nil {
				return checkErr
			}
			if checks {
				checksPending = true
				continue
			}
			return errors.New("Finish PHP or Node.js checks and maintenance before upgrading Fluxo")
		}
	}
	// All tables must be inspected before a read-only retry is admitted. A
	// check in one subsystem cannot mask a mutation, generation or barrier in
	// another. A retry is returned only after every possible hard blocker.
	if checksPending {
		return ErrInstallerChecksPending
	}
	return nil
}
