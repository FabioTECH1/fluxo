package maintenancetask

import (
	"strings"
	"testing"
)

// Public stock Resolute defaults, collected under the fixed maintenance environment.
const resoluteGeneratedConfiguration = `APT::Solver "3.0";
APT::Compressor "";
APT::Compressor::. "";
APT::Compressor::.::Name ".";
APT::Compressor::.::Extension "";
APT::Compressor::.::Binary "";
APT::Compressor::.::Cost "0";
APT::Compressor::zstd "";
APT::Compressor::zstd::Name "zstd";
APT::Compressor::zstd::Extension ".zst";
APT::Compressor::zstd::Binary "zstd";
APT::Compressor::zstd::Cost "60";
APT::Compressor::zstd::CompressArg "";
APT::Compressor::zstd::CompressArg:: "-6";
APT::Compressor::zstd::UncompressArg "";
APT::Compressor::zstd::UncompressArg:: "-d";
APT::Compressor::lz4 "";
APT::Compressor::lz4::Name "lz4";
APT::Compressor::lz4::Extension ".lz4";
APT::Compressor::lz4::Binary "false";
APT::Compressor::lz4::Cost "50";
APT::Compressor::gzip "";
APT::Compressor::gzip::Name "gzip";
APT::Compressor::gzip::Extension ".gz";
APT::Compressor::gzip::Binary "gzip";
APT::Compressor::gzip::Cost "100";
APT::Compressor::gzip::CompressArg "";
APT::Compressor::gzip::CompressArg:: "-6n";
APT::Compressor::gzip::UncompressArg "";
APT::Compressor::gzip::UncompressArg:: "-d";
APT::Compressor::xz "";
APT::Compressor::xz::Name "xz";
APT::Compressor::xz::Extension ".xz";
APT::Compressor::xz::Binary "xz";
APT::Compressor::xz::Cost "200";
APT::Compressor::xz::CompressArg "";
APT::Compressor::xz::CompressArg:: "-6";
APT::Compressor::xz::UncompressArg "";
APT::Compressor::xz::UncompressArg:: "-d";
APT::Compressor::bzip2 "";
APT::Compressor::bzip2::Name "bzip2";
APT::Compressor::bzip2::Extension ".bz2";
APT::Compressor::bzip2::Binary "false";
APT::Compressor::bzip2::Cost "300";
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
Dir "/";
Dir::State "var/lib/apt";
Dir::State::lists "lists/";
Dir::State::cdroms "cdroms.list";
Dir::State::extended_states "extended_states";
Dir::State::status "/var/lib/dpkg/status";
Dir::Cache "var/cache/apt";
Dir::Cache::archives "archives/";
Dir::Cache::srcpkgcache "srcpkgcache.bin";
Dir::Cache::pkgcache "pkgcache.bin";
Dir::Etc "etc/apt";
Dir::Etc::sourcelist "sources.list";
Dir::Etc::sourceparts "sources.list.d";
Dir::Etc::main "apt.conf";
Dir::Etc::netrc "auth.conf";
Dir::Etc::netrcparts "auth.conf.d";
Dir::Etc::parts "apt.conf.d";
Dir::Etc::preferences "preferences";
Dir::Etc::preferencesparts "preferences.d";
Dir::Etc::trustedparts "trusted.gpg.d";
Dir::Boot "boot";
Dir::Usr "usr";
Dir::Bin "";
Dir::Bin::methods "/usr/lib/apt/methods";
Dir::Bin::solvers "";
Dir::Bin::solvers:: "/usr/lib/apt/solvers";
Dir::Bin::planners "";
Dir::Bin::planners:: "/usr/lib/apt/planners";
Dir::Bin::dpkg "/usr/bin/dpkg";
Dir::Bin::gzip "/bin/gzip";
Dir::Bin::bzip2 "/bin/bzip2";
Dir::Bin::xz "/usr/bin/xz";
Dir::Bin::lz4 "/usr/bin/lz4";
Dir::Bin::zstd "/usr/bin/zstd";
Dir::Bin::lzma "/usr/bin/xz";
Dir::Media "";
Dir::Media::MountPath "/media/apt";
Dir::Log "var/log/apt";
Dir::Log::Terminal "term.log";
Dir::Log::History "history.log";
Dir::Log::Planner "eipp.log.xz";
Dir::Ignore-Files-Silently "";
Dir::Ignore-Files-Silently:: "~$";
Dir::Ignore-Files-Silently:: "\.disabled$";
Dir::Ignore-Files-Silently:: "\.bak$";
Dir::Ignore-Files-Silently:: "\.dpkg-[a-z]+$";
Dir::Ignore-Files-Silently:: "\.ucf-[a-z]+$";
Dir::Ignore-Files-Silently:: "\.save$";
Dir::Ignore-Files-Silently:: "\.orig$";
Dir::Ignore-Files-Silently:: "\.distUpgrade$";
binary "apt-config";
Version "";
Version::1.2 "";
Version::1.2::APT "";
Version::1.2::APT::Color "1";
Version::1.2::APT::Keep-Downloaded-Packages "0";
Version::1.2::DPkg "";
Version::1.2::DPkg::Progress-Fancy "1";
Version::3.0 "";
Version::3.0::APT "";
Version::3.0::APT::Output-Version "30";
Version::3.0::Pager "1";
Version::1.1 "";
Version::1.1::APT "";
Version::1.1::APT::Cache "";
Version::1.1::APT::Cache::Show "";
Version::1.1::APT::Cache::Show::Version "2";
Version::1.1::APT::Cache::AllVersions "0";
Version::1.1::APT::Cache::ShowVirtuals "1";
Version::1.1::APT::Cache::Search "";
Version::1.1::APT::Cache::Search::Version "2";
Version::1.1::APT::Cache::ShowDependencyType "1";
Version::1.1::APT::Cache::ShowVersion "1";
Version::1.1::APT::Get "";
Version::1.1::APT::Get::Upgrade-Allow-New "1";
Version::1.1::APT::Cmd "";
Version::1.1::APT::Cmd::Show-Update-Stats "1";
Version::1.5 "";
Version::1.5::APT "";
Version::1.5::APT::Get "";
Version::1.5::APT::Get::Update "";
Version::1.5::APT::Get::Update::InteractiveReleaseInfoChanges "1";
Version::2.0 "";
Version::2.0::APT "";
Version::2.0::APT::Cmd "";
Version::2.0::APT::Cmd::Pattern-Only "1";
Version::2.0::Dpkg "";
Version::2.0::Dpkg::Lock "";
Version::2.0::Dpkg::Lock::Timeout "120";
Version::0.31 "";
Version::1.21 "";
Version::1.21::APT "";
Version::1.21::APT::Solver "3.0";
Version::2.11 "";
Version::2.11::APT "";
Version::2.11::APT::Solver "3.0";
Version::3.1 "";
Version::3.1::APT "";
Version::3.1::APT::Solver "3.0";
`

func TestAPTResoluteGeneratedDefaults(t *testing.T) {
	for _, version := range []string{"3.2.0", "3.2.1ubuntu0.1"} {
		t.Run(version, func(t *testing.T) {
			p := aptPlatformSpec{aptVersion: version}
			if err := validateAPTConfigForPlatform(resoluteGeneratedConfiguration, p); err != nil {
				t.Fatal(err)
			}
			for _, change := range []string{
				strings.Replace(resoluteGeneratedConfiguration, `Dir::Boot "boot";`, `Dir::Boot "/tmp";`, 1),
				strings.Replace(resoluteGeneratedConfiguration, `Dir::Usr "usr";`, `Dir::Usr "/tmp";`, 1),
				strings.Replace(resoluteGeneratedConfiguration, `Version::3.1::APT::Solver "3.0";`, `Version::3.1::APT::Solver "external";`, 1),
				resoluteGeneratedConfiguration + `Version::9::Dir::Cache "/tmp";`,
			} {
				if change == resoluteGeneratedConfiguration {
					t.Fatal("bad negative fixture")
				}
				if validateAPTConfigForPlatform(change, p) == nil {
					t.Fatal("custom conditional/path accepted")
				}
			}
		})
	}
	if validateAPTConfig(resoluteGeneratedConfiguration) == nil {
		t.Fatal("3.2 defaults leaked into 2.8")
	}
}
