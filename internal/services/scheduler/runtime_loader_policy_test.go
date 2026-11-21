package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRuntimeLoaderPolicyContinuityRequiresExactLiveProvider(t *testing.T) {
	s, _, _ := loaderStoreFixture()
	saved := s.Epochs[s.Current].Provider
	fresh := saved
	fresh.PolicyHash = strings.Repeat("c", 64)
	fresh.RuntimeHash = strings.Repeat("d", 64)
	if err := validateRuntimeLoaderContinuity(saved, fresh); err != nil {
		t.Fatal("same qualified engine refused", err)
	}
	for _, field := range []string{"pid", "start", "boot", "group", "inode", "backend", "configuration", "profile", "policy"} {
		t.Run(field, func(t *testing.T) {
			candidate := fresh
			switch field {
			case "pid":
				candidate.Identity.PID++
			case "start":
				candidate.Identity.Start += "0"
			case "boot":
				candidate.Identity.Boot = "00000000-0000-4000-8000-000000000001"
			case "group":
				candidate.Identity.Group += "-other"
			case "inode":
				candidate.Identity.GroupInode++
			case "backend":
				candidate.Identity.Backend = jammyCronCandidateID
			case "configuration":
				candidate.Identity.Configuration = strings.Repeat("e", 64)
			case "profile":
				candidate.Profile = "other"
			case "policy":
				candidate.PolicyHash = "invalid"
			}
			if validateRuntimeLoaderContinuity(saved, candidate) == nil {
				t.Fatal("unreviewed engine change admitted")
			}
		})
	}
}

func TestRuntimeDistributionPolicyContinuityStillBindsSameProcess(t *testing.T) {
	s, _, _ := loaderStoreFixture()
	for _, backend := range []string{jammyCronCandidateID, resoluteCronCandidateID} {
		saved := s.Epochs[s.Current].Provider
		saved.Identity.Backend = backend
		saved.Identity.Group = "/system.slice/cron.service"
		saved.Identity.Configuration = strings.Repeat("b", 64)
		saved.Profile = backend + "/loader-policy-v1"
		current := saved
		current.Identity.Configuration = strings.Repeat("d", 64)
		current.PolicyHash = strings.Repeat("e", 64)
		if err := validateRuntimeLoaderContinuity(saved, current); err != nil {
			t.Fatal(backend, err)
		}
		current.Identity.PID++
		if validateRuntimeLoaderContinuity(saved, current) == nil {
			t.Fatal("process replacement admitted automatically")
		}
	}
}

func TestBackendReconcilerRenewsOnlyTypedPolicyMismatch(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		first, renewErr      error
		wantCalls, wantRenew int
	}{
		{"unchanged", nil, nil, 1, 0},
		{"unsupported", errors.New("unreviewed PAM input"), nil, 1, 0},
		{"policy", ErrLoaderPolicyChanged, nil, 2, 1},
		{"pending", ErrLoaderPolicyChanged, errors.New("maintenance pending"), 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, renews := 0, 0
			result := reconcileBackendWithPolicyRenewal(context.Background(), func(context.Context) error {
				calls++
				if calls == 1 {
					return tc.first
				}
				return nil
			}, func(context.Context) error { renews++; return tc.renewErr })
			if calls != tc.wantCalls || renews != tc.wantRenew {
				t.Fatal(calls, renews)
			}
			if tc.renewErr != nil && !errors.Is(result, tc.renewErr) {
				t.Fatal("failed renewal hidden", result)
			}
			if tc.name == "unsupported" && !errors.Is(result, tc.first) {
				t.Fatal("unsupported inspection swallowed")
			}
			if tc.name == "policy" && result != nil {
				t.Fatal(result)
			}
		})
	}
}

func TestRuntimeLoaderRetryRequiresSingleTypedObservation(t *testing.T) {
	busy := diagnostic("waiting_for_legacy_jobs", "busy")
	churn := &pamDirectorySnapshotChanged{"/run/systemd/transient", "during inspection"}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"draining", busy, true}, {"protected-directory-churn", churn, true}, {"wrapped", fmt.Errorf("inspect: %w", churn), true},
		{"nil", nil, false}, {"typed-nil", (*pamDirectorySnapshotChanged)(nil), false},
		{"same-text", errors.New(churn.Error()), false}, {"authority-changed", errors.New("candidate path authority changed"), false},
		{"mixed", errors.Join(churn, errors.New("unsafe inode")), false}, {"two-busy", errors.Join(churn, busy), false},
		{"wrapped-mixed", fmt.Errorf("admission: %w", errors.Join(churn, errors.New("unsafe"))), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if RuntimeLoaderObservationPending(tc.err) != tc.want {
				t.Fatal(tc.err)
			}
		})
	}
}
