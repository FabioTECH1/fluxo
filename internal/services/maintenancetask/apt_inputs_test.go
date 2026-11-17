package maintenancetask

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/syscmd"
)

// Exact public default input bytes from the reviewed Noble fixture. Keeping
// these independent bytes exercises the production digest approvals.
const aptInputVendorFixture = `Acquire::Changelogs::AlwaysOnline "true";
Acquire::http::User-Agent-Non-Interactive "true";
`

func TestAPTPlatformPostgresClusterProtectionHasExactOptionalBytes(t *testing.T) {
	const template = `// DO NOT EDIT!
// File maintained by /usr/share/postgresql-common/pg_updateaptconfig.
//
// Mark all PostgreSQL packages as NeverAutoRemove for which PostgreSQL
// clusters exist. This is especially important when the "postgresql" meta
// package changes its dependencies to a new version, which might otherwise
// trigger the old postgresql-NN package to be automatically removed, rendering
// the old database cluster inaccessible.

APT
{
  NeverAutoRemove
  {
    "^postgresql.*-%d";
  };
};
`
	for _, test := range []struct {
		version int
		inputs  map[string]aptConfigInput
	}{{14, jammyAPTConfigInputs}, {16, nobleAPTConfigInputs}, {18, resoluteAPTConfigInputs}} {
		approved := test.inputs["02autoremove-postgresql"]
		raw := []byte(fmt.Sprintf(template, test.version))
		if approved.required || approved.sha256 != digest(raw) {
			t.Fatalf("PostgreSQL %d optional input differs from reviewed bytes", test.version)
		}
	}
}

const aptInputAutoremoveFixture = `APT
{
  NeverAutoRemove
  {
	"^firmware-linux.*";
	"^linux-firmware$";
	"^linux-image-[a-z0-9]*$";
	"^linux-image-[a-z0-9]*-[a-z0-9]*$";
  };

  VersionedKernelPackages
  {
	# kernels
	"linux-.*";
	"kfreebsd-.*";
	"gnumach-.*";
	# (out-of-tree) modules
	".*-modules";
	".*-kernel";
  };

  Never-MarkAuto-Sections
  {
	"metapackages";
	"tasks";
  };

  Move-Autobit-Sections
  {
	"oldlibs";
  };
};
`

func aptMemoryInputSource(files map[string][]byte) aptInputSource {
	return aptInputSource{
		read: func(path string) ([]byte, error) {
			raw, ok := files[path]
			if !ok {
				return nil, os.ErrNotExist
			}
			return append([]byte(nil), raw...), nil
		},
		list: func(path string) ([]string, error) {
			if path != aptPartsConfig {
				return nil, errors.New("unexpected fixture directory")
			}
			var names []string
			for file := range files {
				if filepath.Dir(file) == path {
					names = append(names, filepath.Base(file))
				}
			}
			return names, nil
		},
	}
}

func aptReviewedFixture() map[string][]byte {
	return map[string][]byte{
		aptPartsConfig + "/01-vendor-ubuntu": []byte(aptInputVendorFixture),
		aptPartsConfig + "/01autoremove":     []byte(aptInputAutoremoveFixture),
	}
}

func TestAPTHostInputManifestRejectsMaskedConfigurationBeforeToolExecution(t *testing.T) {
	files := aptReviewedFixture()
	source := aptMemoryInputSource(files)
	manifest, err := inspectAPTHostInputs(source)
	if err != nil {
		t.Fatal(err)
	}
	p := newProfile(PackageCleanup)
	manifest.record(&p)
	if p.Files[aptMainConfig] != "absent" || p.External["apt-host-input-contract"] != aptHostInputsRevision {
		t.Fatal("incomplete manifest evidence")
	}
	calls := 0
	if err = manifest.verifyAndRun(source, func() error { calls++; return nil }); err != nil || calls != 1 {
		t.Fatal("reviewed inputs refused", err, calls)
	}
	// apt-config's promoted output can hide the global hook, so validating this
	// otherwise innocuous dump is deliberately insufficient authorization.
	maskedDump := "APT::Install::Pre-Invoke::probe \"\";\nBinary \"apt-config\";\n"
	if err = validateAPTConfig(maskedDump); err != nil {
		t.Fatal("masking reproduction is not a flat-dump bypass", err)
	}
	files[aptPartsConfig+"/99mask"] = []byte(`APT::Install::Pre-Invoke::probe "/root/unreviewed-helper";
Binary::apt-config::APT::Install::Pre-Invoke::probe "";
`)
	calls = 0
	if err = manifest.verifyAndRun(source, func() error { calls++; return nil }); err == nil || calls != 0 {
		t.Fatal("unreviewed raw configuration reached the APT subprocess", err, calls)
	}
}

func TestAPTHostInputManifestRefusesMutationIncludesAndIgnoredEntries(t *testing.T) {
	for name, mutate := range map[string]func(map[string][]byte){
		"main config": func(files map[string][]byte) { files[aptMainConfig] = []byte(`Dir::Bin::dpkg "/root/helper";`) },
		"nested include": func(files map[string][]byte) {
			files[aptPartsConfig+"/01autoremove"] = append([]byte(aptInputAutoremoveFixture), []byte("#include \"/root/extra.conf\";\n")...)
		},
		"modified approved name": func(files map[string][]byte) {
			files[aptPartsConfig+"/01-vendor-ubuntu"] = []byte(`Binary::apt-config::Dir::Bin::dpkg "/root/helper";`)
		},
		"ignored backup":     func(files map[string][]byte) { files[aptPartsConfig+"/01autoremove.dpkg-dist"] = []byte("backup") },
		"unknown empty file": func(files map[string][]byte) { files[aptPartsConfig+"/.keep"] = nil },
		"missing protection": func(files map[string][]byte) { delete(files, aptPartsConfig+"/01autoremove") },
	} {
		t.Run(name, func(t *testing.T) {
			files := aptReviewedFixture()
			source := aptMemoryInputSource(files)
			manifest, err := inspectAPTHostInputs(source)
			if err != nil {
				t.Fatal(err)
			}
			mutate(files)
			calls := 0
			if err = manifest.verifyAndRun(source, func() error { calls++; return nil }); err == nil || calls != 0 {
				t.Fatal("changed input reached a subprocess", err, calls)
			}
		})
	}
}

func TestAPTHostInputManifestBindsOptionalStateAndExactlyEmptyMainFile(t *testing.T) {
	files := aptReviewedFixture()
	files[aptMainConfig] = nil
	source := aptMemoryInputSource(files)
	manifest, err := inspectAPTHostInputs(source)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.files[aptMainConfig] != digest(nil) {
		t.Fatal("empty main file was not bound")
	}
	// Both variants are individually reviewed, but a change after capture must
	// still prevent executing under evidence for the old variant.
	delete(files, aptMainConfig)
	calls := 0
	if err = manifest.verifyAndRun(source, func() error { calls++; return nil }); err == nil || calls != 0 {
		t.Fatal("absent/empty transition ignored", err, calls)
	}
	files[aptMainConfig] = nil
	files[aptPartsConfig+"/20auto-upgrades"] = []byte("APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n")
	current, err := inspectAPTHostInputs(source)
	if err != nil {
		t.Fatal("reviewed optional snippet refused", err)
	}
	if !strings.HasPrefix(current.files[aptPartsConfig+"/20auto-upgrades"], "b81a3a") {
		t.Fatal("optional input not bound")
	}
	if err = manifest.verifyAndRun(source, func() error { calls++; return nil }); err == nil || calls != 0 {
		t.Fatal("optional membership changed without requalification", err, calls)
	}
}

func TestVMAPTHostInputFilesystemProtection(t *testing.T) {
	if !aptProfileVMGuard() {
		t.Skip("explicit root opt-in on retained acceptance VM only")
	}
	credential, err := syscmd.ResolveCredential("_apt")
	if err != nil || credential.Uid == 0 {
		t.Fatal("missing sandbox identity", err)
	}
	for _, kind := range []string{"symlink", "hardlink", "foreign-owner", "directory", "writable-parts"} {
		t.Run(kind, func(t *testing.T) {
			root, err := os.MkdirTemp("/root", "fluxo-apt-inputs-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			parts := filepath.Join(root, "apt.conf.d")
			if err = os.Mkdir(parts, 0700); err != nil {
				t.Fatal(err)
			}
			for logical, raw := range aptReviewedFixture() {
				if err = os.WriteFile(filepath.Join(parts, filepath.Base(logical)), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			source := diskAPTInputSourceAt(root)
			manifest, err := inspectAPTHostInputs(source)
			if err != nil {
				t.Fatal("protected private fixture refused", err)
			}
			target := filepath.Join(parts, "01autoremove")
			switch kind {
			case "symlink", "hardlink":
				outside := filepath.Join(root, "same-reviewed-bytes")
				if err = os.WriteFile(outside, []byte(aptInputAutoremoveFixture), 0600); err != nil {
					t.Fatal(err)
				}
				if err = os.Remove(target); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink" {
					err = os.Symlink(outside, target)
				} else {
					err = os.Link(outside, target)
				}
			case "foreign-owner":
				err = os.Chown(target, int(credential.Uid), 0)
			case "directory":
				if err = os.Remove(target); err != nil {
					t.Fatal(err)
				}
				err = os.Mkdir(target, 0700)
			case "writable-parts":
				err = os.Chmod(parts, 0775)
			}
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			if err = manifest.verifyAndRun(source, func() error { calls++; return nil }); err == nil || calls != 0 {
				t.Fatal("unsafe input reached an APT subprocess", err, calls)
			}
		})
	}
}
