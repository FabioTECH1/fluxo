package server

import "testing"

func TestDeploymentStrategyCannotChangeAfterCreation(t *testing.T) {
	for _, test := range []struct {
		name      string
		current   string
		requested string
		wantError bool
	}{
		{name: "omitted", current: "standard", requested: ""},
		{name: "same standard", current: "standard", requested: "standard"},
		{name: "same zero downtime", current: "zero-downtime", requested: "zero-downtime"},
		{name: "standard to zero downtime", current: "standard", requested: "zero-downtime", wantError: true},
		{name: "zero downtime to standard", current: "zero-downtime", requested: "standard", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateDeploymentStrategyUnchanged(test.current, test.requested)
			if (err != nil) != test.wantError {
				t.Fatalf("validateDeploymentStrategyUnchanged(%q, %q) error = %v, wantError %v", test.current, test.requested, err, test.wantError)
			}
		})
	}
}

func TestRepositorySettingsDoNotMutateZeroDowntimeSiteRoot(t *testing.T) {
	if shouldSyncRepositoryInPlace("zero-downtime") {
		t.Fatal("zero-downtime repository settings would mutate the site root")
	}
	if !shouldSyncRepositoryInPlace("standard") {
		t.Fatal("standard repository settings would not synchronize the in-place checkout")
	}
}
