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

	"fluxo/internal/services/packages"
	"fluxo/internal/services/scheduler"
)

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
	if err = CheckInstallerDatabase(ctx, dataDir); err != nil {
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
	path := filepath.Join(dataDir, "fluxo.db")
	if err := rootOwned(path); err != nil {
		return err
	}
	uri := url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite", uri.String()+"?mode=ro&_pragma=busy_timeout(2000)")
	if err != nil {
		return err
	}
	// Older releases without these tables cannot own a maintenance generation.
	for table, predicate := range map[string]string{"php_update_operations": "status IN ('queued','running')", "php_update_generations": "state NOT IN ('succeeded','rolled_back')", "php_update_barriers": "1", "php_repository_migrations": "state='pending'", "node_update_operations": "status IN ('queued','running')", "node_update_generations": "state NOT IN ('succeeded','rolled_back')", "node_update_barriers": "1"} {
		var exists, count int
		if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&exists); err != nil {
			db.Close()
			return err
		}
		if exists == 0 {
			continue
		}
		if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE "+predicate).Scan(&count); err != nil {
			db.Close()
			return err
		}
		if count > 0 {
			db.Close()
			return errors.New("Finish PHP or Node.js checks and maintenance before upgrading Fluxo")
		}
	}
	if err = db.Close(); err != nil {
		return err
	}
	return nil
}
