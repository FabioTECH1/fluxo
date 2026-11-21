package bridge

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/scheduler"
)

// ReconcileRuntimeSchedulerPolicy is ongoing current-runtime maintenance. It
// never runs historical converters, edits application rows, stops the panel or
// switches/restarts a cron engine. The existing publication ledger owns crash
// recovery and fresh cron acknowledgements; no new intent format is introduced.
func ReconcileRuntimeSchedulerPolicy(ctx context.Context, dataDir string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return retryRuntimeSchedulerPolicy(ctx, func() error { return reconcileRuntimeSchedulerPolicyOnce(ctx, dataDir) }, func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
			return nil
		}
	})
}

// A brief cron receipt can appear during qualification. Each complete attempt
// releases lifecycle/package/native/coordination/SQLite locks before waiting,
// so neither a receipt nor a launcher waits behind our own retry. Only the
// existing typed provider-drain or protected-directory snapshot refusal is retryable, always before publication
// intent; changed/unsafe inputs and every other error remain hard refusals.
func retryRuntimeSchedulerPolicy(ctx context.Context, attempt func() error, pause func(context.Context) error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := attempt()
		if !scheduler.RuntimeLoaderObservationPending(err) {
			return err
		}
		if err := pause(ctx); err != nil {
			return err
		}
	}
}

func reconcileRuntimeSchedulerPolicyOnce(ctx context.Context, dataDir string) error {
	if os.Geteuid() != 0 || dataDir != "/var/lib/fluxo" {
		return errors.New("automatic Scheduler policy renewal requires the installed root scope")
	}
	needed, err := scheduler.RuntimeLoaderPolicyNeeded(ctx)
	if err != nil || !needed {
		return err
	}
	if err := runtimeSchedulerInstallerSettled(dataDir); err != nil {
		return err
	}
	report, err := executiondomain.InspectCompatibility(dataDir)
	if err != nil {
		return err
	}
	if !report.Compatible {
		return errors.New("recover incomplete execution compatibility before Scheduler policy renewal")
	}
	image, err := readCertifyingExecutable()
	if err != nil {
		return err
	}
	imageSHA := digest(image)
	release, err := packages.Lifecycle(ctx, false)
	if err != nil {
		return err
	}
	defer release()
	releasePackages, err := packages.Acquire(ctx)
	if err != nil {
		return err
	}
	defer releasePackages()
	native, err := packages.NativeDPKG(ctx)
	if err != nil {
		return err
	}
	defer native()
	binding, err := runtimeSchedulerPanelBinding(ctx, dataDir, imageSHA)
	if err != nil {
		return err
	}
	if err = runtimeSchedulerInstallerSettled(dataDir); err != nil {
		return err
	}
	if err = scheduler.Quiescent(ctx); err != nil {
		return err
	}
	if err = maintenancetask.ExecutionDomainsQuiescent(ctx); err != nil {
		return err
	}
	unlock, err := lockCoordination(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	db, err := open(ctx, dataDir, true)
	if err != nil {
		return err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	if err = runtimeSchedulerDatabaseIdle(ctx, conn); err != nil {
		return err
	}
	if err = runtimeSchedulerInstallerSettled(dataDir); err != nil {
		return err
	}
	plan, err := scheduler.PlanRuntimeLoaderPolicyLocked(ctx)
	if err != nil || plan == nil {
		return err
	}
	again, err := runtimeSchedulerPanelBinding(ctx, dataDir, imageSHA)
	if err != nil || again != binding {
		return errors.New("running panel changed before Scheduler policy publication")
	}
	if err = runtimeSchedulerInstallerSettled(dataDir); err != nil {
		return err
	}
	if err = runtimeSchedulerDatabaseIdle(ctx, conn); err != nil {
		return err
	}
	// Apply persists its full protected intent before changing nonces/triggers.
	// Do not wait for ACKs while owning exclusivity: real cron workers need it.
	if err := scheduler.ApplyRuntimeLoaderPolicyLocked(ctx, *plan); err != nil {
		return err
	}
	log.Print("Scheduler compatibility refreshed for the current engine; saved jobs and history preserved")
	return nil
}

func runtimeSchedulerPanelBinding(ctx context.Context, dataDir, imageSHA string) (string, error) {
	path, err := os.Executable()
	if err != nil || filepath.Clean(path) != "/usr/local/bin/fluxo" {
		return "", errors.New("automatic Scheduler renewal requires the installed running panel")
	}
	binding, active, err := installerPanelBinding(ctx, dataDir, imageSHA)
	if err != nil {
		return "", err
	}
	if !active {
		return "", errors.New("automatic Scheduler renewal requires an active panel")
	}
	pid, err := run(ctx, "/usr/bin/systemctl", "show", "fluxo.service", "--property=MainPID", "--value")
	if err != nil || strings.TrimSpace(pid) != strconv.Itoa(os.Getpid()) {
		return "", errors.New("only the current panel main process may renew Scheduler policy automatically")
	}
	return binding, nil
}

func runtimeSchedulerInstallerSettled(dataDir string) error {
	intent, err := readInstallerExecutionIntent(filepath.Join(dataDir, installerExecutionRecord))
	if err != nil {
		return err
	}
	policy, err := readInstallerSchedulerIntent(dataDir)
	if err != nil {
		return err
	}
	if intent != nil && intent.Phase != "complete" || installerSchedulerPending(policy) {
		return errors.New("finish the exact interrupted installation before automatic Scheduler renewal")
	}
	return nil
}

func runtimeSchedulerDatabaseIdle(ctx context.Context, q queryer) error {
	checks := []struct{ table, predicate string }{
		{"php_update_operations", "status IN ('queued','running')"}, {"node_update_operations", "status IN ('queued','running')"},
		{"php_update_generations", "state NOT IN ('succeeded','rolled_back')"}, {"node_update_generations", "state NOT IN ('succeeded','rolled_back')"},
		{"php_repository_migrations", "state='pending'"}, {"php_update_barriers", "1"}, {"node_update_barriers", "1"}, {"node_update_php_barriers", "1"},
	}
	for _, c := range checks {
		var n int
		if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+c.table+" WHERE "+c.predicate).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("automatic Scheduler renewal is deferred while %s is pending", c.table)
		}
	}
	return nil
}
