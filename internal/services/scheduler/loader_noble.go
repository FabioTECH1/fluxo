package scheduler

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// This is a loader-policy profile, never a replacement backend label. Existing
// Noble retirement identities retain their historical empty Backend meaning.
const nobleLoaderPAMProfileID = "ubuntu-noble-cron-loader-policy-v1"

func (p *pamCandidateInspection) nobleSELinuxInputs(pid int) error {
	for _, path := range []string{"/etc/selinux/config", "/run/setrans/.setrans-unix", "/var/run/setrans/.setrans-unix"} {
		if err := p.requireAbsent(path, "Noble loader profile requires absent SELinux configuration/translation authority"); err != nil {
			return err
		}
	}
	for _, name := range []string{"/proc/self/mountinfo", fmt.Sprintf("/proc/%d/mountinfo", pid)} {
		raw, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.SplitN(line, " - ", 2)
			if len(fields) == 2 && strings.HasPrefix(fields[1], "selinuxfs ") {
				return errors.New("active SELinux mount requires a separate loader profile")
			}
		}
	}
	root, err := os.Readlink(fmt.Sprintf("/proc/%d/root", pid))
	if err != nil || root != "/" {
		return errors.New("Noble cron uses an unreviewed filesystem root")
	}
	cronNS, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/mnt", pid))
	if err != nil {
		return err
	}
	managerNS, err := os.Readlink("/proc/1/ns/mnt")
	if err != nil || cronNS != managerNS {
		return errors.New("Noble cron does not share the reviewed manager mount namespace")
	}
	p.report.Identity["cron-mount-namespace"] = cronNS
	p.report.Identity["selinux-observed-inputs"] = "no-selinuxfs/no-config/no-translation-socket"
	// This observation does not claim to read libselinux's cached constructor
	// flags. Source review separately covers both selected conditional paths.
	return nil
}
