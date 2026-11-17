package maintenancetask

import (
	"context"
	"errors"

	"fluxo/internal/services/scheduler"
	"github.com/google/uuid"
)

// Loader acknowledgement and current provider identity are launch admission,
// not whole-provider drainage. A cron dispatcher is itself in that provider's
// cgroup, so it must never wait for that cgroup to become empty. Full legacy
// drainage remains the separate maintenance/cutover-readiness obligation.
func requireLaunchRegistration(ctx context.Context, r registration, snapshot *registration) error {
	boot, err := bootID()
	if err != nil {
		return err
	}
	if err = validateLaunchRegistration(r, snapshot, boot); err != nil {
		return err
	}
	if err = verifyRegistration(ctx, r); err != nil {
		return err
	}
	if err = verifyHelperReaderContracts(ctx, r.Helper); err != nil {
		return err
	}
	return scheduler.VerifyCronBackendCurrent(ctx, r.Backend)
}

// Bridge certification requires a receipt usable by the launch path as well as
// the separate legacy-drain readiness proof. A reboot alone cannot grant it.
func requireCurrentLaunchAcknowledgement(ctx context.Context, r registration) error {
	boot, err := bootID()
	if err != nil {
		return err
	}
	if err = validateLaunchRegistration(r, nil, boot); err != nil {
		return err
	}
	return scheduler.VerifyCronBackendCurrent(ctx, r.Backend)
}

func validateLaunchRegistration(r registration, snapshot *registration, boot string) error {
	if !r.Enabled || !r.Published || !r.Received {
		return errors.New("wait for the exact guarded task publication and real loader acknowledgement before launch")
	}
	if _, err := uuid.Parse(r.Identity); err != nil {
		return errors.New("task launch registration identity is invalid")
	}
	if _, err := uuid.Parse(r.Nonce); err != nil {
		return errors.New("task launch registration nonce is invalid")
	}
	if err := scheduler.ValidateBackendIdentity(r.Backend); err != nil {
		return err
	}
	if r.Boot != boot || r.Backend.Boot != boot {
		return errors.New("wait for a real current-boot task loader acknowledgement")
	}
	if snapshot != nil {
		s := *snapshot
		if !s.Enabled || !s.Published || !s.Received || r.Identity != s.Identity || r.Nonce != s.Nonce || r.Helper != s.Helper || r.Database != s.Database || r.Contract != s.Contract || r.Boot != s.Boot || r.Backend != s.Backend || r.Predecessor != s.Predecessor || r.PredecessorAbsent != s.PredecessorAbsent {
			return errors.New("task launch authority refers to an older or different loader registration")
		}
	}
	return nil
}
