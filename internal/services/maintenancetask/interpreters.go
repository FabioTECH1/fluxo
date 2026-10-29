package maintenancetask

import (
	"bytes"
	"context"
	"errors"
	"fluxo/internal/syscmd"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var phpBranch = regexp.MustCompile(`^[0-9]{1,2}\.[0-9]{1,2}$`)

func validatePHPINI(raw []byte) error {
	for _, line := range strings.Split(string(raw), "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, ";") || strings.HasPrefix(s, "[") {
			continue
		}
		key, value, ok := strings.Cut(s, "=")
		if !ok {
			return errors.New("Unsupported PHP CLI ini syntax")
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), "\"'")
		switch key {
		case "auto_prepend_file", "auto_append_file", "opcache.preload", "opcache.file_cache", "ffi.preload":
			if value != "" {
				return fmt.Errorf("PHP CLI %s can load unreviewed executable code", key)
			}
		case "extension_dir":
			return errors.New("Custom PHP CLI extension directories require manual review")
		case "extension", "zend_extension":
			if !standardPHPExtensions[strings.TrimSuffix(value, ".so")] {
				return fmt.Errorf("Unsupported PHP CLI extension: %s", value)
			}
		}
	}
	return nil
}

var standardPHPExtensions = map[string]bool{
	"bcmath": true, "calendar": true, "ctype": true, "curl": true, "dom": true, "exif": true, "ffi": true,
	"fileinfo": true, "ftp": true, "gd": true, "gettext": true, "iconv": true, "intl": true, "mbstring": true,
	"mysqli": true, "mysqlnd": true, "opcache": true, "pcntl": true, "pdo": true, "pdo_mysql": true,
	"igbinary": true, "msgpack": true, "redis": true, "pdo_pgsql": true, "pdo_sqlite": true, "pgsql": true, "phar": true, "posix": true, "readline": true,
	"shmop": true, "simplexml": true, "soap": true, "sockets": true, "sodium": true, "sqlite3": true,
	"sysvmsg": true, "sysvsem": true, "sysvshm": true, "tokenizer": true, "xml": true, "xmlreader": true,
	"xmlwriter": true, "xsl": true, "zip": true,
}

func qualifyPHPModules(ctx context.Context, p *profile, version string, modules []string) error {
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
		if err := packageInputs(ctx, p, owner); err != nil {
			return err
		}
	}
	return nil
}

func qualifyPython(ctx context.Context, p *profile) error {
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
	if err = packageInputs(ctx, p, "python3.12-minimal", "libpython3.12-minimal", "libpython3.12-stdlib"); err != nil {
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
			if e = packageInputs(ctx, p, "python3-setuptools"); e != nil {
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
			if e = packageInputs(ctx, p, "python3-zope.interface"); e != nil {
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

func validateCertbotEntryPoints(raw []byte) error {
	section := ""
	allowed := map[string]string{"manual": "certbot._internal.plugins.manual:Authenticator", "null": "certbot._internal.plugins.null:Installer", "standalone": "certbot._internal.plugins.standalone:Authenticator", "webroot": "certbot._internal.plugins.webroot:Authenticator"}
	for _, line := range strings.Split(string(raw), "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if strings.HasPrefix(s, "[") {
			section = s
			continue
		}
		if section != "[certbot.plugins]" {
			continue
		}
		key, value, ok := strings.Cut(s, "=")
		if !ok || allowed[strings.TrimSpace(key)] != strings.TrimSpace(value) {
			return errors.New("Unknown Certbot plugin entry point")
		}
	}
	return nil
}

// Ubuntu's standard setuptools shim is executable startup code, so accept its
// exact implementation only after the package payload and imports are verified.
const distroDistutilsPrecedence = "import os; var = 'SETUPTOOLS_USE_DISTUTILS'; enabled = os.environ.get(var, 'local') == 'local'; enabled and __import__('_distutils_hack').add_shim(); \n"
