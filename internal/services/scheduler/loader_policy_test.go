package scheduler

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestLoaderPolicyReviewRequiresExactEpochAndBothPolicies(t *testing.T) {
	epoch, oldPolicy, newPolicy := uuid.NewString(), strings.Repeat("a", 64), strings.Repeat("b", 64)
	base := LoaderPolicyReview{Epoch: epoch, Saved: LoaderProvider{Profile: "ubuntu-resolute-cron-v1/loader-policy-v1", PolicyHash: oldPolicy}, Candidate: LoaderProvider{Profile: "ubuntu-resolute-cron-v1/loader-policy-v1", PolicyHash: newPolicy}}
	base.Saved.Identity.Backend, base.Candidate.Identity.Backend = resoluteCronCandidateID, resoluteCronCandidateID
	if err := validateLoaderPolicyReview(base, epoch, oldPolicy, newPolicy); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ epoch, old, new string }{
		{uuid.NewString(), oldPolicy, newPolicy}, {epoch, newPolicy, oldPolicy}, {epoch, oldPolicy, oldPolicy}, {epoch, "bad", newPolicy}, {epoch, oldPolicy, "bad"},
	} {
		if validateLoaderPolicyReview(base, tc.epoch, tc.old, tc.new) == nil {
			t.Fatal("stale or invalid operator expectation accepted", tc)
		}
	}
	for _, field := range []string{"pending", "backend", "profile"} {
		report := base
		switch field {
		case "pending":
			report.PendingPublication = uuid.NewString()
		case "backend":
			report.Candidate.Identity.Backend = jammyCronCandidateID
		case "profile":
			report.Candidate.Profile = "other-profile"
		}
		if validateLoaderPolicyReview(report, epoch, oldPolicy, newPolicy) == nil {
			t.Fatal("unreviewed transition accepted", field)
		}
	}
}

func TestLoaderHistoryRetainsDifferentReviewedPolicyWithoutRewritingPredecessor(t *testing.T) {
	s, _, _ := loaderStoreFixture()
	s = loaderFixturePublished(s)
	old := s.Epochs[s.Current]
	oldRaw, _ := json.Marshal(old)
	var previous LoaderPublicationPlan
	for _, p := range s.Publications {
		previous = p
	}
	current := old
	current.ID, current.Predecessor = uuid.NewString(), old.ID
	current.Provider.PolicyHash = strings.Repeat("c", 64)
	current.Targets = map[int]LoaderTarget{}
	plan := previous
	plan.ID, plan.EpochID, plan.Phase = uuid.NewString(), current.ID, "intent"
	plan.OriginalCore, plan.IntendedCore = previous.IntendedCore, previous.IntendedCore
	plan.Triggers = map[int]loaderTriggerPublication{}
	plan.Epoch = current
	plan.Epoch.Phase = "prepared"
	// Use an empty-entry core: this isolates the policy transition, without
	// asserting that enabled launchers can avoid nonce/receipt replacement.
	var core journal
	_ = json.Unmarshal(previous.IntendedCore, &core)
	core.Entries = map[int]Entry{}
	plan.OriginalCore, _ = json.Marshal(core)
	plan.IntendedCore = plan.OriginalCore
	s.Epochs[current.ID], s.Publications[plan.ID], s.Current, s.Pending = plan.Epoch, plan, current.ID, plan.ID
	raw, _ := json.Marshal(s)
	decoded, err := decodeLoaderStore(raw)
	if err != nil {
		t.Fatal("predecessor-linked reviewed policy transition refused", err)
	}
	retained, _ := json.Marshal(decoded.Epochs[old.ID])
	if !reflect.DeepEqual(oldRaw, retained) {
		t.Fatal("historical provider evidence changed")
	}
}
