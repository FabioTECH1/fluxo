package scheduler

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type pamCandidateObservation struct {
	Dev, Ino              uint64
	Mode, UID, GID, Nlink uint32
	Size                  int64
	Mtime, Ctime          unix.Timespec
}
type pamCandidatePayload struct{ MD5, Package string }

type pamDirectorySnapshotChanged struct{ path, phase string }

func (e *pamDirectorySnapshotChanged) Error() string {
	return fmt.Sprintf("candidate input changed %s: %s", e.phase, e.path)
}
func pamSameProtectedDirectory(a, b pamCandidateObservation) bool {
	return a.Mode&unix.S_IFMT == unix.S_IFDIR && a.Mode&0022 == 0 && a.UID == 0 &&
		a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.UID == b.UID && a.GID == b.GID
}

func pamCandidateObserve(path string) (pamCandidateObservation, error) {
	var s unix.Stat_t
	if err := unix.Lstat(path, &s); err != nil {
		return pamCandidateObservation{}, err
	}
	return pamCandidateObservation{uint64(s.Dev), s.Ino, s.Mode, s.Uid, s.Gid, uint32(s.Nlink), s.Size, s.Mtim, s.Ctim}, nil
}

// Follow only protected root-owned aliases, recording every component and link.
// Do not use EvalSymlinks before checking ownership of the traversal itself.
func (p *pamCandidateInspection) canonical(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", errors.New("noncanonical candidate input path")
	}
	for links := 0; links < 24; links++ {
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		current := "/"
		changed := false
		for i, part := range parts {
			current = filepath.Join(current, part)
			s, err := pamCandidateObserve(current)
			if err != nil {
				return "", err
			}
			if s.UID != 0 || s.Mode&unix.S_IFMT != unix.S_IFLNK && s.Mode&0022 != 0 {
				return "", fmt.Errorf("unprotected candidate path component: %s", current)
			}
			if err := p.remember(current, s); err != nil {
				return "", err
			}
			if s.Mode&unix.S_IFMT == unix.S_IFLNK {
				target, err := os.Readlink(current)
				if err != nil {
					return "", err
				}
				if !filepath.IsAbs(target) {
					target = filepath.Join(filepath.Dir(current), target)
				}
				path = filepath.Join(append([]string{target}, parts[i+1:]...)...)
				changed = true
				break
			}
			if i < len(parts)-1 && s.Mode&unix.S_IFMT != unix.S_IFDIR {
				return "", errors.New("candidate path crossed a non-directory")
			}
		}
		if !changed {
			return path, nil
		}
	}
	return "", errors.New("candidate alias traversal exceeded bound")
}

func (p *pamCandidateInspection) read(path string, limit int64) ([]byte, error) {
	canonical, err := p.canonical(path)
	if err != nil {
		return nil, err
	}
	raw, err := p.readCanonical(canonical, limit)
	if err != nil {
		return nil, err
	}
	p.record(path, raw)
	return raw, nil
}
func (p *pamCandidateInspection) readPrivateIdentity(path string) ([]byte, error) {
	canonical, err := p.canonical(path)
	if err != nil {
		return nil, err
	}
	// Identity data is parsed only in memory. Neither credential contents nor
	// credential-derived hashes are copied to the public qualification report.
	return p.readCanonical(canonical, 8<<20)
}
func (p *pamCandidateInspection) readCanonical(path string, limit int64) ([]byte, error) {
	before, err := pamCandidateObserve(path)
	if err != nil {
		return nil, err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.UID != 0 || before.Mode&0022 != 0 || before.Nlink != 1 || before.Size < 0 || before.Size > limit {
		return nil, fmt.Errorf("unsafe candidate regular input: %s", path)
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var opened unix.Stat_t
	if err = unix.Fstat(fd, &opened); err != nil || uint64(opened.Dev) != before.Dev || opened.Ino != before.Ino {
		return nil, errors.New("candidate input replaced before read")
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	after, err := pamCandidateObserve(path)
	if err != nil || after != before || int64(len(raw)) != before.Size {
		return nil, errors.New("candidate input changed during read")
	}
	if err := p.remember(path, before); err != nil {
		return nil, err
	}
	return raw, nil
}
func (p *pamCandidateInspection) sensitiveMetadata(path string) error {
	canonical, err := p.canonical(path)
	if err != nil {
		return err
	}
	s, err := pamCandidateObserve(canonical)
	if err != nil {
		return err
	}
	if s.Mode&unix.S_IFMT != unix.S_IFREG || s.Nlink != 1 || s.Mode&0007 != 0 {
		return fmt.Errorf("unprotected credential-file metadata: %s", path)
	}
	if err := p.remember(canonical, s); err != nil {
		return err
	}
	return nil
}

var pamCandidateVendorPackages = []string{"cron", "libpam0g", "libpam-modules", "libpam-modules-bin", "libpam-cap", "libpam-systemd", "libcap2", "libaudit1", "libcap-ng0", "libselinux1", "libpcre2-8-0", "libcrypt1", "libc6", "libnss-systemd", "systemd", "libsystemd-shared", "libsystemd0", "liblzma5", "libzstd1", "liblz4-1", "libgcrypt20", "libgpg-error0", "libssl3", "libssl3t64", "libmount1", "libblkid1", "libacl1", "libseccomp2", "libapparmor1", "libdb5.3", "libdb5.3t64", "libgcc-s1", "libstdc++6", "libidn2-0", "libunistring2", "libunistring5", "libudev1", "libnsl2", "libtirpc3", "libtirpc3t64", "libgssapi-krb5-2", "libkrb5-3", "libk5crypto3", "libcom-err2", "libkrb5support0", "libkeyutils1"}

func (p *pamCandidateInspection) vendorPayloads(id string) error {
	raw, err := p.read("/var/lib/dpkg/status", 32<<20)
	if err != nil {
		return err
	}
	installed, err := pamCandidateInstalled(raw)
	if err != nil {
		return err
	}
	expected := map[string]string{}
	switch id {
	case nobleLoaderPAMProfileID, cronieLoaderPAMProfileID:
		expected = map[string]string{"libpam0g": "1.5.3-5ubuntu5.7", "libpam-modules": "1.5.3-5ubuntu5.7", "libpam-modules-bin": "1.5.3-5ubuntu5.7", "libpam-cap": "1:2.66-5ubuntu2.4", "libcap2": "1:2.66-5ubuntu2.4", "libnss-systemd": "255.4-1ubuntu8.18", "libpam-systemd": "255.4-1ubuntu8.18", "libselinux1": "3.5-2ubuntu2.1", "libc6": "2.39-0ubuntu8.12"}
	case jammyCronCandidateID:
		expected = map[string]string{"libpam0g": "1.4.0-11ubuntu2.8", "libpam-modules": "1.4.0-11ubuntu2.8", "libpam-modules-bin": "1.4.0-11ubuntu2.8", "libpam-cap": "1:2.44-1ubuntu0.22.04.3", "libcap2": "1:2.44-1ubuntu0.22.04.3", "libnss-systemd": "249.11-0ubuntu3.23", "libpam-systemd": "249.11-0ubuntu3.23"}
	case resoluteCronCandidateID:
		expected = map[string]string{"libpam0g": "1.7.0-5ubuntu3.2", "libpam-modules": "1.7.0-5ubuntu3.2", "libpam-modules-bin": "1.7.0-5ubuntu3.2", "libpam-cap": "1:2.75-10ubuntu2", "libcap2": "1:2.75-10ubuntu2", "libnss-systemd": "259.5-0ubuntu3.4", "libpam-systemd": "259.5-0ubuntu3.4"}
	}
	if id == cronieLoaderPAMProfileID {
		expected["cronie"] = "1.7.1-1"
	}

	systemdVersion := expected["libnss-systemd"]
	for _, pkg := range []string{"systemd", "libsystemd-shared", "libsystemd0", "libudev1"} {
		if version, exists := installed[pkg]; exists && version != systemdVersion {
			return fmt.Errorf("reviewed systemd lookup dependency version differs: %s", pkg)
		}
	}
	for pkg, version := range expected {
		if installed[pkg] != version {
			return fmt.Errorf("reviewed vendor package version differs: %s", pkg)
		}
	}
	packages := pamCandidateVendorPackages
	if id == cronieLoaderPAMProfileID {
		packages = append(append([]string(nil), packages...), "cronie")
	}
	for _, pkg := range packages {
		version, ok := installed[pkg]
		if !ok {
			continue
		}
		p.report.Packages[pkg] = version
		// Multi-Arch metadata may be qualified or unqualified; exactly one protected
		// installed manifest is accepted. The local dpkg payload database is trusted
		// OS metadata, never a claim of fresh signed repository provenance.
		var manifest []byte
		for _, suffix := range []string{":amd64", ""} {
			path := "/var/lib/dpkg/info/" + pkg + suffix + ".md5sums"
			data, e := p.read(path, 16<<20)
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return e
			}
			if manifest != nil {
				return fmt.Errorf("ambiguous vendor payload manifests: %s", pkg)
			}
			manifest = data
		}
		if manifest == nil {
			return fmt.Errorf("vendor payload manifest is unavailable: %s", pkg)
		}
		entries, e := parsePAMCandidateMD5(manifest)
		if e != nil {
			return e
		}
		for path, sum := range entries {
			// Only executable/library payloads can become code authority here. Do not
			// canonicalize unrelated package data or missing configuration templates.
			if !strings.HasPrefix(path, "usr/lib/") && !strings.HasPrefix(path, "lib/") && !strings.HasPrefix(path, "usr/sbin/") && !strings.HasPrefix(path, "sbin/") {
				continue
			}
			canonical, e := p.canonical("/" + path)
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return e
			}
			if old, ok := p.payload[canonical]; ok && old.MD5 != sum {
				return fmt.Errorf("conflicting vendor code ownership: %s", canonical)
			}
			p.payload[canonical] = pamCandidatePayload{sum, pkg}
		}
	}
	return nil
}

func pamCandidateInstalled(raw []byte) (map[string]string, error) {
	result := map[string]string{}
	for _, paragraph := range strings.Split(string(raw), "\n\n") {
		fields := map[string]string{}
		for _, line := range strings.Split(paragraph, "\n") {
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
				continue
			}
			key, value, ok := strings.Cut(line, ":")
			if ok {
				if _, duplicate := fields[key]; duplicate {
					return nil, errors.New("duplicate protected package status field")
				}
				fields[key] = strings.TrimSpace(value)
			}
		}
		if fields["Status"] != "install ok installed" {
			continue
		}
		if fields["Architecture"] != "amd64" && fields["Architecture"] != "all" {
			continue
		}
		name := fields["Package"]
		if name == "" {
			return nil, errors.New("invalid installed vendor package metadata")
		}
		if _, ok := result[name]; ok {
			return nil, errors.New("ambiguous installed vendor package architecture")
		}
		result[name] = fields["Version"]
	}
	return result, nil
}

func parsePAMCandidateMD5(raw []byte) (map[string]string, error) {
	result := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" {
			continue
		}
		if len(line) < 35 || line[32:34] != "  " {
			return nil, errors.New("invalid protected vendor payload metadata")
		}
		sum, path := line[:32], line[34:]
		decoded, err := hex.DecodeString(sum)
		if err != nil || len(decoded) != md5.Size || strings.ToLower(sum) != sum || filepath.IsAbs(path) || filepath.Clean(path) != path || path == "." || strings.HasPrefix(path, "../") || strings.ContainsAny(path, "\x00\r\n") {
			return nil, errors.New("invalid protected vendor payload entry")
		}
		if _, ok := result[path]; ok {
			return nil, errors.New("duplicate vendor payload entry")
		}
		result[path] = sum
	}
	return result, nil
}

func (p *pamCandidateInspection) authenticate(path string, raw []byte) error {
	payload, ok := p.payload[path]
	if !ok {
		return fmt.Errorf("reachable code is not owned by a reviewed vendor package: %s", path)
	}
	sum := md5.Sum(raw)
	if hex.EncodeToString(sum[:]) != payload.MD5 {
		return fmt.Errorf("reachable code differs from protected vendor payload: %s", path)
	}
	p.record(path, raw)
	return nil
}

func (p *pamCandidateInspection) remember(path string, observation pamCandidateObservation) error {
	if old, ok := p.observed[path]; ok && old != observation {
		if pamSameProtectedDirectory(old, observation) {
			return &pamDirectorySnapshotChanged{path, "between observations"}
		}
		return fmt.Errorf("candidate input changed between observations: %s", path)
	}
	p.observed[path] = observation
	return nil
}
