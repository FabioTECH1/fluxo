package maintenancetask

import (
	"context"
	"errors"

	"fluxo/internal/services/scheduler"
)

// Installer admission must not require an obsolete composite package digest
// to be live. It qualifies the existing same-generation registrations using
// their saved verifier before allowing provisioning; publication remains a
// separate archived, prepared-floor transition after the image is installed.
// Runtime Quiescent and launch/receipt equality keep their original semantics.
func InstallerQuiescent(ctx context.Context) error {
	a, err := absent()
	if err != nil || a {
		return err
	}
	unlock, err := gate(ctx, false)
	if err != nil {
		return err
	}
	j, err := read()
	if err != nil {
		unlock()
		return err
	}
	if j.Schema != 3 {
		unlock()
		return Quiescent(ctx)
	}
	defer unlock()
	if err = registrationRequalificationDomainsEmpty(ctx, j); err != nil {
		return err
	}
	original, err := readRegular(stateRoot+"/state.json", 8<<20)
	if err != nil {
		return err
	}
	review, err := inspectRegistrationBackends(ctx, j, original)
	if err != nil {
		return err
	}
	seen := map[scheduler.CronBackendIdentity]bool{}
	for _, current := range review.Current {
		if seen[current] {
			continue
		}
		seen[current] = true
		if err = scheduler.VerifyCronBackendEmpty(ctx, current); err != nil {
			return err
		}
	}
	return nil
}

// Existing empty subsystems and historical formats stay with normal format
// activation. This never creates registration evidence or guesses an old row.
func InstallerRegistrationReviewLocked(ctx context.Context) (*RegistrationBackendReview, error) {
	a, err := absent()
	if err != nil || a {
		return nil, err
	}
	j, err := read()
	if err != nil {
		return nil, err
	}
	if j.Schema != 3 {
		return nil, nil
	}
	review, err := InspectRegistrationBackendsLocked(ctx)
	if err != nil {
		return nil, err
	}
	if len(review.AffectedIDs) == 0 {
		return nil, nil
	}
	for id, current := range review.Current {
		saved, ok := review.Saved[id]
		if !ok || !sameBackendGeneration(saved, current) {
			return nil, errors.New("installer cannot switch a saved maintenance provider generation")
		}
	}
	return &review, nil
}
