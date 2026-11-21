package scheduler

import (
	"context"
	"errors"
)

// A current running panel may renew an independently qualified closure for the
// SAME live engine. Historical configuration semantics and the strict ordinary
// comparison remain unchanged. An engine replacement/reboot belongs to the
// existing installer path, including its retained execution obligations.
func validateRuntimeLoaderContinuity(saved, current LoaderProvider) error {
	if err := validateInstallerLoaderContinuity(saved, current); err != nil {
		return err
	}
	candidate := current.Identity
	// Distribution-v1's configuration includes the qualified PAM/package closure.
	// Its immutable verifier still independently qualifies the entire closure.
	if newDistributionBackend(candidate.Backend) {
		candidate.Configuration = saved.Identity.Configuration
	}
	if candidate != saved.Identity {
		return errors.New("automatic Scheduler policy renewal requires the same live engine; use the normal installer for provider replacement")
	}
	return nil
}

// RuntimeLoaderObservationPending identifies only a single retryable native
// observation: a still-populated qualified provider, or membership churn on the
// same protected directory inode. A caller must discard the entire observation,
// release its gates and repeat full qualification; it never accepts a changed
// input. File/authority/identity changes and joined failures remain refusals.
func RuntimeLoaderObservationPending(err error) bool {
	if InstallerCronDrainPending(err) {
		return true
	}
	for err != nil {
		if changed, ok := err.(*pamDirectorySnapshotChanged); ok {
			return changed != nil
		}
		err = errors.Unwrap(err)
	}
	return false
}

// Caller owns lifecycle/package/native-dpkg, coordination and database write
// exclusion. No historical core is initialized or converted by this path.
func RuntimeLoaderPolicyNeededLocked(ctx context.Context) (bool, error) {
	if absent() {
		return false, nil
	}
	j, err := read()
	if err != nil || j.Schema != loaderEpochSchema || !j.LoaderEpochEstablished {
		return false, err
	}
	if err = loaderPublicationMayStart(j); err != nil {
		return false, err
	}
	review, err := InspectLoaderPolicyLocked(ctx)
	if err != nil {
		return false, err
	}
	if review.Saved.PolicyHash == review.Candidate.PolicyHash {
		return false, nil // Ordinary generation renewal keeps its existing path.
	}
	if err = validateRuntimeLoaderContinuity(review.Saved, review.Candidate); err != nil {
		return false, err
	}
	return review.Saved.PolicyHash != review.Candidate.PolicyHash, nil
}

// Read-only fast path used before taking global maintenance exclusivity.
func RuntimeLoaderPolicyNeeded(ctx context.Context) (bool, error) {
	if absent() {
		return false, nil
	}
	unlock, err := lock(ctx)
	if err != nil {
		return false, err
	}
	defer unlock()
	return RuntimeLoaderPolicyNeededLocked(ctx)
}

func PlanRuntimeLoaderPolicyLocked(ctx context.Context) (*LoaderPublicationPlan, error) {
	needed, err := RuntimeLoaderPolicyNeededLocked(ctx)
	if err != nil || !needed {
		return nil, err
	}
	review, err := InspectLoaderPolicyLocked(ctx)
	if err != nil {
		return nil, err
	}
	if err = validateRuntimeLoaderContinuity(review.Saved, review.Candidate); err != nil {
		return nil, err
	}
	j, err := read()
	if err != nil {
		return nil, err
	}
	for _, v := range j.Invocations {
		if err = invocationQuiescent(ctx, v); err != nil {
			return nil, err
		}
	}
	if err = VerifyCronBackendEmpty(ctx, review.Candidate.Identity); err != nil {
		return nil, err
	}
	plan, err := planLoaderPublicationLocked(ctx, j, nil, &review)
	if err != nil || plan.ID == "" {
		return nil, err
	}
	return &plan, nil
}

func ApplyRuntimeLoaderPolicyLocked(ctx context.Context, plan LoaderPublicationPlan) error {
	current, err := inspectLoaderProvider(ctx, &plan.Epoch.Provider)
	if err != nil {
		return err
	}
	if err = validateRuntimeLoaderContinuity(plan.Epoch.Provider, current); err != nil {
		return err
	}
	if current.PolicyHash != plan.Epoch.Provider.PolicyHash {
		return errors.New("Scheduler inputs changed before automatic policy publication")
	}
	// This protocol persists the exact plan before core/trigger writes, preserves
	// predecessor history, and publishes every enabled target UNACKNOWLEDGED.
	return ApplyInstallerLoaderPolicyLocked(ctx, plan)
}
