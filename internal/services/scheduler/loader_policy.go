package scheduler

import (
	"context"
	"errors"
)

// Aggregate historical digests cannot explain a field-level historical diff.
// Inspection supplies a fresh native qualification, not permission to publish.
type LoaderPolicyReview struct {
	Epoch              string         `json:"epoch"`
	Saved              LoaderProvider `json:"saved"`
	Candidate          LoaderProvider `json:"candidate"`
	PendingPublication string         `json:"pending_publication,omitempty"`
}

type LoaderPolicyResult struct {
	Review                LoaderPolicyReview `json:"review"`
	Publication           string             `json:"publication,omitempty"`
	NewEpoch              string             `json:"new_epoch,omitempty"`
	Published             bool               `json:"published"`
	RequiresFreshReceipts bool               `json:"requires_fresh_receipts"`
}

// Caller holds the existing Scheduler gate; this function never initializes,
// repairs, acknowledges or rewrites a registration.
func InspectLoaderPolicyLocked(ctx context.Context) (LoaderPolicyReview, error) {
	var report LoaderPolicyReview
	s, err := readLoaderStore()
	if err != nil {
		return report, err
	}
	epoch, exists := s.Epochs[s.Current]
	if !exists {
		return report, errors.New("current loader policy history is missing")
	}
	report.Epoch, report.Saved, report.PendingPublication = s.Current, epoch.Provider, s.Pending
	if s.Pending != "" {
		return report, errors.New("recover the exact pending loader publication before policy review")
	}
	j, err := read()
	if err != nil {
		return report, err
	}
	if j.Schema != loaderEpochSchema || !j.LoaderEpochEstablished {
		return report, errors.New("an established schema5 loader epoch is required")
	}
	report.Candidate, err = inspectLoaderProvider(ctx, &report.Saved)
	return report, err
}

// Exact pending intent retains identical invocation snapshots on both sides.
// Ordinary Quiescent intentionally refuses pending publication; recovery must
// inspect those retained domains without granting ordinary read/write admission.
func LoaderPolicyRecoveryQuiescent(ctx context.Context, publication string) error {
	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	s, err := readLoaderStore()
	if err != nil {
		return err
	}
	if !loaderUUID(publication) || s.Pending != publication {
		return errors.New("pending loader publication does not match the requested UUID")
	}
	j, err := readJournal(false)
	if err != nil {
		return err
	}
	for _, invocation := range j.Invocations {
		if err = invocationQuiescent(ctx, invocation); err != nil {
			return err
		}
	}
	return nil
}

func validateLoaderPolicyReview(report LoaderPolicyReview, epoch, oldPolicy, newPolicy string) error {
	if !loaderUUID(epoch) || !loaderDigestValid(oldPolicy) || !loaderDigestValid(newPolicy) || oldPolicy == newPolicy || report.PendingPublication != "" || report.Epoch != epoch || report.Saved.PolicyHash != oldPolicy || report.Candidate.PolicyHash != newPolicy {
		return errors.New("loader policy review expectations changed; inspect and review again")
	}
	if report.Saved.Profile == "" || report.Saved.Profile != report.Candidate.Profile || report.Saved.Identity.Backend != report.Candidate.Identity.Backend {
		return errors.New("loader policy requalification cannot select a different backend or profile")
	}
	return nil
}

// The bridge owns stopped-panel, lifecycle/package/native and database
// admission plus both existing coordination gates. Ordinary reconciliation
// continues to refuse policy changes. Old snapshots remain predecessor history.
func RequalifyLoaderPolicyLocked(ctx context.Context, epoch, oldPolicy, newPolicy string, beforeCommit func() error) (LoaderPolicyResult, error) {
	var result LoaderPolicyResult
	report, err := InspectLoaderPolicyLocked(ctx)
	result.Review = report
	if err != nil {
		return result, err
	}
	if err = validateLoaderPolicyReview(report, epoch, oldPolicy, newPolicy); err != nil {
		return result, err
	}
	if err = VerifyCronBackendEmpty(ctx, report.Candidate.Identity); err != nil {
		return result, err
	}
	j, err := read()
	if err != nil {
		return result, err
	}
	for _, invocation := range j.Invocations {
		if err = invocationQuiescent(ctx, invocation); err != nil {
			return result, err
		}
	}
	plan, err := planLoaderPublicationLocked(ctx, j, nil, &report)
	if err != nil {
		return result, err
	}
	if beforeCommit == nil {
		return result, errors.New("loader policy publication requires stopped-panel revalidation")
	}
	if err = beforeCommit(); err != nil {
		return result, err
	}
	current, err := inspectLoaderProvider(ctx, &report.Saved)
	if err != nil || !sameLoaderProvider(current, report.Candidate) {
		return result, errors.New("loader provider changed before reviewed publication")
	}
	result.Publication, result.NewEpoch = plan.ID, plan.EpochID
	result.RequiresFreshReceipts = len(plan.Triggers) != 0
	if err = ApplyLoaderPublicationLocked(ctx, &j, plan); err != nil {
		return result, err
	}
	result.Published = true
	return result, nil
}

func RecoverLoaderPolicyPublicationLocked(ctx context.Context, publication string) error {
	if !loaderUUID(publication) {
		return errors.New("an exact loader publication UUID is required")
	}
	s, err := readLoaderStore()
	if err != nil {
		return err
	}
	if s.Pending != publication {
		return errors.New("pending loader publication does not match the requested UUID")
	}
	return RecoverLoaderPublicationsLocked(ctx)
}
