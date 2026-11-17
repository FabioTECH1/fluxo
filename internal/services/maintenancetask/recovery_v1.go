package maintenancetask

// V1 observation protocol retained from the exact baseline below. These
// functions reproduce the expected evidence SHAPE, never a maintenance payload.
// They are called only AFTER full current inspection, under recovery locks and
// proven domain quiescence. The original snapshot is compared in both directions;
// missing observations cannot authorize their own omission. Fresh admission never
// calls these functions. Keep this versioned producer while V1 history is live.

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/releaseinfo"
	"fluxo/internal/syscmd"
)

func legacyV1Observation(ctx context.Context, kind Kind) (profile, error) {
	p := newProfile(kind)
	var err error
	switch kind {
	case ComposerUpdate:
		err = legacyV1QualifyComposer(ctx, &p)
	case CertificateRenewal:
		err = legacyV1QualifyCertbot(ctx, &p)
	case PackageCleanup:
		err = legacyV1QualifyCleanup(ctx, &p)
	default:
		err = errors.New("unsupported V1 recovery observation kind")
	}
	return p, err
}

// Observation producer from qualify.go at 0b82892cd429e3e7e6b5f31e4b3a99a4ddee342a.
func legacyV1PackageInputs(ctx context.Context, p *profile, packages ...string) error {
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

// Observation producer from qualify.go at 0b82892cd429e3e7e6b5f31e4b3a99a4ddee342a.
func legacyV1QualifyComposer(ctx context.Context, p *profile, completedSelfUpdate ...string) error {
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
	return legacyV1QualifyPHPCLI(ctx, p)
}

// Observation producer from qualify.go at 0b82892cd429e3e7e6b5f31e4b3a99a4ddee342a.
func legacyV1QualifyPHPCLI(ctx context.Context, p *profile) error {
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
	if err = legacyV1PackageInputs(ctx, p, "php"+version+"-cli", "php"+version+"-common"); err != nil {
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
	return legacyV1QualifyPHPModules(ctx, p, version, modules)
}

// Observation producer from qualify.go at 0b82892cd429e3e7e6b5f31e4b3a99a4ddee342a.
func legacyV1QualifyCertbot(ctx context.Context, p *profile) error {
	if err := legacyV1PackageInputs(ctx, p, "certbot", "python3-certbot", "python3-acme"); err != nil {
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
	if err = legacyV1QualifyPython(ctx, p); err != nil {
		return err
	}
	return legacyV1QualifyNginx(ctx, p)
}

// Observation producer from interpreters.go at 0b82892cd429e3e7e6b5f31e4b3a99a4ddee342a.
func legacyV1QualifyPHPModules(ctx context.Context, p *profile, version string, modules []string) error {
	if len(modules) == 0 {
		return nil
	}
	args := []string{"--admindir=/var/lib/dpkg", "-S"}
	for _, name := range modules {
		args = append(args, "/usr/lib/php/*/"+name+".so")
	}
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var out bytes.Buffer
	if err := syscmd.RunMaintenance(probe, "", &out, nil, "/usr/bin/dpkg-query", args...); err != nil {
		return fmt.Errorf("PHP CLI extension package is unavailable: %w", err)
	}
	owners := map[string]bool{}
	for _, line := range strings.Split(out.String(), "\n") {
		owner, path, ok := strings.Cut(line, ": /")
		if !ok {
			continue
		}
		if !strings.HasPrefix(owner, "php"+version+"-") {
			continue
		}
		owners[owner] = true
		p.External["php-module:"+filepath.Base(path)] = "/" + path
	}
	for _, module := range modules {
		if p.External["php-module:"+module+".so"] == "" {
			return fmt.Errorf("PHP %s CLI extension %s has no matching installed implementation", version, module)
		}
	}
	for owner := range owners {
		if err := legacyV1PackageInputs(ctx, p, owner); err != nil {
			return err
		}
	}
	return nil
}

// Observation producer from interpreters.go at 0b82892cd429e3e7e6b5f31e4b3a99a4ddee342a.
func legacyV1QualifyPython(ctx context.Context, p *profile) error {
	python, err := filepath.EvalSymlinks("/usr/bin/python3")
	if err != nil {
		return err
	}
	if python != "/usr/bin/python3.12" {
		return errors.New("Unsupported Certbot Python interpreter")
	}
	if err = p.file(python, false); err != nil {
		return err
	}
	if err = legacyV1PackageInputs(ctx, p, "python3.12-minimal", "libpython3.12-minimal", "libpython3.12-stdlib"); err != nil {
		return err
	}
	for _, path := range []string{"/usr/local/lib/python3.12/dist-packages", "/root/.local/lib/python3.12/site-packages"} {
		es, err := p.directory(path, true)
		if err != nil {
			return err
		}
		if len(es) != 0 {
			return fmt.Errorf("Custom Python import directory needs review: %s", path)
		}
	}
	entries, err := p.directory("/usr/lib/python3/dist-packages", false)
	if err != nil {
		return err
	}
	for _, e := range entries {
		path := filepath.Join("/usr/lib/python3/dist-packages", e.Name())
		if e.Name() == "distutils-precedence.pth" {
			raw, e := readRegular(path, 1<<20)
			if e != nil {
				return e
			}
			if string(raw) != distroDistutilsPrecedence {
				return errors.New("Unqualified setuptools startup shim")
			}
			if e = legacyV1PackageInputs(ctx, p, "python3-setuptools"); e != nil {
				return e
			}
			if p.Files[path] != digest(raw) {
				return errors.New("Setuptools startup shim is not bound to its package payload")
			}
			continue
		}
		if e.Name() == "zope.interface-6.1-nspkg.pth" {
			raw, e := readRegular(path, 1<<20)
			if e != nil {
				return e
			}
			if digest(raw) != "4961151fe8c45ac298acbd2aa02e86049692b71fe22b11a8018f4f432705542e" {
				return errors.New("Unqualified Zope namespace startup shim")
			}
			if e = legacyV1PackageInputs(ctx, p, "python3-zope.interface"); e != nil {
				return e
			}
			if p.Files[path] != digest(raw) {
				return errors.New("Zope startup shim is not bound to its package payload")
			}
			continue
		}
		if strings.HasSuffix(e.Name(), ".pth") || e.Name() == "sitecustomize.py" || e.Name() == "usercustomize.py" {
			return fmt.Errorf("Python startup extension needs review: %s", path)
		}
		if !e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".dist-info") && !strings.HasSuffix(e.Name(), ".egg-info") {
			continue
		}
		b, err := readRegular(filepath.Join(path, "entry_points.txt"), 1<<20)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		p.Files[filepath.Join(path, "entry_points.txt")] = digest(b)
		if err = validateCertbotEntryPoints(b); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return nil
}

// Observation producer from nginx.go at 0b82892cd429e3e7e6b5f31e4b3a99a4ddee342a.
func legacyV1QualifyNginx(ctx context.Context, p *profile) error {
	if err := legacyV1PackageInputs(ctx, p, "nginx", "nginx-common"); err != nil {
		return err
	}
	if err := p.file("/usr/sbin/nginx", false); err != nil {
		return err
	}
	u, err := unitProperties(ctx, "nginx.service", "Id,LoadState,ActiveState,SubState,MainPID,ControlPID,ControlGroup,Job,Delegate,DropInPaths,ExecReload,ExecStart,FragmentPath,NeedDaemonReload,Environment,ExecReloadEx")
	if err != nil {
		return err
	}
	want := map[string]string{"Id": "nginx.service", "LoadState": "loaded", "ActiveState": "active", "SubState": "running", "ControlPID": "0", "Delegate": "no", "DropInPaths": "", "Environment": "", "ControlGroup": "/system.slice/nginx.service"}
	for k, v := range want {
		if u[k] != v {
			return fmt.Errorf("Nginx renewal domain %s needs maintenance review", k)
		}
	}
	if u["Job"] != "" {
		return errors.New("Nginx has an outstanding service job")
	}
	fragment, err := filepath.EvalSymlinks(u["FragmentPath"])
	if err != nil {
		return err
	}
	if p.Files[fragment] == "" || u["NeedDaemonReload"] != "no" {
		return errors.New("Nginx must use its inspected packaged unit without pending configuration changes")
	}
	startPrefix := "{ path=/usr/sbin/nginx ; argv[]=/usr/sbin/nginx -g daemon on; master_process on; ;"
	if !strings.HasPrefix(u["ExecStart"], startPrefix) || strings.Count(u["ExecStart"], "{ path=") != 1 {
		return errors.New("Nginx startup differs from the supported default configuration path")
	}
	prefix := "{ path=/usr/sbin/nginx ; argv[]=/usr/sbin/nginx -g daemon on; master_process on; -s reload ;"
	if !strings.HasPrefix(u["ExecReload"], prefix) || strings.Count(u["ExecReload"], "{ path=") != 1 {
		return errors.New("Nginx reload command differs from the supported packaged reload")
	}
	pid, err := strconv.Atoi(u["MainPID"])
	if err != nil || pid <= 1 {
		return errors.New("Nginx has no stable master")
	}
	anchor, err := processIdentity(pid)
	if err != nil {
		return err
	}
	exe, err := processExecutable(pid)
	if err != nil {
		return err
	}
	if exe != "/usr/sbin/nginx" {
		return errors.New("Nginx master executable changed")
	}
	env, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return err
	}
	for _, entry := range strings.Split(string(env), "\x00") {
		key, value, ok := strings.Cut(entry, "=")
		if ok && value != "" && (strings.HasPrefix(key, "LD_") || key == "OPENSSL_CONF" || key == "OPENSSL_MODULES") {
			return fmt.Errorf("Nginx loader environment needs maintenance review: %s", key)
		}
	}
	p.External["nginx-master"] = anchor
	visited := map[string]bool{}
	return legacyV1NginxConfig(p, "/etc/nginx/nginx.conf", visited, 0)
}

// Observation producer from nginx.go at 0b82892cd429e3e7e6b5f31e4b3a99a4ddee342a.
func legacyV1NginxConfig(p *profile, path string, visited map[string]bool, depth int) error {
	if depth > 32 {
		return errors.New("Nginx include depth exceeds the supported profile")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(real, "/etc/nginx/") {
		return fmt.Errorf("Nginx include outside the supported configuration tree: %s", path)
	}
	if visited[real] {
		return nil
	}
	visited[real] = true
	b, err := readRegular(real, 8<<20)
	if err != nil {
		return err
	}
	p.Files[real] = digest(b)
	p.External["nginx-include:"+path] = real
	statements, err := nginxStatements(string(b))
	if err != nil {
		return err
	}
	for _, s := range statements {
		if len(s) == 0 {
			continue
		}
		name := s[0]
		if name == "load_module" || name == "ssl_engine" || name == "env" || strings.HasPrefix(name, "lua_") || strings.Contains(name, "_by_lua") || strings.HasPrefix(name, "perl") || strings.HasPrefix(name, "js_") {
			return fmt.Errorf("Nginx executable directive needs maintenance review: %s", name)
		}
		if name != "include" {
			continue
		}
		if len(s) != 2 || strings.ContainsAny(s[1], "$\\") {
			return errors.New("Unsupported dynamic Nginx include")
		}
		pattern := s[1]
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join("/etc/nginx", pattern)
		}
		// Bind directory membership too; a newly added include cannot hide
		// behind an unchanged parent configuration hash.
		if strings.ContainsAny(filepath.Dir(pattern), "*?[") {
			return errors.New("Unsupported Nginx include directory glob")
		}
		if _, err = p.directory(filepath.Dir(pattern), false); err != nil {
			return err
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return err
		}
		for _, child := range matches {
			if err = legacyV1NginxConfig(p, child, visited, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// Observation producer from cleanup.go at 0b82892cd429e3e7e6b5f31e4b3a99a4ddee342a.
func legacyV1QualifyCleanup(ctx context.Context, p *profile) error {
	if err := legacyV1PackageInputs(ctx, p, "apt"); err != nil {
		return err
	}
	if !strings.HasPrefix(p.External["package:apt"], "2.8.") {
		return errors.New("APT cleanup implementation needs compatibility review")
	}
	for _, path := range []string{"/usr/bin/apt-get", "/usr/bin/apt-config", "/var/lib/dpkg/status", "/var/lib/apt/extended_states"} {
		if err := p.file(path, path == "/var/lib/apt/extended_states"); err != nil {
			return err
		}
	}
	out, err := legacyV1Run(ctx, 10*time.Second, "/usr/bin/apt-config", "dump")
	if err != nil {
		return err
	}
	if err = legacyV1ValidateAPTConfig(out); err != nil {
		return err
	}
	p.External["apt-effective-configuration"] = digest([]byte(out))
	return nil
}

// Observation producer from cleanup.go at 0b82892cd429e3e7e6b5f31e4b3a99a4ddee342a.
func legacyV1ValidateAPTConfig(out string) error {
	binaries := map[string]string{"dir::bin::methods": "/usr/lib/apt/methods", "dir::bin::solvers": "/usr/lib/apt/solvers", "dir::bin::planners": "/usr/lib/apt/planners", "dir::bin::dpkg": "/usr/bin/dpkg", "dir::bin::gzip": "/bin/gzip", "dir::bin::bzip2": "/bin/bzip2", "dir::bin::xz": "/usr/bin/xz", "dir::bin::lz4": "/usr/bin/lz4", "dir::bin::zstd": "/usr/bin/zstd", "dir::bin::lzma": "/usr/bin/xz"}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, rest, ok := strings.Cut(line, " ")
		if !ok {
			return errors.New("Unsupported APT configuration serialization")
		}
		key = strings.ToLower(key)
		encoded := strings.TrimSpace(rest)
		// apt-config dump retains regex backslashes literally; it is not JSON or
		// a Go string literal. Only strip its documented outer quote/semicolon.
		if len(encoded) < 3 || !strings.HasPrefix(encoded, "\"") || !strings.HasSuffix(encoded, "\";") {
			return fmt.Errorf("Unsupported APT configuration value for %s", key)
		}
		value := encoded[1 : len(encoded)-2]
		if strings.Contains(key, "invoke") || strings.Contains(key, "pre-install-pkgs") {
			if value == "" {
				continue
			}
			if strings.HasPrefix(key, "apt::update::") || strings.HasPrefix(key, "dpkg::post-invoke") || strings.HasPrefix(key, "dpkg::pre-install-pkgs") {
				continue
			}
			return fmt.Errorf("APT hook needs maintenance review: %s", key)
		}
		if strings.HasPrefix(key, "dir::bin") && value != "" {
			base := strings.TrimSuffix(key, "::")
			if expected, ok := binaries[base]; !ok || expected != value {
				return fmt.Errorf("APT executable override needs maintenance review: %s", key)
			}
		}
		if (key == "apt::solver" || key == "apt::planner") && value != "internal" && value != "" {
			return errors.New("External APT planning programs need maintenance review")
		}
	}
	return nil
}
