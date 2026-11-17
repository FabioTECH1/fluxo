package maintenancetask

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"fluxo/internal/syscmd"
)

const aptCleanupExecutionProfile = "apt-a0-noqueue-v3"

// The initial package profile is A0: no package removal/configuration, followed
// by cache cleanup. Nonempty autoremove needs a reviewed script/trigger closure;
// it is reported as blocked, never silently treated as completed.
func qualifyCleanup(ctx context.Context, p *profile) error {
	platform, err := inspectCleanupPlatform(ctx, p)
	if err != nil {
		return err
	}
	if !platform.executionQualified {
		return fmt.Errorf("Ubuntu %s / APT %s: %w", platform.release, platform.aptVersion, errAPTCandidateProfile)
	}
	return nil
}

// inspectCleanupPlatform gathers read-only evidence; it grants no execution
// authority to a candidate. qualifyCleanup is the production admission gate.
func inspectCleanupPlatform(ctx context.Context, p *profile) (aptPlatformSpec, error) {
	return inspectCleanupPlatformVersion(ctx, p, "")
}

// The historical tuple is inspection-only and used solely by explicit recovery.
// Ordinary qualification always selects the current exact production tuple.
func inspectCleanupPlatformVersion(ctx context.Context, p *profile, historical string) (aptPlatformSpec, error) {
	platform, err := selectAPTPlatform(p)
	if err != nil {
		return platform, err
	}
	if historical != "" {
		if historical != "2.8.3" || platform.release != "24.04" {
			return platform, errors.New("unsupported historical APT recovery tuple")
		}
		platform.aptVersion, platform.executionQualified = historical, false
		p.External["apt-platform-profile"] = "ubuntu-24.04:apt-" + historical
	}
	inputs, err := inspectAPTPlatformInputs(diskAPTInputSource(), platform)
	if err != nil {
		return platform, err
	}
	inputs.record(p)
	if err := packageInputs(ctx, p, "apt"); err != nil {
		return platform, err
	}
	if p.External["package:apt"] != platform.aptVersion {
		return platform, errors.New("APT cleanup implementation needs compatibility review")
	}
	if err := inspectAPTNativeCodecs(p); err != nil {
		return platform, err
	}
	if err := inspectAPTMethodInventory(platform, p); err != nil {
		return platform, err
	}
	if err := inspectAPTSourceInputs(p); err != nil {
		return platform, err
	}
	p.External["apt-execution-profile"] = aptCleanupExecutionProfile
	solver, err := cleanupAPTSolver(platform.aptVersion)
	if err != nil {
		return platform, err
	}
	p.External["apt-fixed-solver"] = solver
	for _, path := range []string{"/usr/bin/apt-get", "/usr/bin/apt-config", "/var/lib/dpkg/status", "/var/lib/apt/extended_states"} {
		if err := p.file(path, path == "/var/lib/apt/extended_states"); err != nil {
			return platform, err
		}
	}
	if err := qualifyAPTLayout(p); err != nil {
		return platform, err
	}
	var output bytes.Buffer
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := inputs.verifyAndRun(diskAPTInputSource(), func() error {
		return syscmd.RunMaintenance(probe, "", &output, nil, "/usr/bin/apt-config", "dump")
	}); err != nil {
		return platform, err
	}
	out := output.String()
	if err := validateAPTConfigForPlatform(out, platform); err != nil {
		return platform, err
	}
	p.External["apt-effective-configuration"] = digest([]byte(out))
	return platform, nil
}

func validateNoPackageChanges(out string) error {
	for _, line := range strings.Split(out, "\n") {
		for _, prefix := range []string{"Remv ", "Purg ", "Inst ", "Conf "} {
			if strings.HasPrefix(line, prefix) {
				return errors.New("System Cleanup selected packages with scripts or triggers requiring review; no package changes were made")
			}
		}
	}
	if !strings.Contains(out, "0 upgraded, 0 newly installed, 0 to remove") {
		return errors.New("APT cleanup did not produce a supported empty removal plan")
	}
	return nil
}

// These options belong to V3 execution authority, not retained V1/V2 history.
// NoQueue fences method startup before DownloadAllowed's later filtering.
func cleanupAPTSolver(version string) (string, error) {
	switch version {
	case "2.4.14", "2.8.3", "2.4.15ubuntu0.1", "2.8.4ubuntu0.1":
		return "internal", nil
	case "3.2.0", "3.2.1ubuntu0.1":
		return "3.0", nil
	}
	return "", errors.New("APT cleanup version has no reviewed fixed solver")
}
func cleanupAPTArguments(p profile) ([]string, error) {
	if p.Kind != PackageCleanup || p.External["apt-execution-profile"] != aptCleanupExecutionProfile {
		return nil, errors.New("APT cleanup requires the NoQueue V3 execution profile")
	}
	solver, err := cleanupAPTSolver(p.External["package:apt"])
	if err != nil {
		return nil, err
	}
	if p.External["apt-fixed-solver"] != solver {
		return nil, errors.New("APT cleanup fixed solver does not match its reviewed version")
	}
	return []string{"-o", "Debug::pkgAcqArchive::NoQueue=true", "--no-download", "-o", "APT::Solver=" + solver, "-o", "APT::Planner=internal"}, nil
}
