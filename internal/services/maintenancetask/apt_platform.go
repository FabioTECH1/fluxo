package maintenancetask

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
)

type aptConfigInput struct {
	sha256   string
	required bool
}
type aptPlatformSpec struct {
	release, codename, aptVersion, inputRevision string
	inputs                                       map[string]aptConfigInput
	executionQualified                           bool
}

var errAPTCandidateProfile = errors.New("APT cleanup profile remains an unqualified candidate")

// Security-update source review (2026-11-17): official fully patched archives
// under https://archive.ubuntu.com/ubuntu/pool/main/a/apt/ . SHA256 of tar.xz,
// verified against the matching HTTPS .dsc:
//
//	2.4.15ubuntu0.1: 66114eb42e88fa5613b9bcf779596d3c3d7a1af81dedb415db437e3370349270
//	2.8.4ubuntu0.1: 727dc3beaf35623cb49120843b2c0255bd23eeaa00d0481864b2e6e4f50c01e3
//	3.2.1ubuntu0.1: 0febcf286d6038c534c4aa83119dbf8a4dda29a10e133ca97adb01b368d420cc
//
// Rechecked pkgInitConfig ordering; binary/Version promotion; internal solver
// selection; NoQueue before QueueURI; GetArchives before DownloadAllowed;
// empty-plan return and first failing dpkg Pre-Invoke before package hooks,
// logging and dpkg; autoclean archive paths; exact method/alias inventories.
// The apt-a0-noqueue-v3 fences and raw conffile approvals remain unchanged.
// Native ARM NoQueue/archive/codec checks passed in run 44322233679; Resolute's
// additional solver-3 fence passed in run 44323611261. Host/provider and exact
// package/configuration admission remain independent mandatory gates.
func aptPlatformForRelease(raw []byte) (aptPlatformSpec, error) {
	fields := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != key || key == "" {
			return aptPlatformSpec{}, errors.New("unsupported vendor OS identity")
		}
		if _, duplicate := fields[key]; duplicate {
			return aptPlatformSpec{}, errors.New("duplicate vendor OS identity field")
		}
		if strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "'") {
			if len(value) < 2 || value[len(value)-1] != value[0] {
				return aptPlatformSpec{}, errors.New("malformed vendor OS identity")
			}
			value = value[1 : len(value)-1]
		}
		fields[key] = value
	}
	if fields["ID"] != "ubuntu" {
		return aptPlatformSpec{}, errors.New("APT cleanup requires a reviewed Ubuntu LTS platform")
	}
	var spec aptPlatformSpec
	switch fields["VERSION_ID"] {
	case "22.04":
		spec = aptPlatformSpec{"22.04", "jammy", "2.4.15ubuntu0.1", "jammy-apt-config-inputs-v1", jammyAPTConfigInputs, true}
	case "24.04":
		spec = aptPlatformSpec{"24.04", "noble", "2.8.4ubuntu0.1", aptHostInputsRevision, nobleAPTConfigInputs, true}
	case "26.04":
		spec = aptPlatformSpec{"26.04", "resolute", "3.2.1ubuntu0.1", "resolute-apt-config-inputs-v1", resoluteAPTConfigInputs, true}
	default:
		return spec, errors.New("APT cleanup platform requires compatibility review")
	}
	if spec.release != "24.04" && runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		spec.executionQualified = false
	}
	if fields["VERSION_CODENAME"] != spec.codename || fields["UBUNTU_CODENAME"] != "" && fields["UBUNTU_CODENAME"] != spec.codename {
		return aptPlatformSpec{}, errors.New("vendor OS release/codename identity disagrees")
	}
	return spec, nil
}

// Select from protected vendor data, never from an executable, environment OS
// hint or APT's configuration parser. A local override must match the vendor
// data exactly; only the standard root-owned os-release link is admitted.
func selectAPTPlatform(p *profile) (aptPlatformSpec, error) {
	raw, err := readRegular("/usr/lib/os-release", 1<<20)
	if err != nil {
		return aptPlatformSpec{}, err
	}
	spec, err := aptPlatformForRelease(raw)
	if err != nil {
		return spec, err
	}
	if err = trustedDatabaseDirectory("/etc"); err != nil {
		return spec, err
	}
	before, err := os.Lstat("/etc/os-release")
	if err != nil {
		return spec, err
	}
	if before.Mode()&os.ModeSymlink != 0 {
		st, ok := before.Sys().(*syscall.Stat_t)
		target, err := os.Readlink("/etc/os-release")
		if err != nil || !ok || st.Uid != 0 || st.Nlink != 1 || target != "../usr/lib/os-release" && target != "/usr/lib/os-release" {
			return spec, errors.New("effective OS identity link needs compatibility review")
		}
		after, err := os.Lstat("/etc/os-release")
		if err != nil || !os.SameFile(before, after) {
			return spec, errors.New("effective OS identity changed during inspection")
		}
		p.External["apt-os-release-link"] = target
	} else {
		effective, err := readRegular("/etc/os-release", 1<<20)
		if err != nil {
			return spec, err
		}
		if !bytes.Equal(raw, effective) {
			return spec, errors.New("effective OS identity differs from protected vendor identity")
		}
		p.Files["/etc/os-release"] = digest(effective)
	}
	p.Files["/usr/lib/os-release"] = digest(raw)
	p.External["apt-platform-profile"] = fmt.Sprintf("ubuntu-%s:apt-%s", spec.release, spec.aptVersion)
	return spec, nil
}
