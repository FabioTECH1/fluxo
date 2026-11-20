package maintenancetask

import (
	"bytes"
	"context"
	"crypto/md5"
	"debug/elf"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const reviewedImagickPackage = "3.8.1-1+ubuntu24.04.1+deb.sury.org+1"
const imagickStartupProfile = "imagick-3.8.1-im6-q16-composer-startup-v1"

// This is startup compatibility for the fixed, authenticated Composer self-
// update only. It does not admit image operations, format discovery, filters,
// or external delegates. Composer's command/configuration qualification remains
// mandatory. In Imagick 3.8.1 MINIT calls MagickWandGenesis; MINFO (not MINIT)
// calls MagickQueryFormats. IM6 initialization reads logging/resource policy,
// but does not invoke image delegates or open coder/filter modules.
// Reviewed sources:
// https://github.com/Imagick/imagick/blob/3.8.1/imagick.c
// https://github.com/ImageMagick/ImageMagick6/blob/6.9.12-98/magick/magick.c
// https://github.com/ImageMagick/ImageMagick6/blob/6.9.12-98/magick/configure.c
// Binding delegates.xml below records configuration drift; it grants no
// authority to execute any of its commands.
func qualifyImagickStartup(ctx context.Context, p *profile, version string) error {
	if p.Kind != ComposerUpdate || (version != "8.4" && version != "8.5") || phpPackageVersion(p, "php"+version+"-imagick") != reviewedImagickPackage {
		return errors.New("Packaged Imagick startup needs compatibility review")
	}
	triplet := map[string]string{"amd64": "x86_64-linux-gnu", "arm64": "aarch64-linux-gnu"}[runtime.GOARCH]
	if triplet == "" {
		return errors.New("Imagick architecture needs compatibility review")
	}
	if err := packageInputs(ctx, p, "imagemagick-6-common", "libmagickcore-6.q16-7t64", "libmagickwand-6.q16-7t64"); err != nil {
		return err
	}
	for _, pkg := range []string{"imagemagick-6-common", "libmagickcore-6.q16-7t64", "libmagickwand-6.q16-7t64"} {
		if !reviewedImageMagickVersion.MatchString(phpPackageVersion(p, pkg)) || phpPackageVersion(p, pkg) != phpPackageVersion(p, "imagemagick-6-common") {
			return errors.New("ImageMagick implementation needs compatibility review: " + pkg)
		}
	}
	module := p.External["php-module:imagick.so"]
	raw, err := readRegular(module, 128<<20)
	if err != nil || p.Files[module] != digest(raw) {
		return errors.New("Verified Imagick module changed")
	}
	api, _ := phpModuleAPI(version)
	if err = inspectImagickELF(raw, api); err != nil {
		return err
	}
	for _, name := range []string{"libMagickCore-6.Q16.so.7", "libMagickWand-6.Q16.so.7"} {
		path := "/usr/lib/" + triplet + "/" + name
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		code, err := readRegular(real, 128<<20)
		if err != nil || p.Files[real] != digest(code) {
			return errors.New("Imagick library is outside its verified dependency closure")
		}
		if err = inspectPHPModuleELF(raw, code); err != nil {
			return err
		}
		p.External["imagick-library:"+name] = real
	}
	if err = qualifyImagickLoader(ctx, p, raw); err != nil {
		return err
	}
	// Installed IM6's configure search path is compiled into the reviewed vendor
	// build. No MAGICK_*, XDG_CONFIG_HOME or application environment is inherited
	// by RunMaintenance. HOME is /root and cwd is /.
	for _, path := range []string{"/root/.config/ImageMagick", "/root/.magick"} {
		entries, err := p.directory(path, true)
		if err != nil {
			return err
		}
		if len(entries) != 0 {
			return fmt.Errorf("Custom ImageMagick startup configuration needs review: %s", path)
		}
	}
	expected, err := imageMagickConfigManifest(ctx, p)
	if err != nil {
		return err
	}
	roots := []string{"/etc/ImageMagick-6", "/usr/share/ImageMagick-6", "/usr/lib/" + triplet + "/ImageMagick-6.9.12/config-Q16", "/usr/share/doc/imagemagick-6-common/html"}
	for _, root := range roots {
		entries, err := p.directory(root, true)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			// Documentation is not an image configuration input unless it adds XML.
			if root == roots[3] && !strings.HasSuffix(entry.Name(), ".xml") {
				continue
			}
			path := filepath.Join(root, entry.Name())
			if entry.IsDir() {
				return fmt.Errorf("Custom ImageMagick configuration subtree needs review: %s", path)
			}
			raw, err := readRegular(path, 8<<20)
			if err != nil {
				return err
			}
			if err = bindImageMagickConfig(p, path, raw, expected); err != nil {
				return err
			}
		}
	}
	for _, required := range []string{"/etc/ImageMagick-6/log.xml", "/etc/ImageMagick-6/policy.xml", "/usr/share/ImageMagick-6/locale.xml", roots[2] + "/configure.xml"} {
		if p.Files[required] == "" {
			return errors.New("ImageMagick startup configuration missing: " + required)
		}
	}
	p.External["php-imagick-startup"] = imagickStartupProfile
	return nil
}

// Security revisions retain the reviewed Noble source/ABI family. Their exact
// installed versions, payloads and configuration are still immutable evidence.
var reviewedImageMagickVersion = regexp.MustCompile(`^8:6\.9\.12\.98\+dfsg1-5\.2(build2|ubuntu0\.[0-9]+(~esm[0-9]+)?)$`)

func phpPackageVersion(p *profile, name string) string {
	if v := p.External["package:"+name]; v != "" {
		return v
	}
	return p.External["package:"+name+":"+runtime.GOARCH]
}

func inspectImagickELF(raw []byte, api string) error {
	if !bytes.Contains(raw, []byte("API"+api+",NTS\x00")) {
		return errors.New("Imagick PHP module API is not the selected non-ZTS branch")
	}
	object, err := elf.NewFile(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	defer object.Close()
	libraries, err := object.ImportedLibraries()
	if err != nil {
		return err
	}
	required := map[string]bool{"libgomp.so.1": false, "libMagickWand-6.Q16.so.7": false, "libMagickCore-6.Q16.so.7": false, "libc.so.6": false}
	for _, name := range libraries {
		if _, ok := required[name]; !ok {
			return errors.New("Imagick native dependency needs review: " + name)
		}
		required[name] = true
	}
	for name, found := range required {
		if !found {
			return errors.New("Reviewed Imagick native dependency missing: " + name)
		}
	}
	symbols, err := object.DynamicSymbols()
	if err != nil {
		return err
	}
	for _, symbol := range symbols {
		if symbol.Name == "get_module" && symbol.Section != elf.SHN_UNDEF {
			return nil
		}
	}
	return errors.New("Imagick PHP module entry point missing")
}

func imageMagickConfigManifest(ctx context.Context, p *profile) (map[string]string, error) {
	manifest := map[string]string{}
	for _, pkg := range []string{"imagemagick-6-common", "libmagickcore-6.q16-7t64"} {
		name := pkg
		if p.External["package:"+name] == "" {
			name += ":" + runtime.GOARCH
		}
		path := "/var/lib/dpkg/info/" + name + ".md5sums"
		raw, err := readRegular(path, 8<<20)
		if err != nil || p.Files[path] != digest(raw) {
			return nil, errors.New("ImageMagick package integrity metadata changed")
		}
		entries, err := parseProtectedMD5Manifest(raw)
		if err != nil {
			return nil, err
		}
		for path, sum := range entries {
			if _, duplicate := manifest[path]; duplicate {
				return nil, errors.New("Ambiguous ImageMagick package integrity metadata")
			}
			manifest[path] = sum
		}
	}
	// dpkg's installed Conffiles field supplies the vendor digest of /etc inputs;
	// the .conffiles list alone has no integrity hashes. A locally changed file
	// cannot be blessed simply by hashing its current bytes.
	output, err := queryPHPModulePackages(ctx, p, "-W", "-f=${Conffiles}\n", "imagemagick-6-common")
	if err != nil {
		return nil, err
	}
	return addImageMagickConffiles(manifest, output)
}

func addImageMagickConffiles(manifest map[string]string, output string) (map[string]string, error) {
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || !strings.HasPrefix(fields[0], "/etc/ImageMagick-6/") || filepath.Clean(fields[0]) != fields[0] {
			return nil, errors.New("Unreviewed ImageMagick conffile metadata")
		}
		sum, err := hex.DecodeString(fields[1])
		if err != nil || len(sum) != md5.Size {
			return nil, errors.New("Invalid ImageMagick conffile digest")
		}
		key := strings.TrimPrefix(fields[0], "/")
		if _, exists := manifest[key]; exists {
			return nil, errors.New("Duplicate ImageMagick conffile metadata")
		}
		manifest[key] = strings.ToLower(fields[1])
	}
	return manifest, nil
}

func bindImageMagickConfig(p *profile, path string, raw []byte, expected map[string]string) error {
	sum := md5.Sum(raw)
	if expected[strings.TrimPrefix(path, "/")] != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("ImageMagick configuration has no matching vendor payload: %s", path)
	}
	p.Files[path] = digest(raw)
	return nil
}
