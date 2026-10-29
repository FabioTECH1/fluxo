package maintenancetask

import (
	"context"
	"errors"
	"fmt"

	"fluxo/internal/services/packages"
)

// ReconcileInvocations establishes quiescence, never successful job execution.
// It is an explicit root recovery action. A consumed authority is not replayed;
// original input evidence and its unknown outcome remain in the receipt.
func ReconcileInvocations(ctx context.Context) error {
	a, err := absent()
	if err != nil || a {
		return err
	}
	release, err := packages.Lifecycle(ctx, false)
	if err != nil {
		return err
	}
	defer release()
	pkgRelease, err := packages.Acquire(ctx)
	if err != nil {
		return err
	}
	defer pkgRelease()
	native, err := packages.NativeDPKG(ctx)
	if err != nil {
		return err
	}
	defer native()
	unlock, err := gate(ctx, true)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	if err = retireAbsentSubmissions(ctx, &j); err != nil {
		return err
	}
	boot, err := bootID()
	if err != nil {
		return err
	}
	for name, v := range j.Invocations {
		if v.Status != "started" && v.Status != "failed" {
			continue
		}
		p, err := inspectInvocation(ctx, v)
		if err != nil {
			return err
		}
		n, err := domainCount(name)
		if err != nil {
			return err
		}
		if n != 0 || p["LoadState"] != "not-found" && (p["MainPID"] != "0" || p["ControlPID"] != "0" || p["ActiveState"] != "inactive" && p["ActiveState"] != "failed") {
			return errors.New("The interrupted job still has processes; allow them to finish naturally before reconciliation")
		}
		current := v.Profile
		if v.Registration.Contract.Kind != ApplicationJob {
			current, err = qualifyContractProfile(ctx, v.Registration.Contract)
		}
		if err != nil {
			return fmt.Errorf("Interrupted job inputs need operator repair: %w", err)
		}
		if !compatibleProfiles(v.Profile, current, false, boot != v.Boot) {
			return errors.New("Interrupted job changed protected code/configuration; retain its evidence and repair the changed inputs before reconciliation")
		}
		v.ReconciledFrom, v.ReconciledBoot = v.Status, boot
		v.Status = "interrupted"
		v.ResultProfile = &current
		v.Error = "Execution result unknown; the execution domain and supported effects were verified quiescent. This invocation was not replayed."
		j.Invocations[name] = v
		if err = save(j); err != nil {
			return err
		}
	}
	if err = compact(ctx, &j); err != nil {
		return err
	}
	return reconcileAbsentRegistrations(ctx, &j)
}
