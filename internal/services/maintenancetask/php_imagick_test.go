package maintenancetask

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestImagickRequiresReviewedNativeImplementation(t *testing.T) {
	libraries := []string{"libgomp.so.1", "libMagickWand-6.Q16.so.7", "libMagickCore-6.Q16.so.7", "libc.so.6"}
	for _, api := range []string{"20240924", "20250925"} {
		if err := inspectImagickELF(phpTestELF(api, libraries, ""), api); err != nil {
			t.Fatal(err)
		}
	}
	for _, scenario := range []string{"wrong-api", "missing-wand", "missing-core", "new-delegate-library", "wrong-magick-ABI", "no-entrypoint"} {
		t.Run(scenario, func(t *testing.T) {
			deps := append([]string(nil), libraries...)
			api := "20240924"
			switch scenario {
			case "wrong-api":
				api = "20250925"
			case "missing-wand":
				deps = append(deps[:1], deps[2:]...)
			case "missing-core":
				deps = append(deps[:2], deps[3:]...)
			case "new-delegate-library":
				deps = append(deps, "libcustomdelegate.so")
			case "wrong-magick-ABI":
				deps[1] = "libMagickWand-7.Q16HDRI.so.10"
			}
			raw := phpTestELF(api, deps, "")
			if scenario == "no-entrypoint" {
				raw = []byte(strings.ReplaceAll(string(raw), "get_module", "not_module"))
			}
			if inspectImagickELF(raw, "20240924") == nil {
				t.Fatal("unreviewed native implementation accepted")
			}
		})
	}
}

func TestImageMagickVendorConfigurationAndDrift(t *testing.T) {
	path := "/etc/ImageMagick-6/delegates.xml"
	raw := []byte(`<delegatemap><delegate command="vendor command"/></delegatemap>`)
	sum := md5.Sum(raw)
	manifest, err := addImageMagickConffiles(map[string]string{}, " "+path+" "+hex.EncodeToString(sum[:])+"\n")
	if err != nil {
		t.Fatal(err)
	}
	before := newProfile(ComposerUpdate)
	if err = bindImageMagickConfig(&before, path, raw, manifest); err != nil {
		t.Fatal(err)
	}
	if before.Files[path] != digest(raw) {
		t.Fatal("configuration not durably bound")
	}
	if bindImageMagickConfig(&before, path, append(raw, 'x'), manifest) == nil {
		t.Fatal("tampered conffile accepted")
	}
	if bindImageMagickConfig(&before, "/etc/ImageMagick-6/custom.xml", raw, manifest) == nil {
		t.Fatal("unowned config accepted")
	}
	for _, output := range []string{path + " bad", path + " " + hex.EncodeToString(sum[:]) + " obsolete", "/etc/other.xml " + hex.EncodeToString(sum[:]), "/etc/ImageMagick-6/../other.xml " + hex.EncodeToString(sum[:]), path + " " + hex.EncodeToString(sum[:]) + "\n" + path + " " + hex.EncodeToString(sum[:])} {
		if _, err = addImageMagickConffiles(map[string]string{}, output); err == nil {
			t.Fatal("invalid configuration metadata accepted", output)
		}
	}
	after := newProfile(ComposerUpdate)
	after.Files[path] = digest(append(raw, 'x'))
	if compatibleProfiles(before, after, false, false) {
		t.Fatal("configuration changes treated as mutable Composer data")
	}
	after = before
	after.External = map[string]string{"imagick-loader:libMagickCore-6.Q16.so.7:/lib/core": "/opt/local/core"}
	if compatibleProfiles(before, after, false, false) {
		t.Fatal("loader dependency changes ignored")
	}
}

func TestImageMagickVersionsAreNobleSourceAndABIBounded(t *testing.T) {
	for _, version := range []string{"8:6.9.12.98+dfsg1-5.2build2", "8:6.9.12.98+dfsg1-5.2ubuntu0.1~esm15", "8:6.9.12.98+dfsg1-5.2ubuntu0.2"} {
		if !reviewedImageMagickVersion.MatchString(version) {
			t.Fatal("reviewed source family rejected", version)
		}
	}
	for _, version := range []string{"6.9.12.98+dfsg1-5.2build2", "8:6.9.12.99+dfsg1-5.2build2", "8:7.1.1-1", "8:6.9.12.98+dfsg1-5.2build2+custom", "8:6.9.12.98+dfsg1-5.2ubuntu0.1~other1"} {
		if reviewedImageMagickVersion.MatchString(version) {
			t.Fatal("unreviewed source family accepted", version)
		}
	}
	for _, version := range []string{"8.3", "8.4", "8.5"} {
		p := newProfile(ComposerUpdate)
		p.External["package:php"+version+"-imagick"] = "unreviewed"
		if qualifyImagickStartup(context.Background(), &p, version) == nil {
			t.Fatal("unreviewed Imagick package accepted")
		}
	}
}

func TestImagickLoaderRequiresVerifiedTransitiveLibraries(t *testing.T) {
	for _, scenario := range []string{"packaged", "missing-library", "unowned-library", "tampered-library", "custom-path", "custom-rpath", "missing-transitive", "wrong-soname", "different-magick-implementation"} {
		t.Run(scenario, func(t *testing.T) {
			p := newProfile(ComposerUpdate)
			module := phpTestELF("20240924", []string{"libMagickCore-6.Q16.so.7"}, "")
			core := phpTestNamedELF("", []string{"libc.so.6"}, "", "libMagickCore-6.Q16.so.7")
			libc := phpTestNamedELF("", nil, "", "libc.so.6")
			if scenario == "custom-rpath" {
				core = phpTestNamedELF("", []string{"libc.so.6"}, "/opt/custom", "libMagickCore-6.Q16.so.7")
			}
			if scenario == "wrong-soname" {
				core = libc
			}
			p.External["imagick-library:libMagickCore-6.Q16.so.7"] = "/usr/lib/core"
			if scenario == "different-magick-implementation" {
				p.External["imagick-library:libMagickCore-6.Q16.so.7"] = "/usr/lib/other-core"
			}
			files := map[string][]byte{"/usr/lib/core": core, "/usr/lib/libc": libc}
			for path, raw := range files {
				p.Files[path] = digest(raw)
			}
			cache := map[string][]string{"libMagickCore-6.Q16.so.7": {"/usr/lib/core"}, "libc.so.6": {"/usr/lib/libc"}}
			switch scenario {
			case "missing-library":
				delete(files, "/usr/lib/core")
			case "unowned-library":
				delete(p.Files, "/usr/lib/core")
			case "tampered-library":
				files["/usr/lib/core"] = append(core, 'x')
			case "custom-path":
				cache["libMagickCore-6.Q16.so.7"] = []string{"/opt/custom/core"}
				files["/opt/custom/core"] = core
			case "missing-transitive":
				delete(cache, "libc.so.6")
			}
			err := bindImagickLibraries(&p, module, cache, func(path string, _ int64) ([]byte, error) {
				raw, ok := files[path]
				if !ok {
					return nil, os.ErrNotExist
				}
				return raw, nil
			}, func(path string) (string, error) { return path, nil })
			if scenario == "packaged" {
				if err != nil {
					t.Fatal(err)
				}
				if p.External["imagick-loader:libc.so.6:/usr/lib/libc"] != "/usr/lib/libc" {
					t.Fatal("transitive loader input not bound")
				}
			} else if err == nil {
				t.Fatal("unsafe dependency accepted")
			}
		})
	}
}

func TestImagickLoaderCacheRejectsAliasesAndUnsupportedOutput(t *testing.T) {
	output := "2 libs found in cache `/etc/ld.so.cache'\n\tlibMagickCore-6.Q16.so.7 (libc6,x86-64) => /lib/x86_64-linux-gnu/libMagickCore-6.Q16.so.7\n\tlibc.so.6 (libc6,x86-64) => /lib/x86_64-linux-gnu/libc.so.6\n"
	cache, err := parseImagickLoaderCache(output)
	if err != nil || len(cache) != 2 {
		t.Fatal(cache, err)
	}
	for _, output := range []string{"", "cache unavailable", "libcore => relative/path", "libcore (libc6,x86-64) => /usr/lib/../local/libcore", "libcore unexpected =>"} {
		if _, err = parseImagickLoaderCache(output); err == nil {
			t.Fatal("invalid loader metadata accepted", output)
		}
	}
	p := newProfile(ComposerUpdate)
	err = bindImagickLibraries(&p, phpTestELF("", []string{"libcore"}, ""), map[string][]string{"libcore": {"/lib/core"}}, func(string, int64) ([]byte, error) { return nil, errors.New("unsafe root ownership") }, func(path string) (string, error) { return path, nil })
	if err == nil {
		t.Fatal("unsafe dependency ownership accepted")
	}
}

// Read-only opt-in check for the existing dedicated VM. No PHP, Composer,
// ImageMagick, image delegate, package installer or application is executed.
func TestVMReadOnlyPackagedImagickPHPQualification(t *testing.T) {
	host, _ := os.Hostname()
	if os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" || host != "fluxo-php-patch-test" {
		t.Skip("explicit root dedicated VM inspection only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	p := newProfile(ComposerUpdate)
	if err := qualifyPHPCLI(ctx, &p); err != nil {
		t.Fatal(err)
	}
	if p.External["php-imagick-startup"] != imagickStartupProfile {
		t.Fatal("fixture must have packaged Imagick enabled in default CLI")
	}
	t.Log("read-only packaged Imagick qualification passed", p.External["php-interpreter"], "bound files", len(p.Files))
}
