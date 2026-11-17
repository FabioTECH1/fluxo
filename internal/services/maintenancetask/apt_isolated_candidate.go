package maintenancetask

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
)

const aptIsolatedCandidateRevision = "apt-a0-isolated-candidate-v1"

// aptIsolatedCandidate is a specification, deliberately unused by production.
// It creates no files, selects no supported APT build and grants no execution
// authority. Activation still needs immutable artifacts, native compression and
// command-line qualification, host-intent review, and helper/profile migration.
type aptIsolatedCandidate struct {
	Revision    string
	ConfigPath  string
	Files       map[string][]byte
	Directories []string
	Environment []string
	FileMode    fs.FileMode
	DirMode     fs.FileMode
}

func newAPTIsolatedCandidate(root string) (aptIsolatedCandidate, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !strings.HasPrefix(root, stateRoot+"/apt-profiles/") || strings.ContainsAny(root, "\"'\\ \t\r\n\x00;$") {
		return aptIsolatedCandidate{}, errors.New("unsafe isolated APT candidate directory")
	}
	// A future publisher must create/verify these artifacts under nonsymlink,
	// root-owned, non-writable ancestors. It must refuse missing files before
	// invoking APT: APT itself may merely warn about an absent APT_CONFIG.
	configPath := filepath.Join(root, "apt.conf")
	mainPath, partsPath := filepath.Join(root, "main.conf"), filepath.Join(root, "parts")
	text := `// Disabled Fluxo A0 candidate; never load host executable configuration.
Dir "/";
Dir::Etc "/etc/apt";
Dir::Etc::main "` + mainPath + `";
Dir::Etc::parts "` + partsPath + `";
Dir::Etc::sourcelist "/etc/apt/sources.list";
Dir::Etc::sourceparts "/etc/apt/sources.list.d";
Dir::Etc::preferences "/etc/apt/preferences";
Dir::Etc::preferencesparts "/etc/apt/preferences.d";
Dir::Etc::netrc "/etc/apt/auth.conf";
Dir::Etc::netrcparts "/etc/apt/auth.conf.d";
Dir::Etc::trusted "/etc/apt/trusted.gpg";
Dir::Etc::trustedparts "/etc/apt/trusted.gpg.d";
Dir::State "/var/lib/apt";
Dir::State::status "/var/lib/dpkg/status";
Dir::State::extended_states "/var/lib/apt/extended_states";
Dir::State::lists "/var/lib/apt/lists";
Dir::Cache "/var/cache/apt";
Dir::Cache::archives "/var/cache/apt/archives";
Dir::Cache::pkgcache "` + filepath.Join(root, "cache", "pkgcache.bin") + `";
Dir::Cache::srcpkgcache "` + filepath.Join(root, "cache", "srcpkgcache.bin") + `";
Dir::Log "/var/log/apt";
Dir::Log::Terminal "term.log";
Dir::Log::History "history.log";
Dir::Log::Planner "eipp.log.xz";
APT::Solver "internal";
APT::Solver::Strict-Pinning "true";
APT::Planner "internal";
DPkg::Chroot-Directory "/";
DPkg::Path "/usr/sbin:/usr/bin:/sbin:/bin";
`
	// APT recreates standard compressors even without host configuration. Its
	// literal "false" fallback is an internal refusal before execvp; this is
	// not a promise that a native backend exists. Each retained input format
	// must be natively tested and unsupported formats must fail closed.
	for _, name := range []string{"zstd", "lz4", "gzip", "xz", "bzip2", "lzma"} {
		text += "APT::Compressor::" + name + "::Binary \"false\";\n"
	}
	return aptIsolatedCandidate{
		Revision: aptIsolatedCandidateRevision, ConfigPath: configPath,
		Files:       map[string][]byte{configPath: []byte(text), mainPath: {}},
		Directories: []string{root, partsPath, filepath.Join(root, "cache")},
		// This is a complete environment, not an append-to-inherited contract.
		Environment: []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "APT_CONFIG=" + configPath},
		FileMode:    0600, DirMode: 0700,
	}, nil
}
