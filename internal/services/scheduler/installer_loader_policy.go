package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"time"
)

// A normal installation authorizes enrollment in the independently qualified
// current closure of the SAME saved provider contract. It does not authorize
// selecting a different engine, pinned service/configuration or loader profile.
// Distribution-v1 Configuration includes the entire independently qualified
// PAM/package closure, so its composite digest can change after a supported
// package transaction; historical Noble/Cronie configuration keeps exact equality.
// Historical
// aggregate hashes cannot reconstruct a diff; preserve them as predecessor
// evidence rather than pretending to identify or normalize individual changes.
func validateInstallerLoaderContinuity(saved, current LoaderProvider) error {
	if ValidateInstallerBackendRenewal(saved.Identity, current.Identity) != nil ||
		!loaderDigestValid(saved.PolicyHash) || !loaderDigestValid(current.PolicyHash) ||
		!loaderDigestValid(saved.RuntimeHash) || !loaderDigestValid(current.RuntimeHash) ||
		saved.Profile == "" || saved.Profile != current.Profile {
		return errors.New("installer cannot change the saved Scheduler provider contract; existing jobs and policy history are preserved")
	}
	return nil
}

// This classifies an installer transition only; it is not live qualification or
// launch authority. Callers independently qualify the saved verifier's current
// provider, prove its complete hierarchy empty and retain every known task/
// Scheduler execution obligation. Replaced daemons cannot turn live descendants
// into an empty domain. Historical Noble/Cronie static pins stay exact.
func ValidateInstallerBackendRenewal(saved, current CronBackendIdentity) error {
	if ValidateBackendIdentity(saved) != nil || ValidateBackendIdentity(current) != nil ||
		saved.Backend != current.Backend || saved.Group != current.Group ||
		saved.Configuration != current.Configuration && !newDistributionBackend(saved.Backend) {
		return errors.New("installer cannot switch the saved scheduled-job provider contract")
	}
	return nil
}

// The bridge owns stopped-panel, lifecycle, package/native-dpkg and coordination
// admission. The exact returned plan must be archived before Apply is called.
func PlanInstallerLoaderPolicyLocked(ctx context.Context) (*LoaderPublicationPlan, error) {
	if absent() {
		return nil, nil
	}
	j, err := read()
	if err != nil {
		return nil, err
	}
	if j.Schema != loaderEpochSchema || !j.LoaderEpochEstablished {
		return nil, nil // Initial publication belongs to the existing reconciler.
	}
	review, err := InspectLoaderPolicyLocked(ctx)
	if err != nil {
		return nil, err
	}
	if err = validateInstallerLoaderContinuity(review.Saved, review.Candidate); err != nil {
		return nil, err
	}
	for _, v := range j.Invocations {
		if err = invocationQuiescent(ctx, v); err != nil {
			return nil, err
		}
	}
	plan, err := planLoaderPublicationLocked(ctx, j, nil, &review)
	if err != nil || plan.ID == "" {
		return nil, err
	}
	if err = VerifyCronBackendEmpty(ctx, review.Candidate.Identity); err != nil {
		return nil, err
	}
	return &plan, nil
}

func InstallerLoaderPolicyNeededLocked(ctx context.Context) (bool, error) {
	if absent() {
		return false, nil
	}
	j, err := read()
	if err != nil || j.Schema != loaderEpochSchema || !j.LoaderEpochEstablished {
		return false, err
	}
	review, err := InspectLoaderPolicyLocked(ctx)
	if err != nil {
		return false, err
	}
	if err = validateInstallerLoaderContinuity(review.Saved, review.Candidate); err != nil {
		return false, err
	}
	return !sameLoaderProvider(review.Saved, review.Candidate), nil
}

// Exclusive lifecycle admission is held by the installer. Use the same native
// observer as ordinary runtime reconciliation; never mark unstarted work as
// started, kill a populated domain, or remove uncertain submission authority.
func ReconcileInstallerDomains(ctx context.Context) error {
	if absent() {
		return nil
	}
	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	return reconcileInstallerCore(ctx, j, bootID, save)
}

func reconcileInstallerCore(ctx context.Context, j journal, currentBoot func() (string, error), persist func(journal) error) error {
	// Historical readers accept extension fields and can project schema1 to
	// schema2 in memory. An early typed save would silently convert/drop that
	// evidence even if the later database/panel admission refuses the upgrade.
	// Leave those cores to their existing runtime/explicit activation paths.
	if j.Schema != loaderEpochSchema {
		return nil
	}
	boot, err := currentBoot()
	if err != nil {
		return err
	}
	if err = checkUpgradeOwnership(j, boot, startKey); err != nil {
		return err
	}
	branches := map[string]bool{}
	for _, inv := range j.Invocations {
		branches[inv.Entry.Version] = true
	}
	for branch := range branches {
		if _, err = observe(ctx, &j, branch); err != nil {
			return err
		}
	}
	for _, inv := range j.Invocations {
		if err = invocationQuiescent(ctx, inv); err != nil {
			return err
		}
	}
	if len(branches) != 0 {
		return persist(j)
	}
	return nil
}

func InstallerLoaderRecoveryQuiescent(ctx context.Context, publication string) error {
	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	s, err := readLoaderStore()
	if err != nil {
		return err
	}
	if s.Pending != "" && s.Pending != publication {
		return errors.New("a different Scheduler publication needs its own recovery")
	}
	j, err := readJournal(false)
	if err != nil {
		return err
	}
	for _, inv := range j.Invocations {
		if err = invocationQuiescent(ctx, inv); err != nil {
			return err
		}
	}
	return nil
}

// Resume only the archived plan. A crash before ledger intent can apply that
// exact plan; a partial publication uses existing exact-side recovery. A sealed
// publication is verified without replaying nonces or genuine acknowledgements.
func ApplyInstallerLoaderPolicyLocked(ctx context.Context, plan LoaderPublicationPlan) error {
	if err := requireLoaderWriter(ctx); err != nil {
		return err
	}
	s, err := readLoaderStore()
	if err != nil {
		return err
	}
	if existing, found, e := installerLoaderPublication(s, plan); e != nil {
		return e
	} else if found {
		if existing.Phase == "committed" {
			return nil
		}
		if s.Pending != plan.ID {
			return errors.New("installer Scheduler publication lost its pending identity")
		}
	} else if s.Pending != "" || s.Current != plan.Epoch.Predecessor {
		return errors.New("installer Scheduler predecessor changed before publication")
	}
	now, err := inspectLoaderProvider(ctx, &plan.Epoch.Provider)
	if err != nil {
		return err
	}
	if err = validateInstallerLoaderContinuity(plan.Epoch.Provider, now); err != nil || now.PolicyHash != plan.Epoch.Provider.PolicyHash {
		return errors.New("Scheduler inputs changed during installer publication; retain the exact recovery evidence")
	}
	if err = VerifyCronBackendEmpty(ctx, now.Identity); err != nil {
		return err
	}
	if s.Pending == plan.ID {
		return applyPendingLoaderPublication(ctx, &s)
	}
	j, err := read()
	if err != nil {
		return err
	}
	return ApplyLoaderPublicationLocked(ctx, &j, plan)
}

func installerLoaderPublication(s loaderEpochStore, plan LoaderPublicationPlan) (LoaderPublicationPlan, bool, error) {
	p, found := s.Publications[plan.ID]
	if !found {
		if ref, archived := s.Archives[plan.EpochID]; archived {
			raw, _, err := readLoaderProtectedFile(loaderArchivePath(plan.EpochID), loaderFileBudget)
			if err != nil {
				return p, false, err
			}
			pair, err := decodeLoaderArchivePair(raw, plan.EpochID, ref, s.Archives)
			if err != nil {
				return p, false, err
			}
			p, found = pair.Publication, true
		}
	}
	if found {
		copy := p
		copy.Phase = plan.Phase
		if !reflect.DeepEqual(copy, plan) {
			return p, false, errors.New("installer Scheduler archive conflicts with retained publication")
		}
	}
	return p, found, nil
}

func VerifyInstallerLoaderPolicyLocked(plan LoaderPublicationPlan) error {
	s, err := readLoaderStore()
	if err != nil {
		return err
	}
	p, found, err := installerLoaderPublication(s, plan)
	if err != nil || !found || p.Phase != "committed" || s.Pending != "" {
		return errors.New("installer Scheduler publication has not committed its exact archived plan")
	}
	current := s.Epochs[s.Current].Provider
	if err = validateInstallerLoaderContinuity(plan.Epoch.Provider, current); err != nil || plan.Epoch.Provider.PolicyHash != current.PolicyHash {
		return errors.New("Scheduler policy changed after installer publication")
	}
	return nil
}

// No helper or receipt is manufactured here. The ordinary cron-origin receipt
// path and periodic generation renewal must make the current epoch ready.
func WaitInstallerLoaderAcknowledgements(ctx context.Context) error {
	for {
		ready, err := installerLoaderReady(ctx)
		if err != nil || ready {
			return err
		}
		select {
		case <-ctx.Done():
			return errors.New("waiting for genuine Scheduler acknowledgements; rerun this release's installer")
		case <-time.After(time.Second):
		}
	}
}

func installerLoaderReady(ctx context.Context) (bool, error) {
	if absent() {
		return true, nil
	}
	unlock, err := lock(ctx)
	if err != nil {
		return false, err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return false, err
	}
	if j.Schema != loaderEpochSchema || !j.LoaderEpochEstablished {
		return enabledLoaderEntries(j) == 0, nil
	}
	s, err := readLoaderStore()
	if err != nil {
		return false, err
	}
	epoch := s.Epochs[s.Current]
	current, err := qualifyLoaderProvider(ctx, &epoch.Provider)
	if err != nil {
		return false, err
	}
	if !sameLoaderProvider(current, epoch.Provider) {
		return false, nil
	}
	for _, e := range j.Entries {
		if !e.Enabled {
			continue
		}
		target, found := epoch.Targets[e.ID]
		if !found || target.Binding != e.Identity || target.Nonce != e.LoaderNonce || target.TriggerHash != hashBytes([]byte(CronContent(e))) {
			return false, errors.New("Scheduler acknowledgement target changed during installer setup")
		}
		raw, err := os.ReadFile(CronPath(e.ID))
		if err != nil || hashBytes(raw) != target.TriggerHash {
			return false, errors.New("Scheduler trigger changed during installer setup")
		}
		if !target.Received {
			return false, nil
		}
	}
	return true, RequireLoaderEpochReadyLocked(ctx, j)
}

// Decode a bridge-archived plan without granting any write authority. Apply
// still validates it against the protected ledger and exact live core/trigger
// bytes through the existing publication protocol.
func DecodeInstallerLoaderPlan(raw []byte) (LoaderPublicationPlan, error) {
	var plan LoaderPublicationPlan
	if len(raw) == 0 || len(raw) > loaderFileBudget {
		return plan, errors.New("invalid archived installer Scheduler plan size")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&plan) != nil || d.Decode(new(any)) != io.EOF ||
		!loaderUUID(plan.ID) || !loaderUUID(plan.EpochID) || plan.Epoch.ID != plan.EpochID ||
		!loaderUUID(plan.Epoch.Predecessor) || plan.Triggers == nil ||
		plan.Phase != "intent" || plan.Epoch.Phase != "prepared" || len(plan.OriginalCore) == 0 || len(plan.IntendedCore) == 0 {
		return plan, errors.New("invalid archived installer Scheduler plan")
	}
	return plan, nil
}
