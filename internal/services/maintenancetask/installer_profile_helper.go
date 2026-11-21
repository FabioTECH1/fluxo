package maintenancetask

import (
	"context"
	"errors"
	"path/filepath"
	"sort"

	"fluxo/internal/services/executiondomain"
)

type HelperRefreshReview struct {
	TaskCoreSHA256 string `json:"task_core_sha256"`
}

// InstallerHelperRefreshReviewLocked is read-only. The installer owns
// lifecycle/coordination gates and must use the returned exact source in its
// archived refresh transition. This does not approve unsupported configuration,
// initialize missing state, issue nonces or establish execution readiness.
func InstallerHelperRefreshReviewLocked(ctx context.Context) (*HelperRefreshReview, error) {
	a, err := absent()
	if err != nil || a {
		return nil, err
	}
	before, err := readRegular(filepath.Join(stateRoot, "state.json"), 8<<20)
	if err != nil {
		return nil, err
	}
	j, err := read()
	if err != nil {
		return nil, err
	}
	if j.Schema != executiondomain.CurrentSystemTaskSchema {
		return nil, nil
	}
	ids, err := inspectOptionalProfileHelpers(ctx, j, func(ctx context.Context, r registration) (executiondomain.ReaderCapabilities, error) {
		if !r.Published || !r.Received {
			return executiondomain.ReaderCapabilities{}, errors.New("Wait for genuine task publication acknowledgement before optional helper refresh")
		}
		if err := verifyRegistration(ctx, r); err != nil {
			return executiondomain.ReaderCapabilities{}, err
		}
		if err := verifyRetainedHelperLineage(r); err != nil {
			return executiondomain.ReaderCapabilities{}, err
		}
		if err := verifyHelperReaderContracts(ctx, r.Helper); err != nil {
			return executiondomain.ReaderCapabilities{}, err
		}
		return executiondomain.ReadHelperCapabilities(ctx, r.Helper, filepath.Base(r.Helper))
	}, qualifyContractProfile)
	if err != nil {
		return nil, err
	}
	after, err := readRegular(filepath.Join(stateRoot, "state.json"), 8<<20)
	if err != nil {
		return nil, err
	}
	if digest(before) != digest(after) {
		return nil, errors.New("Task registration state changed during installer profile review")
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return &HelperRefreshReview{TaskCoreSHA256: digest(before)}, nil
}

func inspectOptionalProfileHelpers(ctx context.Context, j journal, inspect func(context.Context, registration) (executiondomain.ReaderCapabilities, error), qualify func(context.Context, Contract) (profile, error)) ([]int, error) {
	ids := []int{}
	for id, r := range j.Entries {
		if !r.Enabled || r.Contract.Kind != ComposerUpdate && r.Contract.Kind != CertificateRenewal {
			continue
		}
		have, err := inspect(ctx, r)
		if err != nil {
			return nil, err
		}
		features := []string{imagickStartupProfile}
		if r.Contract.Kind == CertificateRenewal {
			features = []string{certbotDirectoryHookContract, certbotDirectoryHookExtendedContract, certbotManualNoHooksContract}
		}
		allOptional := executiondomain.ReaderCapabilities{Format: 1, RoleContracts: map[string][]string{optionalProfileRole: features}}
		if executiondomain.RequireCapabilities(have, allOptional) == nil {
			continue // Already capable: no host-profile reinspection or refresh.
		}
		// Configuration qualification can refuse locally authored or unsupported
		// inputs. Preserve those for explicit review; an installer is not consent
		// to repair them or a whole-host certification boundary.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p, err := qualify(ctx, r.Contract)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		need := profileHelperRequirements(p)
		if len(need.RoleContracts[optionalProfileRole]) == 0 {
			continue
		}
		if executiondomain.RequireCapabilities(have, need) != nil {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids, nil
}
