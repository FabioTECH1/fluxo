package bridge

import (
	"context"

	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/scheduler"
)

// The caller has validated the installer pointer, immutable plan, source
// archives and live file projections before selecting the recovery verifier.
func (r activationRequest) exactHistoricalTaskInstallerReplay(p executionActivation) bool {
	return r.InstallerHistoricalTasks && r.Resume != "" && r.Resume == p.ID && installerHistoricalTaskKind(p.Kind)
}

func verifyHistoricalTaskRetirements(ctx context.Context, request activationRequest, plan executionActivation) error {
	verify := scheduler.VerifyInstallerHistoricalRetirement
	if request.exactHistoricalTaskInstallerReplay(plan) {
		verify = func(ctx context.Context, r scheduler.CronBackendRetirement) error {
			return scheduler.VerifyInstallerRetirementRecovery(ctx, r, func() error {
				return verifyRecoveryInvocationsLocked(ctx, plan)
			})
		}
	}
	for _, r := range plan.HistoricalTaskRetirements {
		if err := verify(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

func verifyRecoveryInvocationsLocked(ctx context.Context, plan executionActivation) error {
	for _, f := range plan.Files {
		// An originally absent core has no archived invocations. Both accepted
		// projections are checked under the initializer and execution gates.
		if f.Path == "/var/lib/fluxo-scheduler/state.json" && f.Before != "absent" {
			if err := scheduler.VerifyInstallerRecoveryInvocationsLocked(ctx); err != nil {
				return err
			}
		}
	}
	return maintenancetask.VerifyInstallerRecoveryInvocationsLocked(ctx)
}
