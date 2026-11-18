package scheduler

import (
	"reflect"
	"strings"
	"testing"
)

func TestResoluteUpdateIsSeparateAndKeepsHistoricalIdentity(t *testing.T) {
	status := func(version string, omit string) []byte {
		var raw string
		for _, name := range []string{"libnss-systemd", "libpam-systemd", "systemd", "libsystemd-shared", "libsystemd0", "libudev1"} {
			if name != omit {
				raw += "Package: " + name + "\nStatus: install ok installed\nArchitecture: arm64\nVersion: " + version + "\n\n"
			}
		}
		return []byte(raw)
	}
	for _, arch := range []string{"amd64", "arm64"} {
		base, _ := distributionCandidateForArchitecture(resoluteCronCandidateID, arch)
		old, err := resoluteUpdateProfile(base, arch, status("259.5-0ubuntu3.4", ""), true)
		if err != nil || !reflect.DeepEqual(old, base) {
			t.Fatal("old profile changed", err)
		}
		base, _ = distributionCandidateForArchitecture(resoluteCronCandidateID, arch)
		before, _ := distributionCandidateForArchitecture(resoluteCronCandidateID, arch)
		current, err := resoluteUpdateProfile(base, arch, status(resoluteSystemd2599Version, ""), true)
		if err != nil {
			t.Fatal(err)
		}
		if arch == "amd64" {
			if !reflect.DeepEqual(current, before) {
				t.Fatal("amd64 profile changed")
			}
			continue
		}
		if current.id != before.id || current.binary != before.binary || current.loaderProfile != resoluteSystemd2599Profile || current.systemdVersion != resoluteSystemd2599Version {
			t.Fatal("backend meaning or profile selection changed")
		}
		if len(current.directories["/etc/security/limits.d"]) != 2 || current.files["/etc/security/limits.d/20-coredump-debian.conf"] == "" {
			t.Fatal("new vendor fragment not pinned")
		}
		if !reflect.DeepEqual(base, before) {
			t.Fatal("source profile mutated")
		}
		clean, err := resoluteUpdateProfile(base, arch, status(resoluteSystemd2599Version, ""), false)
		if err != nil || len(clean.directories["/etc/security/limits.d"]) != 1 || clean.files["/etc/security/limits.d/10-coredump-debian.conf"] != "" {
			t.Fatal("clean updated vendor inventory refused", err)
		}
		for _, omit := range []string{"libpam-systemd", "systemd", "libsystemd-shared", "libsystemd0", "libudev1"} {
			fresh, _ := distributionCandidateForArchitecture(resoluteCronCandidateID, arch)
			if _, err := resoluteUpdateProfile(fresh, arch, status(resoluteSystemd2599Version, omit), true); err == nil {
				t.Fatal("mixed/incomplete update admitted", omit)
			}
		}
	}
	for _, role := range BackendReaderContracts() {
		if strings.Contains(role, "2599") {
			t.Fatal("loader profile became a reader contract")
		}
	}
}
