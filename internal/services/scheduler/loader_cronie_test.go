package scheduler

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestCronieLoaderProfileDoesNotChangeBackendMeaning(t *testing.T) {
	c, err := distributionCandidate(cronieLoaderPAMProfileID)
	if err != nil || c.executable != "/usr/sbin/crond" || c.packageVersion != "1.7.1-1" || c.release != "24.04" {
		t.Fatal("Cronie profile does not select its actual provider", err)
	}
	for _, path := range []string{"/etc/pam.d/cron", "/etc/default/cron", "/usr/lib/systemd/system/cron.service"} {
		if _, present := c.files[path]; present {
			t.Fatal("Cronie inherited a different provider input", path)
		}
	}
	for _, path := range []string{"/etc/pam.d/cronie", "/etc/default/cronie", "/usr/lib/systemd/system/cronie.service", "/etc/security/access.conf"} {
		if !loaderDigestValid(c.files[path]) {
			t.Fatal("Cronie input has no exact content pin", path)
		}
	}
	if !slices.Contains(c.pamModules, "pam_access.so") || len(c.directories["/etc/security/access.d"]) != 0 {
		t.Fatal("Cronie account module/fragment closure is incomplete")
	}
	if newDistributionBackend(cronieLoaderPAMProfileID) || slices.Contains(BackendReaderContracts(), cronieLoaderPAMProfileID) {
		t.Fatal("a loader-only profile acquired persisted backend semantics")
	}
	noble, err := distributionCandidate(nobleLoaderPAMProfileID)
	if err != nil || noble.executable != "/usr/sbin/cron" || slices.Contains(noble.pamModules, "pam_access.so") {
		t.Fatal("separate Cronie closure widened the existing Noble profile", err)
	}
	if _, present := noble.files["/etc/security/access.conf"]; present {
		t.Fatal("existing Noble policy bytes would be reinterpreted")
	}
}

func TestCronieClosureKeepsIdentityAndRefusesRaces(t *testing.T) {
	ctx := context.Background()
	original := proofFixture().Branches["8.4"].Host
	original.Backend, original.Group, original.Configuration = "cronie-noble-1", "/system.slice/cronie.service", strings.Repeat("a", 64)
	for _, scenario := range []string{"stable", "closure-refused", "daemon-changed", "historical-schema"} {
		t.Run(scenario, func(t *testing.T) {
			calls, inspected := 0, false
			basic := func(context.Context) (legacyHost, error) {
				calls++
				h := original
				if scenario == "daemon-changed" && calls == 2 {
					h.PID++
				}
				return h, nil
			}
			failure := errors.New("unreviewed Cronie input")
			inspect := func(id string, pid int) (pamCandidateReport, error) {
				inspected = true
				if id != cronieLoaderPAMProfileID || pid != original.PID {
					t.Fatal("closure selected another provider")
				}
				if scenario == "closure-refused" {
					return pamCandidateReport{}, failure
				}
				return pamCandidateReport{}, nil
			}
			schema := loaderEpochSchema
			if scenario == "historical-schema" {
				schema--
			}
			got, err := qualifyWithLoaderClosure(ctx, schema, cronieLoaderPAMProfileID, basic, inspect)
			if scenario == "closure-refused" || scenario == "daemon-changed" {
				if err == nil || got != (legacyHost{}) {
					t.Fatal("refused or racing provider retained qualification", got, err)
				}
			} else if err != nil || got != original {
				t.Fatal("full closure changed historical identity meaning", got, err)
			}
			if inspected != (schema >= loaderEpochSchema) {
				t.Fatal("closure was skipped or applied retroactively")
			}
		})
	}
}

func TestSavedCronieCannotUseTodaysCronSelector(t *testing.T) {
	previous := backendObserver
	t.Cleanup(func() { backendObserver = previous })
	backendObserver = func(context.Context) BackendObservation {
		return BackendObservation{Engine: "cron", State: "active", Version: "3.0pl1-184ubuntu2", Architecture: "amd64"}
	}
	h := proofFixture().Branches["8.4"].Host
	h.Backend, h.Group, h.Configuration = "cronie-noble-1", "/system.slice/cronie.service", strings.Repeat("a", 64)
	if err := ValidateBackendIdentity(h); err != nil {
		t.Fatal(err)
	}
	if _, err := RequalifyCronBackend(context.Background(), h); err == nil {
		t.Fatal("saved Cronie obligation was reinterpreted through another provider")
	}
}

func TestCronieLoaderReleaseRejectsUnsupportedOrAmbiguousMetadata(t *testing.T) {
	good := "NAME=Ubuntu\nID=ubuntu\nVERSION_ID=\"24.04\"\n"
	if err := validateCronieLoaderRelease([]byte(good)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"", strings.ReplaceAll(good, "24.04", "26.04"), strings.ReplaceAll(good, "ID=ubuntu", "ID=debian"), good + "ID=ubuntu\n", good + "VERSION_ID=\"24.04\"\n", strings.ReplaceAll(good, "ID=ubuntu", "ID=ubuntu\x00")} {
		if validateCronieLoaderRelease([]byte(raw)) == nil {
			t.Fatal("unsupported or ambiguous release metadata accepted")
		}
	}
}
