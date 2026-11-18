package maintenancetask

import (
	"context"
	"errors"

	"fluxo/internal/services/scheduler"
)

// Installer admission must not require an obsolete composite package digest
// to be live. It qualifies current providers for existing registrations using
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
		defer unlock()
		return installerHistoricalTasksQuiescent(ctx, j)
	}
	defer unlock()
	if err = registrationRequalificationDomainsEmpty(ctx, j); err != nil {
		return err
	}
	original, err := readRegular(stateRoot+"/state.json", 8<<20)
	if err != nil {
		return err
	}
	review, err := inspectRegistrationBackendsForMode(ctx, j, original, true)
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
	original, err := readRegular(stateRoot+"/state.json", 8<<20)
	if err != nil {
		return nil, err
	}
	review, err := inspectRegistrationBackendsForMode(ctx, j, original, true)
	if err != nil {
		return nil, err
	}
	if len(review.AffectedIDs) == 0 {
		return nil, nil
	}
	for id, current := range review.Current {
		saved, ok := review.Saved[id]
		if !ok || scheduler.ValidateInstallerBackendRenewal(saved, current) != nil {
			return nil, errors.New("installer cannot switch a saved maintenance provider contract")
		}
	}
	return &review, nil
}

// Historical installer admission never rewrites the old core. Replacement is
// allowed only later, within the archived and reader-fenced format conversion.
func installerHistoricalTasksQuiescent(ctx context.Context, j journal) error {
	if err := historicalInstallerInvocationsSettled(j); err != nil {
		return err
	}
	if j.Schema != 1 && j.Schema != 2 {
		return errors.New("unsupported historical task core")
	}
	if err := registrationRequalificationDomainsEmpty(ctx, j); err != nil {
		return err
	}
	for _, r := range j.Entries {
		if !r.Enabled {
			continue
		}
		if err := verifyRegistration(ctx, r); err != nil {
			return err
		}
		now, err := scheduler.RequalifyCronBackend(ctx, r.Backend)
		if err != nil {
			return err
		}
		if now != r.Backend && now.Boot == r.Backend.Boot {
			if !r.Received || r.Boot != r.Backend.Boot {
				return errors.New("historical retirement requires the original acknowledged registration")
			}
			if err = scheduler.VerifyInstallerHistoricalRetirement(ctx, scheduler.CronBackendRetirement{Saved: r.Backend, Current: now}); err != nil {
				return err
			}
		} else if err = readyBackend(ctx, r); err != nil {
			return err
		}
	}
	return nil
}
