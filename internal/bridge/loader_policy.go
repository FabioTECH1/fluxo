package bridge

import (
	"context"
	"errors"
	"os"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/scheduler"
	"golang.org/x/mod/semver"
)

// LoaderPolicyCommand uses the same installed scope and lock order as bridge
// certification. It never edits a bridge receipt, database row or reader floor.
func LoaderPolicyCommand(ctx context.Context, dataDir, version, action string, expectations []string) (any, error) {
	if os.Geteuid() != 0 || dataDir != "/var/lib/fluxo" {
		return nil, errors.New("loader policy commands require root and the default installed data directory")
	}
	if action != "inspect" && action != "requalify" && action != "recover" || action == "inspect" && len(expectations) != 0 || action == "requalify" && len(expectations) != 3 || action == "recover" && len(expectations) != 1 {
		return nil, errors.New("usage: scheduler-loader inspect | requalify EPOCH OLD_POLICY NEW_POLICY | recover PUBLICATION_UUID")
	}
	if _, err := executiondomain.InspectCompatibility(dataDir); err != nil {
		return nil, err
	}
	if err := activationGatesPresent(); err != nil {
		return nil, err
	}
	if action == "inspect" {
		unlock, err := lockCoordination(ctx)
		if err != nil {
			return nil, err
		}
		defer unlock()
		return scheduler.InspectLoaderPolicyLocked(ctx)
	}
	if !semver.IsValid("v" + version) {
		return nil, errors.New("loader policy publication requires a versioned installed bridge image")
	}
	if executable, err := os.Executable(); err != nil || executable != "/usr/local/bin/fluxo" {
		return nil, errors.New("run the installed /usr/local/bin/fluxo for loader policy publication")
	}
	if _, err := readCertifyingExecutable(); err != nil {
		return nil, err
	}
	release, err := packages.Lifecycle(ctx, false)
	if err != nil {
		return nil, err
	}
	defer release()
	releasePackages, err := packages.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer releasePackages()
	releaseNative, err := packages.NativeDPKG(ctx)
	if err != nil {
		return nil, err
	}
	defer releaseNative()
	if err = panelStopped(ctx); err != nil {
		return nil, err
	}
	if err = packages.TinkerDomainsIdle(); err != nil {
		return nil, err
	}
	if action == "recover" {
		err = scheduler.LoaderPolicyRecoveryQuiescent(ctx, expectations[0])
	} else {
		err = scheduler.Quiescent(ctx)
	}
	if err != nil {
		return nil, err
	}
	if err = maintenancetask.ExecutionDomainsQuiescent(ctx); err != nil {
		return nil, err
	}
	unlock, err := lockCoordination(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if _, err = executiondomain.InspectCompatibility(dataDir); err != nil {
		return nil, err
	}
	db, err := open(ctx, dataDir, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return nil, err
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	if err = checkPending(ctx, conn); err != nil {
		return nil, err
	}
	if err = panelStopped(ctx); err != nil {
		return nil, err
	}
	if action == "recover" {
		err = scheduler.RecoverLoaderPolicyPublicationLocked(ctx, expectations[0])
		return map[string]any{"publication": expectations[0], "recovered": err == nil, "readiness_verified": false}, err
	}
	return scheduler.RequalifyLoaderPolicyLocked(ctx, expectations[0], expectations[1], expectations[2], func() error { return panelStopped(ctx) })
}
