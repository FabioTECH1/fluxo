package maintenancetask

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/syscmd"
)

func TestAPTConfigurationExecutionBoundaries(t *testing.T) {
	// Stock apt-only hooks must not be mistaken for the apt-get execution
	// subtree. Its list-valued solver/planner search directories are valid.
	standard := `Dir "/";
Dir::State "var/lib/apt";
Dir::State::status "/var/lib/dpkg/status";
Dir::Cache "var/cache/apt";
Dir::Cache::archives "archives/";
Dir::Bin "";
Dir::Bin::solvers "";
Dir::Bin::solvers:: "/usr/lib/apt/solvers";
Dir::Bin::planners "";
Dir::Bin::planners:: "/usr/lib/apt/planners";
Binary::apt::AptCli::Hooks::Install:: "/usr/bin/snap advise-snap --from-apt";
APT::Solver "internal";
DPkg::Post-Invoke:: "package-owned-post-hook";
`
	if err := validateAPTConfig(standard); err != nil {
		t.Fatal(err)
	}
	for _, override := range []string{
		`AptCli::Hooks::Install:: "/root/hook";`,
		`Binary::apt-get::AptCli::Hooks::Install:: "/root/hook";`,
		`Binary::apt-get::APT::Solver "/root/solver";`,
		`Binary::apt-get::Dir::Bin::solvers:: "/root/solvers";`,
		`Binary::apt-get::DPkg::Pre-Invoke:: "/root/hook";`,
		`Version::9::Dir::Cache::archives "/root/archive";`,
		`Dir::Cache::archives "/root/archive";`,
		`Dir::Cache "";`,
		`Dir::State "";`,
		`RootDir "/root/new-root";`,
		`DPkg::Chroot-Directory "/root/new-root";`,
		`DPkg::Path "/root/bin:/usr/bin";`,
		`APT::Sandbox::User "root";`,
		`Debug::NoDropPrivs "true";`,
		`APT::Compressor::gzip::Binary "/root/decoder";`,
		`APT::Compressor::gzip::UncompressArg:: "--force";`,
		`APT::Compressor::custom::Binary "custom";`,
		`APT::Compressor:: "custom";`,
	} {
		t.Run(override, func(t *testing.T) {
			if err := validateAPTConfig(standard + override + "\n"); err == nil {
				t.Fatal("executable, conditional or deletion-path override accepted")
			}
		})
	}
}

func TestAPTCompressorArgumentsRemainOrderedAndComplete(t *testing.T) {
	stock := `APT::Compressor "";
APT::Compressor::lzma "";
APT::Compressor::lzma::Name "lzma";
APT::Compressor::lzma::Extension ".lzma";
APT::Compressor::lzma::Binary "xz";
APT::Compressor::lzma::Cost "400";
APT::Compressor::lzma::CompressArg "";
APT::Compressor::lzma::CompressArg:: "--format=lzma";
APT::Compressor::lzma::CompressArg:: "-6";
APT::Compressor::lzma::UncompressArg "";
APT::Compressor::lzma::UncompressArg:: "--format=lzma";
APT::Compressor::lzma::UncompressArg:: "-d";
`
	if err := validateAPTConfig(stock); err != nil {
		t.Fatal(err)
	}
	for _, changed := range []string{
		strings.Replace(stock, `CompressArg:: "--format=lzma";`+"\n"+`APT::Compressor::lzma::CompressArg:: "-6";`, `CompressArg:: "-6";`+"\n"+`APT::Compressor::lzma::CompressArg:: "--format=lzma";`, 1),
		strings.ReplaceAll(stock, `APT::Compressor::lzma::UncompressArg:: "-d";`, ""),
		stock + `APT::Compressor::lzma::Binary "xz";`,
	} {
		if changed == stock {
			t.Fatal("invalid mutation fixture")
		}
		if err := validateAPTConfig(changed); err == nil {
			t.Fatal("changed compression contract accepted")
		}
	}
}

func TestAPTStockLZ4ProgramDoesNotAdmitCustomDecoder(t *testing.T) {
	stock := `APT::Compressor "";
APT::Compressor::lz4 "";
APT::Compressor::lz4::Name "lz4";
APT::Compressor::lz4::Extension ".lz4";
APT::Compressor::lz4::Binary "lz4";
APT::Compressor::lz4::Cost "50";
APT::Compressor::lz4::CompressArg "";
APT::Compressor::lz4::CompressArg:: "-1";
APT::Compressor::lz4::UncompressArg "";
APT::Compressor::lz4::UncompressArg:: "-d";
`
	for _, version := range []string{"2.4.15ubuntu0.1", "2.8.4ubuntu0.1", "3.2.1ubuntu0.1"} {
		if err := validateAPTConfigForPlatform(stock, aptPlatformSpec{aptVersion: version}); err != nil {
			t.Fatal("packaged lz4 tuple refused", version, err)
		}
	}
	for _, changed := range []string{
		strings.Replace(stock, `Binary "lz4"`, `Binary "/tmp/decoder"`, 1),
		strings.Replace(stock, `CompressArg:: "-1"`, `CompressArg:: "-9"`, 1),
		strings.Replace(stock, `UncompressArg:: "-d"`, `UncompressArg:: "--force"`, 1),
		strings.Replace(stock, `Name "lz4"`, `Name "external"`, 1),
		strings.Replace(stock, `APT::Compressor::lz4::UncompressArg:: "-d";`, "", 1),
		stock + `APT::Compressor::lz4::Binary "lz4";`,
	} {
		if err := validateAPTConfig(changed); err == nil {
			t.Fatal("modified or incomplete lz4 decoder accepted")
		}
	}
}

func TestIsolatedAPTSpecificationHasNoExecutionAuthority(t *testing.T) {
	root := stateRoot + "/apt-profiles/candidate-review"
	p, err := newAPTIsolatedCandidate(root)
	if err != nil {
		t.Fatal(err)
	}
	if p.Revision != aptIsolatedCandidateRevision || len(p.Environment) != 5 || p.Environment[4] != "APT_CONFIG="+p.ConfigPath || p.FileMode != 0600 || p.DirMode != 0700 {
		t.Fatal("incomplete isolated environment")
	}
	config := string(p.Files[p.ConfigPath])
	if strings.Contains(config, `RootDir "/";`) || strings.Contains(config, `Dir::Cache::pkgcache "";`) || strings.Contains(config, `Dir::Cache::srcpkgcache "";`) {
		t.Fatal("isolated candidate can resolve a disabled cache to host root")
	}
	for _, required := range []string{`Dir::Etc::main "` + root + `/main.conf";`, `Dir::Etc::parts "` + root + `/parts";`, `Dir::State::status "/var/lib/dpkg/status";`, `Dir::Etc::sourceparts "/etc/apt/sources.list.d";`, `APT::Solver::Strict-Pinning "true";`, `APT::Compressor::gzip::Binary "false";`} {
		if !strings.Contains(config, required) {
			t.Fatalf("missing isolated boundary %s", required)
		}
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("specification generated host artifacts")
	}
	for _, unsafe := range []string{"/tmp/apt", stateRoot + "/apt-profiles/../escape", stateRoot + "/apt-profiles/quote\"", stateRoot + "/apt-profiles/a\ninclude"} {
		if _, err := newAPTIsolatedCandidate(unsafe); err == nil {
			t.Fatal("unsafe profile path accepted", unsafe)
		}
	}
}

func TestVMAPTStandardConfigurationAndLayout(t *testing.T) {
	if !aptProfileVMGuard() {
		t.Skip("explicit root opt-in on retained acceptance VM only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	p := newProfile(PackageCleanup)
	platform, err := inspectCleanupPlatform(ctx, &p)
	if err != nil {
		t.Fatal(err)
	}
	if platform.executionQualified {
		if err = qualifyCleanup(ctx, &p); err != nil {
			t.Fatal(err)
		}
	} else if err = qualifyCleanup(ctx, &p); !errors.Is(err, errAPTCandidateProfile) {
		t.Fatal("candidate execution gate missing", err)
	}
	t.Logf("read-only Ubuntu %s APT %s evidence passed; execution qualified=%v", platform.release, platform.aptVersion, platform.executionQualified)
}

func TestVMAPTPartialRoleOwnershipTransitions(t *testing.T) {
	if !aptProfileVMGuard() {
		t.Skip("explicit root opt-in on retained acceptance VM only")
	}
	credential, err := syscmd.ResolveCredential("_apt")
	if err != nil || credential.Uid == 0 {
		t.Fatal("missing sandbox identity", err)
	}
	dir, err := os.MkdirTemp("/root", "fluxo-apt-private-leaf-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	leaf := filepath.Join(dir, "partial")
	if err = os.Mkdir(leaf, 0700); err != nil {
		t.Fatal(err)
	}
	before := newProfile(PackageCleanup)
	if err = recordAPTPartialDirectory(&before, leaf, credential.Uid); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(leaf, int(credential.Uid), 0); err != nil {
		t.Fatal(err)
	}
	after := newProfile(PackageCleanup)
	if err = recordAPTPartialDirectory(&after, leaf, credential.Uid); err != nil {
		t.Fatal(err)
	}
	if !compatibleProfiles(before, after, false, false) {
		t.Fatal("safe root-to-_apt private-data transition changed execution authority")
	}
	if err = os.Chmod(leaf, 0755); err != nil {
		t.Fatal(err)
	}
	if err = recordAPTPartialDirectory(&after, leaf, credential.Uid); err == nil {
		t.Fatal("nonprivate partial directory accepted")
	}
	if err = os.Remove(leaf); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(dir, leaf); err != nil {
		t.Fatal(err)
	}
	if err = recordAPTPartialDirectory(&after, leaf, credential.Uid); err == nil {
		t.Fatal("symlinked deletion target accepted")
	}
}

func aptProfileVMGuard() bool {
	host, _ := os.Hostname()
	if os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		return false
	}
	if arm64NativeVMHost() {
		return true
	}
	switch host {
	case "fluxo-php-patch-test", "fluxo-bridge-prod-20261101", "fluxo-bridge-noble-final-20261102", "fluxo-bridge-jammy-20261101", "fluxo-bridge-resolute-20261101":
		return true
	}
	return false
}

func TestAPTSolver3SelectionIsExactAndVersioned(t *testing.T) {
	for _, version := range []string{"2.4.14", "2.8.3", "3.2.0", "2.4.15ubuntu0.1", "2.8.4ubuntu0.1", "3.2.1ubuntu0.1", "3.2.2"} {
		for _, solver := range []string{"internal", "3.0", "3.1", "3.9", "external"} {
			err := validateAPTConfigForPlatform(`APT::Solver "`+solver+`";`, aptPlatformSpec{aptVersion: version})
			want := solver == "internal" || ((version == "3.2.0" || version == "3.2.1ubuntu0.1") && solver == "3.0")
			if (err == nil) != want {
				t.Fatalf("version=%s solver=%s error=%v", version, solver, err)
			}
		}
		if validateAPTConfigForPlatform(`APT::Planner "3.0";`, aptPlatformSpec{aptVersion: version}) == nil {
			t.Fatal("solver allowance leaked into planner")
		}
	}
}

func TestAPTInstalledBzip2CompressorRequiresCompleteStockRecord(t *testing.T) {
	const record = `APT::Compressor::bzip2 "";
APT::Compressor::bzip2::Name "bzip2";
APT::Compressor::bzip2::Extension ".bz2";
APT::Compressor::bzip2::Binary "bzip2";
APT::Compressor::bzip2::Cost "300";
APT::Compressor::bzip2::CompressArg "";
APT::Compressor::bzip2::CompressArg:: "-6";
APT::Compressor::bzip2::UncompressArg "";
APT::Compressor::bzip2::UncompressArg:: "-d";`
	for _, version := range []string{"2.4.15ubuntu0.1", "2.8.4ubuntu0.1", "3.2.1ubuntu0.1"} {
		if err := validateAPTConfigForPlatform(record, aptPlatformSpec{aptVersion: version}); err != nil {
			t.Fatal("stock installed bzip2 record refused", version, err)
		}
	}
	for _, invalid := range []string{
		strings.Replace(record, `Binary "bzip2"`, `Binary "/tmp/bzip2"`, 1),
		strings.Replace(record, `"-6"`, `"--help"`, 1),
		strings.Replace(record, `::UncompressArg:: "-d";`, `::UncompressArg:: "-dc";`, 1),
		strings.Replace(record, `::UncompressArg:: "-d";`, `::UncompressArg:: "-d";`+"\n"+`APT::Compressor::bzip2::UncompressArg:: "--help";`, 1),
		strings.Replace(record, `APT::Compressor::bzip2::CompressArg:: "-6";`, "", 1),
		strings.Replace(record, `Binary "bzip2"`, `Binary "false"`, 1),
	} {
		if validateAPTConfig(invalid) == nil {
			t.Fatal("modified or mixed bzip2 record accepted")
		}
	}
}
