package maintenancetask

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// profile is durable invocation evidence, not a command-name exemption.
// Files and directory membership describe code/configuration inputs; external
// domains describe effects which can outlive the helper's cgroup.
type profile struct {
	Kind        Kind                `json:"kind"`
	Files       map[string]string   `json:"files"`
	Directories map[string][]string `json:"directories"`
	External    map[string]string   `json:"external"`
}

func trustedDatabase(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("Unsafe panel database path")
	}
	for p := path; ; p = filepath.Dir(p) {
		i, e := os.Lstat(p)
		if e != nil {
			return e
		}
		s, ok := i.Sys().(*syscall.Stat_t)
		if !ok || s.Uid != 0 || i.Mode().Perm()&0022 != 0 || i.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Unsafe panel database ancestry: %s", p)
		}
		if p == path {
			if !i.Mode().IsRegular() || s.Nlink != 1 {
				return errors.New("Unsafe panel database file")
			}
		} else if !i.IsDir() {
			return errors.New("Unsafe panel database directory")
		}
		if p == "/" {
			return nil
		}
	}
}

// validateCertbotConfig covers the ConfigObj forms written by Fluxo's webroot
// issuance. Unknown execution-affecting options never become empty defaults.
// It intentionally does not execute Python or import Certbot to inspect config.
func validateCertbotConfig(raw []byte, lineage bool) error {
	allowed := map[string]bool{
		"version": true, "archive_dir": true, "cert": true, "privkey": true, "chain": true, "fullchain": true,
		"account": true, "authenticator": true, "server": true, "key_type": true, "webroot_path": true,
		"rsa_key_size": true, "elliptic_curve": true, "pref_challs": true, "preferred_challenges": true,
		"renew_before_expiry": true, "key_reuse": true, "reuse_key": true, "autorenew": true,
		"email": true, "agree_tos": true, "non_interactive": true, "max_log_backups": true, "preconfigured_renewal": true,
		"deploy_hook": true, "renew_hook": true, "pre_hook": true, "post_hook": true, "installer": true,
	}
	section := ""
	seen := map[string]bool{}
	webroot := false
	for _, line := range strings.Split(string(raw), "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") || strings.HasPrefix(s, ";") {
			continue
		}
		if strings.HasPrefix(s, "[") {
			if !lineage || (s != "[renewalparams]" && s != "[[webroot_map]]") {
				return fmt.Errorf("Unsupported Certbot section %s", s)
			}
			section = s
			continue
		}
		key, value, ok := strings.Cut(s, "=")
		if !ok {
			return errors.New("Unsupported Certbot configuration syntax")
		}
		key = strings.ReplaceAll(strings.TrimSpace(key), "-", "_")
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "'") {
			if len(value) < 2 || value[0] != value[len(value)-1] || strings.ContainsAny(value[1:len(value)-1], "\\\"'") {
				return errors.New("Unsupported Certbot quoted value")
			}
			value = value[1 : len(value)-1]
		}
		identity := section + "/" + key
		if seen[identity] {
			return fmt.Errorf("Duplicate Certbot option %s", key)
		}
		seen[identity] = true
		if section == "[[webroot_map]]" {
			if strings.ContainsAny(key, " \t/$%\\") || key == "" || !safeWebroot(value) {
				return errors.New("Unsupported Certbot webroot mapping")
			}
			continue
		}
		if !allowed[key] {
			return fmt.Errorf("Unsupported Certbot option %s", key)
		}
		switch key {
		case "archive_dir":
			if !safeCertificatePath(value, "/etc/letsencrypt/archive/") {
				return errors.New("Certbot archive directory is outside its managed storage")
			}
		case "cert", "privkey", "chain", "fullchain":
			if !safeCertificatePath(value, "/etc/letsencrypt/live/") {
				return errors.New("Certbot certificate path is outside its managed storage")
			}
		case "webroot_path":
			for _, path := range strings.Split(strings.TrimSuffix(value, ","), ",") {
				if !safeWebroot(strings.TrimSpace(path)) {
					return errors.New("Certbot webroot is outside the supported site directories")
				}
			}
		case "server":
			if value != "https://acme-v02.api.letsencrypt.org/directory" && value != "https://acme-staging-v02.api.letsencrypt.org/directory" {
				return errors.New("Certbot ACME provider needs compatibility review")
			}
		case "authenticator":
			if value != "webroot" {
				return errors.New("Automatic runtime maintenance supports only the Fluxo webroot Certbot profile")
			}
			webroot = true
		case "installer", "pre_hook", "post_hook":
			if value != "" && value != "None" {
				return fmt.Errorf("Unsupported Certbot %s", key)
			}
		case "deploy_hook", "renew_hook":
			if value != "" && value != "systemctl reload nginx" {
				return fmt.Errorf("Unsupported Certbot %s: %s", key, value)
			}
		}
	}
	if lineage && !webroot {
		return errors.New("Certbot lineage has no explicit webroot authenticator")
	}
	return nil
}

func safeCertificatePath(path, prefix string) bool {
	return filepath.Clean(path) == path && strings.HasPrefix(path, prefix) && !strings.ContainsAny(path, "\x00\r\n")
}
func safeWebroot(path string) bool {
	return filepath.Clean(path) == path && (strings.HasPrefix(path, "/home/fluxo/") || path == "/var/www/html" || strings.HasPrefix(path, "/var/www/html/")) && !strings.ContainsAny(path, "\x00\r\n")
}

// Approved data effects are distinct from executable/configuration drift.
// Qualifiers still parse all resulting files; these exceptions never authorize
// a new hook, plugin, interpreter, source package or nginx master replacement.
func compatibleProfiles(before, after profile, allowComposerReplacement bool, newBoot bool) bool {
	if before.Kind != after.Kind {
		return false
	}
	mutable := func(path string) bool {
		if before.Kind == CertificateRenewal {
			return strings.HasPrefix(path, "/etc/letsencrypt/renewal/")
		}
		if before.Kind == ComposerUpdate {
			return allowComposerReplacement && path == "/usr/local/bin/composer" || composerMutableData(path)
		}
		return false
	}
	for path, value := range before.Files {
		if !mutable(path) && after.Files[path] != value {
			return false
		}
	}
	for path, value := range after.Files {
		if !mutable(path) && before.Files[path] != value {
			return false
		}
	}
	mutableDir := func(path string) bool {
		return before.Kind == ComposerUpdate && composerMutableData(path)
	}
	for path, names := range before.Directories {
		if !mutableDir(path) && strings.Join(names, "\x00") != strings.Join(after.Directories[path], "\x00") {
			return false
		}
	}
	for path, names := range after.Directories {
		if !mutableDir(path) && strings.Join(names, "\x00") != strings.Join(before.Directories[path], "\x00") {
			return false
		}
	}
	for key, value := range before.External {
		if newBoot && before.Kind == CertificateRenewal && key == "nginx-master" {
			continue
		}
		if after.External[key] != value {
			return false
		}
	}
	for key, value := range after.External {
		if newBoot && before.Kind == CertificateRenewal && key == "nginx-master" {
			continue
		}
		if before.External[key] != value {
			return false
		}
	}
	return true
}

// Composer configuration is executable-adjacent. The supported self-update
// profile has no project/global plugins, autoloaders or scripts. Ordinary
// authentication and cache data are handled separately, never treated as code.
func validateComposerConfig(values map[string]any) error {
	for key, value := range values {
		switch key {
		case "config":
			m, ok := value.(map[string]any)
			if !ok {
				return errors.New("Invalid Composer config")
			}
			for k, v := range m {
				switch k {
				case "secure-http":
					if v != true {
						return errors.New("Composer secure-http must stay enabled")
					}
				case "disable-tls":
					if v != false {
						return errors.New("Composer TLS must stay enabled")
					}
				case "allow-plugins":
					if v == false {
						continue
					}
					m, ok := v.(map[string]any)
					if !ok || len(m) != 0 {
						return errors.New("Composer global plugins are unsupported during coordinated maintenance")
					}
				case "cache-dir", "cache-files-dir", "cache-repo-dir", "cache-vcs-dir":
					path, ok := v.(string)
					if !ok || filepath.Clean(path) != path || !(path == "/root/.cache/composer" || strings.HasPrefix(path, "/root/.cache/composer/") || path == "/root/.composer/cache" || strings.HasPrefix(path, "/root/.composer/cache/") || path == "/root/.config/composer/cache" || strings.HasPrefix(path, "/root/.config/composer/cache/")) {
						return errors.New("Composer cache must remain in its root-private data directory")
					}
				case "cache-ttl", "cache-files-ttl", "cache-files-maxsize", "process-timeout", "preferred-install", "sort-packages", "notify-on-install", "github-protocols", "store-auths", "use-github-api":
				default:
					return fmt.Errorf("Unsupported Composer configuration: %s", k)
				}
			}
		case "require", "require-dev", "autoload", "autoload-dev", "scripts", "repositories", "extra", "include-path":
			return fmt.Errorf("Composer %s needs manual runtime maintenance", key)
		default:
			return fmt.Errorf("Unsupported Composer global field: %s", key)
		}
	}
	return nil
}
