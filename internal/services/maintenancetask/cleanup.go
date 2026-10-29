package maintenancetask

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"fluxo/internal/syscmd"
)

// The initial package profile is A0: no package removal/configuration, followed
// by cache cleanup. Nonempty autoremove needs a reviewed script/trigger closure;
// it is reported as blocked, never silently treated as completed.
func qualifyCleanup(ctx context.Context, p *profile) error {
	if err := packageInputs(ctx, p, "apt"); err != nil {
		return err
	}
	if !strings.HasPrefix(p.External["package:apt"], "2.8.") {
		return errors.New("APT cleanup implementation needs compatibility review")
	}
	for _, path := range []string{"/usr/bin/apt-get", "/usr/bin/apt-config", "/var/lib/dpkg/status", "/var/lib/apt/extended_states"} {
		if err := p.file(path, path == "/var/lib/apt/extended_states"); err != nil {
			return err
		}
	}
	out, err := syscmd.Run(ctx, 10*time.Second, "/usr/bin/apt-config", "dump")
	if err != nil {
		return err
	}
	if err = validateAPTConfig(out); err != nil {
		return err
	}
	p.External["apt-effective-configuration"] = digest([]byte(out))
	return nil
}

func validateAPTConfig(out string) error {
	binaries := map[string]string{"dir::bin::methods": "/usr/lib/apt/methods", "dir::bin::solvers": "/usr/lib/apt/solvers", "dir::bin::planners": "/usr/lib/apt/planners", "dir::bin::dpkg": "/usr/bin/dpkg", "dir::bin::gzip": "/bin/gzip", "dir::bin::bzip2": "/bin/bzip2", "dir::bin::xz": "/usr/bin/xz", "dir::bin::lz4": "/usr/bin/lz4", "dir::bin::zstd": "/usr/bin/zstd", "dir::bin::lzma": "/usr/bin/xz"}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, rest, ok := strings.Cut(line, " ")
		if !ok {
			return errors.New("Unsupported APT configuration serialization")
		}
		key = strings.ToLower(key)
		encoded := strings.TrimSpace(rest)
		// apt-config dump retains regex backslashes literally; it is not JSON or
		// a Go string literal. Only strip its documented outer quote/semicolon.
		if len(encoded) < 3 || !strings.HasPrefix(encoded, "\"") || !strings.HasSuffix(encoded, "\";") {
			return fmt.Errorf("Unsupported APT configuration value for %s", key)
		}
		value := encoded[1 : len(encoded)-2]
		if strings.Contains(key, "invoke") || strings.Contains(key, "pre-install-pkgs") {
			if value == "" {
				continue
			}
			if strings.HasPrefix(key, "apt::update::") || strings.HasPrefix(key, "dpkg::post-invoke") || strings.HasPrefix(key, "dpkg::pre-install-pkgs") {
				continue
			}
			return fmt.Errorf("APT hook needs maintenance review: %s", key)
		}
		if strings.HasPrefix(key, "dir::bin") && value != "" {
			base := strings.TrimSuffix(key, "::")
			if expected, ok := binaries[base]; !ok || expected != value {
				return fmt.Errorf("APT executable override needs maintenance review: %s", key)
			}
		}
		if (key == "apt::solver" || key == "apt::planner") && value != "internal" && value != "" {
			return errors.New("External APT planning programs need maintenance review")
		}
	}
	return nil
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
