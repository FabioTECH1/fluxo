package scheduler

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestInstallerLoaderContinuityPreservesProviderContract(t *testing.T) {
	s, _, _ := loaderStoreFixture()
	saved := s.Epochs[s.Current].Provider
	current := saved
	current.PolicyHash = strings.Repeat("c", 64)
	current.RuntimeHash = strings.Repeat("d", 64)
	current.Identity.Boot, current.Identity.PID, current.Identity.Start, current.Identity.GroupInode = uuid.NewString(), 1234, "56", 789
	if err := validateInstallerLoaderContinuity(saved, current); err != nil {
		t.Fatal("qualified policy/runtime renewal refused", err)
	}
	for _, field := range []string{"backend", "profile", "configuration", "group", "invalid-identity", "invalid-policy", "invalid-runtime"} {
		t.Run(field, func(t *testing.T) {
			changed := current
			switch field {
			case "backend":
				changed.Identity.Backend = jammyCronCandidateID
			case "profile":
				changed.Profile = "another-profile"
			case "configuration":
				changed.Identity.Configuration = strings.Repeat("e", 64)
			case "group":
				changed.Identity.Group = "/system.slice/another.service"
			case "invalid-identity":
				changed.Identity.PID = 0
			case "invalid-policy":
				changed.PolicyHash = "invalid"
			case "invalid-runtime":
				changed.RuntimeHash = "invalid"
			}
			if validateInstallerLoaderContinuity(saved, changed) == nil {
				t.Fatal("unsupported renewal accepted")
			}
		})
	}
}

func TestInstallerDistributionClosureCanRenewItsCompositeConfiguration(t *testing.T) {
	s, _, _ := loaderStoreFixture()
	for _, backend := range []string{jammyCronCandidateID, resoluteCronCandidateID} {
		saved := s.Epochs[s.Current].Provider
		saved.Identity.Backend = backend
		saved.Identity.Group = "/system.slice/cron.service"
		saved.Identity.Configuration = strings.Repeat("b", 64)
		saved.Profile = backend + "/loader-policy-v1"
		current := saved
		current.Identity.Configuration = strings.Repeat("e", 64)
		current.PolicyHash = strings.Repeat("f", 64)
		if err := validateInstallerLoaderContinuity(saved, current); err != nil {
			t.Fatal(backend, err)
		}
		current.Profile = "different-profile"
		if validateInstallerLoaderContinuity(saved, current) == nil {
			t.Fatal("composite renewal switched profile")
		}
	}
}

func TestInstallerLoaderRetryRetainsExactPublicationAndAcknowledgements(t *testing.T) {
	s, _, _ := loaderStoreFixture()
	plan := s.Publications[s.Pending]
	s = loaderFixturePublished(s)
	before, _ := json.Marshal(s)
	got, found, err := installerLoaderPublication(s, plan)
	if err != nil || !found || got.Phase != "committed" {
		t.Fatal(found, err)
	}
	after, _ := json.Marshal(s)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("verification rewrote genuine acknowledgements or history")
	}
	for _, field := range []string{"core", "trigger", "provider", "predecessor", "epoch", "id"} {
		t.Run(field, func(t *testing.T) {
			raw, _ := json.Marshal(plan)
			var changed LoaderPublicationPlan
			_ = json.Unmarshal(raw, &changed)
			switch field {
			case "core":
				changed.IntendedCore = []byte("different")
			case "trigger":
				for id, tr := range changed.Triggers {
					tr.Intended = []byte("different")
					changed.Triggers[id] = tr
				}
			case "provider":
				changed.Epoch.Provider.PolicyHash = strings.Repeat("f", 64)
			case "predecessor":
				changed.Epoch.Predecessor = uuid.NewString()
			case "epoch":
				changed.EpochID = uuid.NewString()
			case "id":
				changed.ID = uuid.NewString()
			}
			_, found, err := installerLoaderPublication(s, changed)
			if err == nil && found {
				t.Fatal("conflicting archive was accepted")
			}
		})
	}
}
