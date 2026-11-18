package scheduler

import (
	"errors"
	"maps"
)

const resoluteSystemd2599Profile = "ubuntu-resolute-cron-v1/systemd2599-arm64-v1"
const resoluteSystemd2599Version = "259.9-0ubuntu0.1"

// This is a versioned input profile of the existing digest-bound backend, not
// a new reader contract. Old identities and configuration digests are never
// rewritten: a changed configuration still fails ordinary exact equality.
func resoluteUpdateProfile(c distributionCronCandidate, architecture string, status []byte, retainedCore10 bool) (distributionCronCandidate, error) {
	if c.id != resoluteCronCandidateID || architecture != "arm64" {
		return c, nil
	}
	a, err := pamArchitectureFor(architecture)
	if err != nil {
		return c, err
	}
	installed, err := pamInstalledFor(a, status)
	if err != nil {
		return c, err
	}
	if installed["libnss-systemd"] != resoluteSystemd2599Version {
		return c, nil
	}
	for _, name := range []string{"libnss-systemd", "libpam-systemd", "systemd", "libsystemd-shared", "libsystemd0", "libudev1"} {
		if installed[name] != resoluteSystemd2599Version {
			return c, errors.New("incomplete reviewed Resolute systemd update")
		}
	}
	c.files, c.directories = maps.Clone(c.files), maps.Clone(c.directories)
	c.loaderProfile, c.systemdVersion = resoluteSystemd2599Profile, resoluteSystemd2599Version
	c.files["/etc/security/limits.d/20-coredump-debian.conf"] = "264c87465f59c92067860c2f1dba07c05ebdc07477b39b7b1301316626044643"
	c.directories["/etc/security/limits.d"] = []string{"20-coredump-debian.conf"}
	delete(c.files, "/etc/security/limits.d/10-coredump-debian.conf")
	if retainedCore10 {
		c.files["/etc/security/limits.d/10-coredump-debian.conf"] = "f4ac5b01865241e90f26cfb4e483b04ff80eb231d3b9fe8ad7057b498dd8f58e"
		c.directories["/etc/security/limits.d"] = []string{"10-coredump-debian.conf", "20-coredump-debian.conf"}
	}
	return c, nil
}
