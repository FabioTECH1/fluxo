package scheduler

import (
	"context"
	"errors"
	"strings"

	"fluxo/internal/services/executiondomain"
)

// A loader policy is separate from the immutable cronie-noble-1 retirement
// identity. Do not add this label to backend contracts or reader requirements.
const cronieLoaderPAMProfileID = "ubuntu-noble-cronie-loader-policy-v1"

func validateCronieLoaderRelease(raw []byte) error {
	want := map[string]string{"ID": "ID=ubuntu", "VERSION_ID": "VERSION_ID=\"24.04\""}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		key, _, _ := strings.Cut(line, "=")
		if expected, relevant := want[key]; relevant {
			if seen[key] || line != expected {
				return errors.New("Cronie loader profile requires the reviewed Ubuntu 24.04 release")
			}
			seen[key] = true
		}
	}
	if len(seen) != len(want) {
		return errors.New("Cronie loader profile requires the reviewed Ubuntu 24.04 release")
	}
	return nil
}

func qualifyCurrentCronieHost(ctx context.Context) (legacyHost, error) {
	basic := func(ctx context.Context) (legacyHost, error) {
		observed := backendObserver(ctx)
		if observed.Engine != "cronie" || observed.State != "active" {
			return legacyHost{}, diagnostic("backend_identity_changed", "The saved Cronie provider is not the sole active scheduling engine. Existing schedules and history are preserved.")
		}
		return qualifyCronieHost(ctx, observed)
	}
	return qualifyWithLoaderClosure(ctx, executiondomain.CurrentSchedulerSchema, cronieLoaderPAMProfileID, basic, inspectPAMCandidate)
}
