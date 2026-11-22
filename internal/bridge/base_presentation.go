package bridge

import (
	"context"
	"path/filepath"

	"fluxo/internal/services/executiondomain"
)

// BaseInstallerRefusal selects advice only after admission has failed. It never
// grants admission, changes the original error or writes installation state.
// Unreadable evidence deliberately yields review, rather than guessing a repair.
func BaseInstallerRefusal(ctx context.Context, dataDir string, admission BaseAdmission) string {
	if pending, err := BaseFreshStatus(dataDir); err == nil && pending != nil && pending.Phase != "complete" {
		return "recovery"
	}
	if admission.Mode != "bridge" {
		return "review"
	}
	intent, err := readInstallerExecutionIntent(filepath.Join(dataDir, installerExecutionRecord))
	if err != nil {
		return "review"
	}
	intent, guarded, err := guardedInstallerIntent(dataDir, intent)
	if err != nil {
		return "review"
	}
	if guarded || intent != nil && intent.Phase != "complete" {
		return "recovery"
	}
	policy, err := readInstallerSchedulerIntent(dataDir)
	if err != nil {
		return "review"
	}
	if installerSchedulerPending(policy) {
		return "recovery"
	}
	floor, err := executiondomain.ReadFloor(dataDir)
	if err != nil {
		return "review"
	}
	if floor == nil {
		return "bridge"
	} // Older/partial installations need the bridge.
	if floor.Phase != "committed" {
		return "recovery"
	}
	// A current bridge with an unrelated schema/index diagnostic can still have
	// unfinished runtime work. Do not replace that work's recovery instructions.
	path, cleanup, err := snapshot(ctx, dataDir)
	if err != nil {
		return "review"
	}
	defer cleanup()
	db, err := openSQLite(ctx, path, false)
	if err != nil {
		return "review"
	}
	defer db.Close()
	if err := checkPending(ctx, db); err != nil {
		return "recovery"
	}
	return "bridge"
}
