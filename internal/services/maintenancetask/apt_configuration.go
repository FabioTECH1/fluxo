package maintenancetask

import (
	"errors"
	"fmt"
	"strings"
)

type aptSetting struct{ key, value string }

// Only these source-reviewed versions share the APT 3 configuration contract.
// A future 3.x version still requires an explicit review and exact package pin.
func reviewedAPT3Configuration(version string) bool {
	return version == "3.2.0" || version == "3.2.1ubuntu0.1"
}

// apt-config dump uses APT's own quoting, not JSON/Go escaping. Preserve literal
// backslashes in regexes and compressor arguments and retain list order.
func parseAPTDump(out string) ([]aptSetting, error) {
	var settings []aptSetting
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, rest, ok := strings.Cut(line, " ")
		encoded := strings.TrimSpace(rest)
		if !ok || len(encoded) < 3 || !strings.HasPrefix(encoded, "\"") || !strings.HasSuffix(encoded, "\";") {
			return nil, errors.New("unsupported APT configuration serialization")
		}
		settings = append(settings, aptSetting{strings.ToLower(key), encoded[1 : len(encoded)-2]})
	}
	return settings, nil
}

// The existing A0 profile accepts standard filesystem/executable selection.
// This is a conservative compatibility check, not a general APT sandbox. The
// isolated candidate below is deliberately separate from current authority.
func validateAPTConfig(out string) error {
	return validateAPTConfigForPlatform(out, aptPlatformSpec{aptVersion: "2.8.3"})
}

func validateAPTConfigForPlatform(out string, platform aptPlatformSpec) error {
	settings, err := parseAPTDump(out)
	if err != nil {
		return err
	}
	compressors := map[string][]string{}
	for _, setting := range settings {
		key, value := setting.key, setting.value
		// apt-get promotes its own binary subtree after apt-config's dump.
		// Other program subtrees (notably Ubuntu's Binary::apt JSON hooks) are
		// not promoted by apt-get and do not grant it execution authority.
		if strings.HasPrefix(key, "binary::apt-get::") {
			key = strings.TrimPrefix(key, "binary::apt-get::")
		} else if strings.HasPrefix(key, "binary::") {
			continue
		}
		// APT's command-line initializer overwrites Binary from argv[0]. The
		// inspected program is fixed to apt-config; apt-get's promoted subtree
		// is checked separately above, rather than inferred from this label.
		if key == "binary" && value != "" && value != "apt-config" || ((strings.HasPrefix(key, "version::") || key == "version") && !standardAPTVersionSetting(key, value, platform)) {
			return fmt.Errorf("conditional APT configuration needs maintenance review: %s", setting.key)
		}
		if strings.HasPrefix(key, "aptcli::hooks") && value != "" {
			return fmt.Errorf("APT JSON hook needs maintenance review: %s", setting.key)
		}
		if strings.Contains(key, "invoke") || strings.Contains(key, "pre-install-pkgs") {
			if value == "" {
				continue
			}
			// These hooks are unreachable through the qualified empty transaction
			// and its failing first dpkg Pre-Invoke guard. Earlier install/JSON
			// hooks are refused, including their promoted apt-get forms.
			if strings.HasPrefix(key, "apt::update::") || strings.HasPrefix(key, "dpkg::post-invoke") || strings.HasPrefix(key, "dpkg::pre-install-pkgs") {
				continue
			}
			return fmt.Errorf("APT hook needs maintenance review: %s", setting.key)
		}
		if key == "rootdir" || key == "dpkg::chroot-directory" {
			if value != "" && value != "/" {
				return fmt.Errorf("APT alternate root needs maintenance review: %s", setting.key)
			}
		}
		if key == "dpkg::path" && value != "/usr/sbin:/usr/bin:/sbin:/bin" && value != "" {
			return errors.New("APT command search path needs maintenance review")
		}
		if key == "apt::sandbox::user" && value != "_apt" {
			return errors.New("APT sandbox user needs maintenance review")
		}
		if key == "debug::nodropprivs" && value != "" && value != "0" && value != "false" && value != "no" && value != "off" {
			return errors.New("APT privilege-drop override needs maintenance review")
		}
		if key == "dir" || strings.HasPrefix(key, "dir::") {
			if err := standardAPTPathForPlatform(key, value, platform); err != nil {
				return fmt.Errorf("%s: %w", setting.key, err)
			}
		}
		if key == "apt::solver" || key == "apt::planner" {
			// APT 3.2 implements the exact 3.0 selector in-process in
			// EDSP::ResolveExternal. This inspection allowance does not
			// replace its independent platform/architecture admission gate.
			stockSolver3 := key == "apt::solver" && value == "3.0" && reviewedAPT3Configuration(platform.aptVersion)
			if value != "internal" && value != "" && !stockSolver3 {
				return errors.New("APT planning selection needs maintenance review")
			}
		} else if (strings.HasPrefix(key, "apt::solver::") || strings.HasPrefix(key, "apt::planner::")) && value != "" {
			return fmt.Errorf("APT planning override needs maintenance review: %s", setting.key)
		}
		if key == "apt::compressor" || strings.HasPrefix(key, "apt::compressor::") {
			compressors[key] = append(compressors[key], value)
		}
	}
	return validateStandardAPTCompressorsForPlatform(compressors, platform)
}

func standardAPTPath(key, value string) error {
	return standardAPTPathForPlatform(key, value, aptPlatformSpec{aptVersion: "2.8.3"})
}

func standardAPTPathForPlatform(key, value string, platform aptPlatformSpec) error {
	base := strings.TrimSuffix(key, "::")
	// Resolute adds read-only disk-space probes for boot/kernel artifacts and usr.
	// Source: apt3.2 init.cc139–140, depcache.cc2633, private-install.cc416.
	if reviewedAPT3Configuration(platform.aptVersion) && ((base == "dir::boot" && (value == "boot" || value == "/boot")) || (base == "dir::usr" && (value == "usr" || value == "/usr"))) {
		return nil
	}
	if value == "" {
		// Empty path scalars can inherit a different parent; they are not all
		// harmless containers. Only these known containers/cache-disable values
		// have an empty form in the accepted profile.
		switch key {
		case "dir::bin", "dir::media", "dir::ignore-files-silently", "dir::bin::solvers", "dir::bin::planners", "dir::cache::pkgcache", "dir::cache::srcpkgcache":
			return nil
		default:
			return errors.New("empty APT filesystem override needs maintenance review")
		}
	}
	paths := map[string][]string{
		"dir": {"/"}, "dir::state": {"var/lib/apt", "/var/lib/apt"},
		"dir::state::lists": {"lists", "/var/lib/apt/lists"}, "dir::state::cdroms": {"cdroms.list", "/var/lib/apt/cdroms.list"},
		"dir::state::extended_states": {"extended_states", "/var/lib/apt/extended_states"}, "dir::state::status": {"/var/lib/dpkg/status"},
		"dir::cache": {"var/cache/apt", "/var/cache/apt"}, "dir::cache::archives": {"archives", "/var/cache/apt/archives"},
		"dir::cache::srcpkgcache": {"srcpkgcache.bin", "/var/cache/apt/srcpkgcache.bin"}, "dir::cache::pkgcache": {"pkgcache.bin", "/var/cache/apt/pkgcache.bin"},
		"dir::etc": {"etc/apt", "/etc/apt"}, "dir::etc::sourcelist": {"sources.list", "/etc/apt/sources.list"},
		"dir::etc::sourceparts": {"sources.list.d", "/etc/apt/sources.list.d"}, "dir::etc::main": {"apt.conf", "/etc/apt/apt.conf"},
		"dir::etc::netrc": {"auth.conf", "/etc/apt/auth.conf"}, "dir::etc::netrcparts": {"auth.conf.d", "/etc/apt/auth.conf.d"},
		"dir::etc::parts": {"apt.conf.d", "/etc/apt/apt.conf.d"}, "dir::etc::preferences": {"preferences", "/etc/apt/preferences"},
		"dir::etc::preferencesparts": {"preferences.d", "/etc/apt/preferences.d"}, "dir::etc::trusted": {"trusted.gpg", "/etc/apt/trusted.gpg"},
		"dir::etc::trustedparts": {"trusted.gpg.d", "/etc/apt/trusted.gpg.d"},
		"dir::bin::methods":      {"/usr/lib/apt/methods"}, "dir::bin::solvers": {"/usr/lib/apt/solvers"}, "dir::bin::planners": {"/usr/lib/apt/planners"},
		"dir::bin::dpkg": {"/usr/bin/dpkg"}, "dir::bin::gzip": {"/bin/gzip"}, "dir::bin::bzip2": {"/bin/bzip2"},
		"dir::bin::xz": {"/usr/bin/xz"}, "dir::bin::lz4": {"/usr/bin/lz4"}, "dir::bin::zstd": {"/usr/bin/zstd"}, "dir::bin::lzma": {"/usr/bin/xz"},
		"dir::media::mountpath": {"/media/apt"}, "dir::log": {"var/log/apt", "/var/log/apt"},
		"dir::log::terminal": {"term.log", "/var/log/apt/term.log"}, "dir::log::history": {"history.log", "/var/log/apt/history.log"},
		"dir::log::planner": {"eipp.log.xz", "/var/log/apt/eipp.log.xz"},
	}
	if base == "dir::ignore-files-silently" {
		return nil // Patterns affect data/config enumeration, not executable paths.
	}
	for _, expected := range paths[base] {
		if strings.TrimSuffix(value, "/") == strings.TrimSuffix(expected, "/") {
			return nil
		}
	}
	return errors.New("APT filesystem or executable override needs maintenance review")
}

func validateStandardAPTCompressors(settings map[string][]string) error {
	return validateStandardAPTCompressorsForPlatform(settings, aptPlatformSpec{aptVersion: "2.8.3"})
}

func validateStandardAPTCompressorsForPlatform(settings map[string][]string, platform aptPlatformSpec) error {
	type compressor struct {
		extension, binary, cost string
		compress, uncompress    []string
	}
	standard := map[string]compressor{
		".": {"", "", "0", nil, nil}, "zstd": {".zst", "zstd", "60", []string{"-19"}, []string{"-d"}},
		"lz4": {".lz4", "false", "50", nil, nil}, "gzip": {".gz", "gzip", "100", []string{"-6n"}, []string{"-d"}},
		"xz": {".xz", "xz", "200", []string{"-6"}, []string{"-d"}}, "bzip2": {".bz2", "false", "300", nil, nil},
		"lzma": {".lzma", "xz", "400", []string{"--format=lzma", "-6"}, []string{"--format=lzma", "-d"}},
	}
	// apt 2.4/2.8/3.2 aptconfiguration.cc selects this exact data when the
	// standard bzip2 program is installed (for example by build-essential).
	// FileFd still selects its package-verified native Bz2 implementation by
	// name; inspectAPTNativeCodecs independently requires that linked backend.
	if binary := settings["apt::compressor::bzip2::binary"]; len(binary) == 1 && binary[0] == "bzip2" {
		standard["bzip2"] = compressor{".bz2", "bzip2", "300", []string{"-6"}, []string{"-d"}}
	}
	// The same reviewed APT sources advertise this exact tuple when the stock
	// lz4 program exists. FileFd selects its required linked LZ4 implementation
	// by compressor.Name before considering an external pipe; arbitrary paths
	// and arguments still fail this comparison and native-codec inspection.
	if binary := settings["apt::compressor::lz4::binary"]; len(binary) == 1 && binary[0] == "lz4" {
		standard["lz4"] = compressor{".lz4", "lz4", "50", []string{"-1"}, []string{"-d"}}
	}

	if reviewedAPT3Configuration(platform.aptVersion) {
		// Resolute changes the default zstd compression level. Candidate inspection
		// recognizes this data; it does not authorize solver-3 execution.
		c := standard["zstd"]
		c.compress = []string{"-6"}
		standard["zstd"] = c
	}
	for key, values := range settings {
		if key == "apt::compressor" {
			if len(values) != 1 || values[0] != "" {
				return errors.New("custom APT compressor selection needs maintenance review")
			}
			continue
		}
		rest := strings.TrimPrefix(key, "apt::compressor::")
		name, field, hasField := strings.Cut(rest, "::")
		profile, known := standard[name]
		if !known {
			return fmt.Errorf("APT compressor needs maintenance review: %s", key)
		}
		var expected []string
		switch field {
		case "":
			if hasField {
				return fmt.Errorf("unsupported APT compressor list: %s", key)
			}
			expected = []string{""}
		case "name":
			expected = []string{name}
		case "extension":
			expected = []string{profile.extension}
		case "binary":
			expected = []string{profile.binary}
		case "cost":
			expected = []string{profile.cost}
		case "compressarg", "uncompressarg":
			expected = []string{""}
		case "compressarg::":
			expected = profile.compress
		case "uncompressarg::":
			expected = profile.uncompress
		default:
			return fmt.Errorf("APT compressor option needs maintenance review: %s", key)
		}
		if strings.Join(values, "\x00") != strings.Join(expected, "\x00") || len(values) != len(expected) {
			return fmt.Errorf("APT compressor implementation needs maintenance review: %s", key)
		}
	}
	for name, profile := range standard {
		base := "apt::compressor::" + name
		if _, exists := settings[base]; !exists {
			continue
		}
		for field, expected := range map[string][]string{"name": {name}, "extension": {profile.extension}, "binary": {profile.binary}, "cost": {profile.cost}, "compressarg::": profile.compress, "uncompressarg::": profile.uncompress} {
			got := settings[base+"::"+field]
			if len(got) != len(expected) || strings.Join(got, "\x00") != strings.Join(expected, "\x00") {
				return fmt.Errorf("incomplete APT compressor contract needs maintenance review: %s", base+"::"+field)
			}
		}
	}
	return nil
}

// APT 3.2 BinarySpecificConfiguration generates these exact compatibility
// defaults (private-cmndline.cc503–532). Raw host inputs remain independently
// closed, so this is not permission for custom conditional configuration.
func standardAPTVersionSetting(key, value string, platform aptPlatformSpec) bool {
	if !reviewedAPT3Configuration(platform.aptVersion) {
		return value == ""
	}
	defaults := map[string]string{
		"version::1.2::apt::color":                                      "1",
		"version::3.0::apt::output-version":                             "30",
		"version::1.1::apt::cache::show::version":                       "2",
		"version::1.1::apt::cache::allversions":                         "0",
		"version::1.1::apt::cache::showvirtuals":                        "1",
		"version::1.1::apt::cache::search::version":                     "2",
		"version::1.1::apt::cache::showdependencytype":                  "1",
		"version::1.1::apt::cache::showversion":                         "1",
		"version::1.1::apt::get::upgrade-allow-new":                     "1",
		"version::1.1::apt::cmd::show-update-stats":                     "1",
		"version::1.2::dpkg::progress-fancy":                            "1",
		"version::1.2::apt::keep-downloaded-packages":                   "0",
		"version::1.5::apt::get::update::interactivereleaseinfochanges": "1",
		"version::2.0::apt::cmd::pattern-only":                          "1",
		"version::3.0::pager":                                           "1",
		"version::0.31::apt::solver":                                    "3.0",
		"version::1.21::apt::solver":                                    "3.0",
		"version::2.11::apt::solver":                                    "3.0",
		"version::3.1::apt::solver":                                     "3.0",
		"version::2.0::dpkg::lock::timeout":                             "120",
	}
	if expected, ok := defaults[key]; ok {
		return value == expected
	}
	if value != "" {
		return false
	}
	for leaf := range defaults {
		if strings.HasPrefix(leaf, key+"::") {
			return true
		}
	}
	return false
}
