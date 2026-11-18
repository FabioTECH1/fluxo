package scheduler

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// VerifyInstallerRetirementRecovery is for replay of an authenticated, immutable
// historical-task installer plan. It returns no replacement identity or receipt.
// The caller holds the lifecycle and execution gates throughout replay.
func VerifyInstallerRetirementRecovery(ctx context.Context, r CronBackendRetirement, settled func() error) error {
	return verifyInstallerRetirementRecovery(r, bootID,
		func() (legacyHost, error) { return RequalifyCronBackend(ctx, r.Saved) },
		func() error { return VerifyInstallerHistoricalRetirement(ctx, r) },
		func(now legacyHost) error { return VerifyCronBackendEmpty(ctx, now) }, settled)
}

func verifyInstallerRetirementRecovery(r CronBackendRetirement, boot func() (string, error), qualify func() (legacyHost, error), strict func() error, drained func(legacyHost) error, settled func() error) error {
	if err := ValidateInstallerHistoricalRetirement(r); err != nil {
		return err
	}
	before, err := boot()
	if err != nil {
		return err
	}
	kernel, err := uuid.Parse(before)
	if err != nil || kernel == uuid.Nil || kernel.String() != before {
		return errors.New("invalid kernel boot identity during historical retirement recovery")
	}
	contractMatches := func(now legacyHost) bool {
		return now.Boot == before && now.Backend == r.Saved.Backend && now.Group == r.Saved.Group && now.Configuration == r.Saved.Configuration
	}
	// Compare UUID values: another spelling of the archived boot does not
	// establish a kernel restart. The archive was validated above.
	if kernel == uuid.MustParse(r.Saved.Boot) || kernel == uuid.MustParse(r.Current.Boot) {
		err := strict()
		if err != nil {
			// Diagnose a further restart even if it raced the strict proof.
			// This observation can only refine the refusal.
			now, probeErr := qualify()
			if probeErr == nil && ValidateBackendIdentity(now) == nil && contractMatches(now) && now != r.Current {
				return errors.New("Cronie changed generation again on the archived boot; preserve the pending operation, arrange an operator-approved reboot, then rerun the same release's normal installer")
			}
		}
		return err
	}
	now, err := qualify()
	if err != nil {
		return err
	}
	if err := ValidateBackendIdentity(now); err != nil {
		return err
	}
	if !contractMatches(now) {
		return errors.New("live Cronie boot or provider configuration differs from the historical recovery contract")
	}
	if err := settled(); err != nil {
		return err
	}
	if err := drained(now); err != nil {
		return err
	}
	after, err := qualify()
	if err != nil {
		return err
	}
	lastBoot, err := boot()
	if err != nil {
		return err
	}
	if lastBoot != before || after != now {
		return errors.New("kernel boot or Cronie identity changed during historical retirement recovery")
	}
	return nil
}

// Caller has validated the exact source/target core under the Scheduler gate.
// Format inspection also works before publication of the prepared reader floor.
func VerifyInstallerRecoveryInvocationsLocked(ctx context.Context) error {
	j, err := readJournal(false)
	if err != nil {
		return err
	}
	for _, v := range j.Invocations {
		if err := invocationQuiescent(ctx, v); err != nil {
			return err
		}
	}
	return nil
}
