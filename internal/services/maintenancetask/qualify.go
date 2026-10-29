package maintenancetask

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"fluxo/internal/releaseinfo"
	"fluxo/internal/syscmd"
)

func (p *profile) file(path string, optional bool) error {
	b, err := readRegular(path, 128<<20)
	if optional && os.IsNotExist(err) {
		p.Files[path] = "absent"
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	p.Files[path] = digest(b)
	return nil
}

func (p *profile) directory(path string, optional bool) ([]os.DirEntry, error) {
	if err := trustedDatabaseDirectory(path); err != nil {
		if optional && os.IsNotExist(err) {
			p.Directories[path] = nil
			return nil, nil
		}
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	p.Directories[path] = names
	return entries, nil
}

func trustedDatabaseDirectory(path string) error {
	for dir := path; ; dir = filepath.Dir(dir) {
		s, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		if !s.IsDir() || s.Mode().Perm()&0022 != 0 {
			return fmt.Errorf("Unsafe task configuration directory: %s", dir)
		}
		// readRegular validates uid and ancestors using a regular child. Empty
		// directories also need the same owner check.
		if s.Sys().(*syscall.Stat_t).Uid != 0 {
			return errors.New("Task configuration directory is not root-owned")
		}
		if dir == "/" {
			return nil
		}
	}
}

// packageInputs binds the installed distribution implementation and checks its
// packaged payload against the administrator's protected dpkg database. Unknown
// implementations or local payload changes do not acquire execution authority.
func packageInputs(ctx context.Context, p *profile, packages ...string) error {
	if err := p.file("/usr/bin/dpkg-query", false); err != nil {
		return err
	}
	if err := p.file("/usr/bin/dpkg", false); err != nil {
		return err
	}
	closure, err := inputClosure(ctx, packages)
	if err != nil {
		return err
	}
	manifests := []string{}
	for _, record := range closure {
		if p.External["package:"+record.name] == record.version {
			continue
		}
		manifest := "/var/lib/dpkg/info/" + record.name + ".md5sums"
		if err = p.file(manifest, false); err != nil {
			return err
		}
		p.External["package:"+record.name] = record.version
		manifests = append(manifests, manifest)
	}
	// Verify importable/executable payload directly. Invoking dpkg --verify
	// would read administrative hook configuration before it was qualified.
	// Minimized Ubuntu servers may omit documentation; docs are not code inputs.
	for _, manifest := range manifests {
		if err = ctx.Err(); err != nil {
			return err
		}
		metadata, err := readRegular(manifest, 8<<20)
		if err != nil {
			return fmt.Errorf("Package integrity metadata for %s: %w", manifest, err)
		}
		for _, line := range strings.Split(string(metadata), "\n") {
			if line == "" {
				continue
			}
			// dpkg md5 manifests use two separators; payload filenames may
			// contain spaces (setuptools ships "launcher manifest.xml").
			if len(line) < 35 || line[32:34] != "  " {
				return errors.New("Unsupported package integrity metadata")
			}
			expectedHash, rel := line[:32], line[34:]
			if _, e := hex.DecodeString(expectedHash); e != nil {
				return errors.New("Invalid package integrity digest")
			}
			if filepath.Clean(rel) != rel || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "../") {
				return errors.New("Unsafe package integrity path")
			}
			code := strings.HasPrefix(rel, "usr/bin/") || strings.HasPrefix(rel, "usr/sbin/") || strings.HasPrefix(rel, "usr/lib/") || strings.HasPrefix(rel, "usr/libexec/") || strings.HasPrefix(rel, "usr/share/php/") || strings.HasPrefix(rel, "bin/") || strings.HasPrefix(rel, "sbin/")
			if !code {
				continue
			}
			path := filepath.Join("/", rel)
			real, err := filepath.EvalSymlinks(path)
			if err != nil {
				return fmt.Errorf("Packaged code %s: %w", path, err)
			}
			data, err := readRegular(real, 128<<20)
			if err != nil {
				return fmt.Errorf("Packaged code %s: %w", path, err)
			}
			sum := md5.Sum(data)
			if hex.EncodeToString(sum[:]) != expectedHash {
				return fmt.Errorf("Packaged executable/import payload changed: %s", path)
			}
			p.Files[real] = digest(data)
		}
	}

	return nil
}

func newProfile(kind Kind) profile {
	return profile{Kind: kind, Files: map[string]string{}, Directories: map[string][]string{}, External: map[string]string{}}
}

func qualifyContractProfile(ctx context.Context, c Contract) (profile, error) {
	if !validContract(c) {
		return profile{}, errors.New("Invalid scheduled-job execution contract")
	}
	if c.Kind == ApplicationJob {
		p := newProfile(ApplicationJob)
		credential, err := syscmd.ResolveCredential(c.User)
		if err != nil {
			return p, err
		}
		if credential.Uid == 0 {
			return p, errors.New("Application cron cannot execute as root")
		}
		p.External["credential"] = fmt.Sprintf("%d:%d", credential.Uid, credential.Gid)
		return p, nil
	}
	return qualifyProfile(ctx, c.Kind)
}

func qualifyProfile(ctx context.Context, kind Kind) (profile, error) {
	p := newProfile(kind)
	var err error
	switch kind {
	case ComposerUpdate:
		err = qualifyComposer(ctx, &p)
	case CertificateRenewal:
		err = qualifyCertbot(ctx, &p)
	case PackageCleanup:
		err = qualifyCleanup(ctx, &p)
	default:
		err = errors.New("Unknown built-in system-task kind")
	}
	return p, err
}

func qualifyComposer(ctx context.Context, p *profile, completedSelfUpdate ...string) error {
	if err := p.file("/usr/local/bin/composer", false); err != nil {
		return err
	}
	want := releaseinfo.ComposerSHA256
	if len(completedSelfUpdate) == 1 {
		want = completedSelfUpdate[0]
	}
	if p.Files["/usr/local/bin/composer"] != want {
		return errors.New("Composer self-update coordination requires the release-authenticated Composer baseline; review its current PHAR before runtime maintenance")
	}
	// Never read/execute a root project's Composer configuration implicitly.
	for _, path := range []string{"/composer.json", "/root/composer.json", "/composer.lock", "/root/composer.lock", "/root/vendor/autoload.php"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			return fmt.Errorf("Composer execution can load project code at %s", path)
		}
		p.Files[path] = "absent"
	}
	for _, home := range []string{"/root/.composer", "/root/.config/composer"} {
		entries, err := p.directory(home, true)
		if err != nil {
			return err
		}
		for _, e := range entries {
			path := filepath.Join(home, e.Name())
			switch e.Name() {
			case "config.json":
				b, err := readRegular(path, 1<<20)
				if err != nil {
					return err
				}
				var v map[string]any
				if err = json.Unmarshal(b, &v); err != nil {
					return err
				}
				if err = validateComposerConfig(v); err != nil {
					return err
				}
				p.Files[path] = digest(b)
			case "keys.dev.pub", "keys.tags.pub":
				raw, err := readRegular(path, 1<<20)
				if err != nil {
					return err
				}
				if err = validateComposerKey(e.Name(), raw); err != nil {
					return err
				}
				p.Files[path] = digest(raw)
			case "update-channel":
				raw, err := readRegular(path, 32)
				if err != nil {
					return err
				}
				switch strings.TrimSpace(string(raw)) {
				case "stable", "preview", "snapshot", "2.2":
				default:
					return errors.New("Unsupported Composer update channel data")
				}
				p.Files[path] = digest(raw)
			case "auth.json", ".htaccess":
				if err = p.file(path, false); err != nil {
					return err
				}
			case "cache":
				if err := qualifyComposerData(p, path); err != nil {
					return err
				}
			default:
				return fmt.Errorf("Composer global code/configuration needs review: %s", path)
			}
		}
	}
	for _, path := range []string{"/root/.cache/composer", "/root/.local/share/composer"} {
		if err := qualifyComposerData(p, path); err != nil {
			return err
		}
	}
	return qualifyPHPCLI(ctx, p)
}

func qualifyPHPCLI(ctx context.Context, p *profile) error {
	resolved, err := filepath.EvalSymlinks("/usr/bin/php")
	if err != nil {
		return err
	}
	version := strings.TrimPrefix(resolved, "/usr/bin/php")
	if resolved != "/usr/bin/php"+version || !phpBranch.MatchString(version) {
		return errors.New("Unsupported Composer PHP interpreter")
	}
	p.External["php-interpreter"] = resolved
	if err = p.file(resolved, false); err != nil {
		return err
	}
	if err = packageInputs(ctx, p, "php"+version+"-cli", "php"+version+"-common"); err != nil {
		return err
	}
	if err = p.file("/etc/php/"+version+"/cli/php.ini", false); err != nil {
		return err
	}
	files, err := p.directory("/etc/php/"+version+"/cli/conf.d", false)
	if err != nil {
		return err
	}
	ini := []string{"/etc/php/" + version + "/cli/php.ini"}
	for _, e := range files {
		path, err := filepath.EvalSymlinks("/etc/php/" + version + "/cli/conf.d/" + e.Name())
		if err != nil {
			return err
		}
		if !strings.HasPrefix(path, "/etc/php/"+version+"/mods-available/") {
			return errors.New("Unsupported Composer PHP CLI ini include")
		}
		if err = p.file(path, false); err != nil {
			return err
		}
		ini = append(ini, path)
	}
	modules := []string{}
	for _, path := range ini {
		b, err := readRegular(path, 1<<20)
		if err != nil {
			return err
		}
		if err = validatePHPINI(b); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
			if ok && (strings.TrimSpace(key) == "extension" || strings.TrimSpace(key) == "zend_extension") {
				name := strings.TrimSuffix(strings.Trim(strings.TrimSpace(value), "\"'"), ".so")
				modules = append(modules, name)
			}
		}

	}
	return qualifyPHPModules(ctx, p, version, modules)
}

func qualifyCertbot(ctx context.Context, p *profile) error {
	if err := packageInputs(ctx, p, "certbot", "python3-certbot", "python3-acme"); err != nil {
		return err
	}
	if !strings.HasPrefix(p.External["package:python3-certbot"], "2.9.0-") {
		return errors.New("Certbot distribution implementation needs compatibility review")
	}
	if err := p.file("/usr/bin/certbot", false); err != nil {
		return err
	}
	if err := p.file("/etc/letsencrypt/cli.ini", true); err != nil {
		return err
	}
	if p.Files["/etc/letsencrypt/cli.ini"] != "absent" {
		b, e := readRegular("/etc/letsencrypt/cli.ini", 1<<20)
		if e != nil {
			return e
		}
		if e = validateCertbotConfig(b, false); e != nil {
			return e
		}
	}
	for _, dir := range []string{"/etc/letsencrypt/renewal-hooks/pre", "/etc/letsencrypt/renewal-hooks/post", "/etc/letsencrypt/renewal-hooks/deploy"} {
		entries, e := p.directory(dir, true)
		if e != nil {
			return e
		}
		if len(entries) > 0 {
			return fmt.Errorf("Custom Certbot directory hooks need maintenance review: %s", dir)
		}
	}
	entries, err := p.directory("/etc/letsencrypt/renewal", true)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".conf") {
			return errors.New("Unknown Certbot renewal configuration entry")
		}
		path := filepath.Join("/etc/letsencrypt/renewal", e.Name())
		b, err := readRegular(path, 1<<20)
		if err != nil {
			return err
		}
		if err = validateCertbotConfig(b, true); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		p.Files[path] = digest(b)
	}
	if err = qualifyPython(ctx, p); err != nil {
		return err
	}
	return qualifyNginx(ctx, p)
}
