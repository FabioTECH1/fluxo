package maintenancetask

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

const aptMethodsDirectory = "/usr/lib/apt/methods"

// This read-only candidate inventory grants no method dispatch authority. It
// closes the extra-file gap left by verifying only files named in md5sums.
// Source aliases come from methods/CMakeLists.txt and CMake/Misc.cmake:add_links
// in the exact original and security-update trees listed in apt_platform.go,
// not from installed symlink guesses.
func inspectAPTMethodInventory(spec aptPlatformSpec, p *profile) error {
	if spec.aptVersion == "" || p.External["package:apt"] != spec.aptVersion {
		return errors.New("APT method inventory requires the matching verified package profile")
	}
	regular, aliases, err := aptMethodPolicy(spec.aptVersion)
	if err != nil {
		return err
	}
	const sumsPath = "/var/lib/dpkg/info/apt.md5sums"
	const listPath = "/var/lib/dpkg/info/apt.list"
	sums, err := readRegular(sumsPath, 8<<20)
	if err != nil {
		return err
	}
	listing, err := readRegular(listPath, 8<<20)
	if err != nil {
		return err
	}
	expected, err := parseAPTMethodManifests(sums, listing, regular, aliases)
	if err != nil {
		return err
	}
	if err = trustedDatabaseDirectory(aptMethodsDirectory); err != nil {
		return err
	}
	entries, err := os.ReadDir(aptMethodsDirectory)
	if err != nil {
		return err
	}
	var actual []string
	for _, entry := range entries {
		actual = append(actual, entry.Name())
	}
	slices.Sort(actual)
	var wanted []string
	for name := range expected {
		wanted = append(wanted, name)
	}
	slices.Sort(wanted)
	if !slices.Equal(actual, wanted) {
		return errors.New("APT method directory membership needs compatibility review")
	}
	for name, md5sum := range expected {
		path := filepath.Join(aptMethodsDirectory, name)
		if target, link := aliases[name]; link {
			before, err := os.Lstat(path)
			if err != nil {
				return err
			}
			st, ok := before.Sys().(*syscall.Stat_t)
			value, err := os.Readlink(path)
			if err != nil || !ok || before.Mode()&os.ModeSymlink == 0 || st.Uid != 0 || st.Gid != 0 || st.Nlink != 1 || value != target {
				return errors.New("APT method alias needs compatibility review")
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) {
				return errors.New("APT method alias changed during inspection")
			}
			p.External["apt-method-alias:"+name] = target
			continue
		}
		data, err := readRegular(path, 32<<20)
		if err != nil {
			return err
		}
		sum := md5.Sum(data)
		if hex.EncodeToString(sum[:]) != md5sum {
			return errors.New("APT method payload differs from protected package metadata")
		}
		p.Files[path] = digest(data)
	}
	p.Files[sumsPath], p.Files[listPath] = digest(sums), digest(listing)
	p.Directories[aptMethodsDirectory] = actual
	p.External["apt-method-inventory"] = "vendor-methods-candidate-v1:" + spec.aptVersion
	return nil
}

func aptMethodPolicy(version string) (map[string]bool, map[string]string, error) {
	regular := map[string]bool{"file": true, "copy": true, "store": true, "gpgv": true, "cdrom": true, "http": true, "mirror": true, "rred": true}
	aliases := map[string]string{"https": "http", "mirror+http": "mirror", "mirror+https": "mirror", "mirror+file": "mirror", "mirror+copy": "mirror"}
	switch version {
	case "2.4.14", "2.8.3", "2.4.15ubuntu0.1", "2.8.4ubuntu0.1":
		regular["ftp"], regular["rsh"] = true, true
		aliases["ssh"], aliases["mirror+ftp"] = "rsh", "mirror"
	case "3.2.0", "3.2.1ubuntu0.1": // Optional sqv is handled by the authenticated package list.
		regular["sqv"] = false
	default:
		return nil, nil, errors.New("APT method implementation requires source review")
	}
	return regular, aliases, nil
}

func parseAPTMethodManifests(sums, listing []byte, regular map[string]bool, aliases map[string]string) (map[string]string, error) {
	hashes, err := parseProtectedMD5Manifest(sums)
	if err != nil {
		return nil, err
	}
	expected := map[string]string{}
	for _, line := range strings.Split(string(listing), "\n") {
		if line == aptMethodsDirectory || !strings.HasPrefix(line, aptMethodsDirectory+"/") {
			continue
		}
		name := strings.TrimPrefix(line, aptMethodsDirectory+"/")
		if filepath.Base(name) != name || filepath.Clean(line) != line {
			return nil, errors.New("aliased APT method package-list path")
		}
		if _, duplicate := expected[name]; duplicate {
			return nil, errors.New("duplicate APT method package-list entry")
		}
		if _, alias := aliases[name]; alias {
			if hashes[strings.TrimPrefix(line, "/")] != "" {
				return nil, errors.New("APT alias unexpectedly has regular-file metadata")
			}
			expected[name] = "symlink"
		} else if _, approved := regular[name]; approved {
			value := hashes[strings.TrimPrefix(line, "/")]
			if value == "" {
				return nil, errors.New("APT method is absent from protected integrity metadata")
			}
			expected[name] = value
		} else {
			return nil, fmt.Errorf("APT package contains an unreviewed method name: %s", name)
		}
	}
	for name, required := range regular {
		if required && expected[name] == "" {
			return nil, errors.New("required vendor APT method is missing")
		}
	}
	for path := range hashes {
		if strings.HasPrefix(path, "usr/lib/apt/methods/") && expected[strings.TrimPrefix(path, "usr/lib/apt/methods/")] == "" {
			return nil, errors.New("APT method integrity/list metadata disagree")
		}
	}
	for name := range aliases {
		if expected[name] == "" {
			return nil, errors.New("required vendor APT alias is missing")
		}
	}
	return expected, nil
}
