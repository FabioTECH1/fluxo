package maintenancetask

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"fluxo/internal/syscmd"
)

// Recovery observes an unknown outcome after actual domain drainage. It never
// replays the payload or changes the ordinary execution/completion comparator.
func qualifyRecoveryProfile(ctx context.Context, c Contract, before profile, newBoot bool) (profile, error) {
	if c.Kind == ApplicationJob {
		return before, nil
	}
	current, err := qualifyContractProfile(ctx, c)
	if err != nil && legacyV1Profile(before) && c.Kind == PackageCleanup && before.External["package:apt"] == "2.8.3" {
		current = newProfile(PackageCleanup)
		_, err = inspectCleanupPlatformVersion(ctx, &current, "2.8.3")
	}
	if err != nil {
		return current, fmt.Errorf("Interrupted job inputs need operator repair: %w", err)
	}
	if compatibleRecoveryObservations(before, current, newBoot) {
		return current, nil
	}
	if legacyV1Profile(before) {
		observed, err := legacyV1Observation(ctx, c.Kind)
		if err != nil {
			return current, fmt.Errorf("Historical V1 job observations need operator repair: %w", err)
		}
		if compatibleRecoveryObservations(before, observed, newBoot) {
			return current, nil
		}
	}
	return current, errors.New("Interrupted job changed protected code/configuration; retain its evidence and repair the changed inputs before reconciliation")
}

func compatibleRecoveryObservations(before, after profile, newBoot bool) bool {
	mutableFile := func(path string) bool {
		return before.Kind == ComposerUpdate && composerMutableData(path) || before.Kind == CertificateRenewal && strings.HasPrefix(path, "/etc/letsencrypt/renewal/")
	}
	mutableDirectory := func(path string) bool { return before.Kind == ComposerUpdate && composerMutableData(path) }
	// Keep the established data-effect exceptions. Every other observation,
	// including an empty value and the reboot-tolerant master anchor, must be
	// present in both producers. A missing key is not an empty observation.
	return sameRecoveryKeys(before.Files, after.Files, mutableFile) &&
		sameRecoveryKeys(before.Directories, after.Directories, mutableDirectory) &&
		sameRecoveryKeys(before.External, after.External, nil) &&
		compatibleProfiles(before, after, false, newBoot)
}

func sameRecoveryKeys[V any](before, after map[string]V, mutable func(string) bool) bool {
	for key := range before {
		if mutable != nil && mutable(key) {
			continue
		}
		if _, present := after[key]; !present {
			return false
		}
	}
	for key := range after {
		if mutable != nil && mutable(key) {
			continue
		}
		if _, present := before[key]; !present {
			return false
		}
	}
	return true
}

func legacyV1Profile(p profile) bool {
	if p.Kind != ComposerUpdate && p.Kind != CertificateRenewal && p.Kind != PackageCleanup {
		return false
	}
	// Presence matters, including empty markers. Mixed/current/V2 profiles do
	// not acquire a guessed compatibility interpretation. Exact producer output
	// is still compared in both directions after this conservative selection.
	for _, key := range []string{"dpkg-bootstrap-contract", "apt-execution-profile", "apt-host-input-contract", "apt-platform-profile", "certbot-platform-profile"} {
		if _, exists := p.External[key]; exists {
			return false
		}
	}
	return len(p.Files) > 0
}

// Read-only baseline producers used stdout, while current inspection also
// validates stderr. Preserve that observation boundary in a clean environment;
// never reconstruct unrecorded inherited process configuration.
func legacyV1Run(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	if name != "/usr/bin/apt-config" && name != "/usr/bin/dpkg-query" {
		return "", errors.New("unsupported V1 observation command")
	}
	probe, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return syscmd.RunMaintenanceStdout(probe, name, args...)
}
