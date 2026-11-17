package bridge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"unicode"

	"fluxo/internal/safeinput"
	"fluxo/internal/services/nginx"
	sitepkg "fluxo/internal/services/site"
	sslservice "fluxo/internal/services/ssl"
)

func existingSiteConfig(ctx context.Context, q queryer, id int) (string, string, error) {
	var domain, path, php, app, web, strategy, preset, mode, output, python, appDir, www string
	var port sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT domain,path,php_version,app_type,app_port,web_root,deployment_strategy,node_preset,node_mode,static_output_dir,COALESCE(python_preset,''),COALESCE(app_directory,'.'),COALESCE(www_redirect,'none') FROM sites WHERE id=?`, id).Scan(&domain, &path, &php, &app, &port, &web, &strategy, &preset, &mode, &output, &python, &appDir, &www)
	if err != nil {
		return "", "", err
	}
	var overrides int
	if err = q.QueryRowContext(ctx, "SELECT count(*) FROM site_vhost_overrides WHERE site_id=?", id).Scan(&overrides); err != nil {
		return "", "", err
	}
	if overrides != 0 {
		return "", "", errors.New("custom vhost requires explicit baseline profile review; preserve its override")
	}
	if app == "node" && mode == "static" {
		web = sitepkg.NormalizeStaticOutputDir(preset, output)
		app = "html"
	}
	var fullWeb string
	if app == "python" && sitepkg.NormalizePythonPreset(python) == "django" {
		normalized, e := sitepkg.NormalizeAppDirectory(appDir)
		if e != nil {
			return "", "", e
		}
		fullWeb = filepath.Join(sitepkg.ActiveSitePath(path, strategy), normalized)
		app = "python-django"
	} else {
		fullWeb, err = safeinput.NormalizeWebRoot(path, web)
		if err != nil {
			return "", "", err
		}
		if strategy == "zero-downtime" {
			rel, e := filepath.Rel(path, fullWeb)
			if e != nil {
				return "", "", e
			}
			fullWeb = filepath.Join(path, "current", rel)
		}
	}
	if app == "laravel" || app == "php" {
		var octane int
		err = q.QueryRowContext(ctx, `SELECT count(*) FROM daemons WHERE site_id=? AND status NOT IN ('enable_pending','enable_publish_pending','enable_fallback_pending','enable_cleanup_pending') AND (name='Laravel Octane' OR command LIKE '%artisan octane:start%')`, id).Scan(&octane)
		if err != nil {
			return "", "", err
		}
		if octane > 0 {
			if port.Int64 <= 0 {
				return "", "", errors.New("Octane port is missing")
			}
			app = "node"
		}
	}
	type pair struct{ cert, key string }
	var active pair
	err = q.QueryRowContext(ctx, "SELECT cert_path,key_path FROM certificates WHERE site_id=? AND active=1 LIMIT 1", id).Scan(&active.cert, &active.key)
	if err != nil && err != sql.ErrNoRows {
		return "", "", err
	}
	bindings := map[string]pair{}
	rows, err := q.QueryContext(ctx, `SELECT b.domain,c.cert_path,c.key_path FROM certificate_domain_bindings b JOIN certificates c ON c.id=b.certificate_id AND c.site_id=b.site_id WHERE b.site_id=?`, id)
	if err != nil {
		return "", "", err
	}
	for rows.Next() {
		var d string
		var p pair
		if err = rows.Scan(&d, &p.cert, &p.key); err != nil {
			rows.Close()
			return "", "", err
		}
		bindings[strings.ToLower(d)] = p
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", "", err
	}
	var hosts []nginx.HostCertificate
	appendHosts := func(d, behavior string, p pair) error {
		if !safeinput.ValidateDomain(d) {
			return errors.New("invalid persisted vhost domain")
		}
		appHost, redirectHost, target := d, "", ""
		switch behavior {
		case "none", "":
		case "from_www":
			appHost, redirectHost, target = d, "www."+d, d
		case "to_www":
			appHost, redirectHost, target = "www."+d, d, "www."+d
		default:
			return errors.New("invalid persisted WWW redirect")
		}
		for _, item := range []struct{ d, target string }{{appHost, ""}, {redirectHost, target}} {
			if item.d == "" {
				continue
			}
			h := nginx.HostCertificate{Domain: item.d, RedirectTo: item.target}
			if p.cert != "" {
				inspection, e := sslservice.InspectCertificateFiles(p.cert, p.key)
				if e != nil {
					return e
				}
				if sslservice.VerifyCertificateDomains(inspection.Certificate, []string{item.d}) == nil {
					h.CertPath, h.KeyPath = p.cert, p.key
				}
			}
			hosts = append(hosts, h)
		}
		return nil
	}
	if err = appendHosts(domain, www, active); err != nil {
		return "", "", err
	}
	rows, err = q.QueryContext(ctx, "SELECT domain,ssl_disabled,COALESCE(www_redirect,'none') FROM domain_aliases WHERE site_id=? ORDER BY id", id)
	if err != nil {
		return "", "", err
	}
	for rows.Next() {
		var d, w string
		var disabled bool
		if err = rows.Scan(&d, &disabled, &w); err != nil {
			rows.Close()
			return "", "", err
		}
		p := active
		if disabled {
			p = pair{}
		} else if bound, ok := bindings[strings.ToLower(d)]; ok {
			p = bound
		}
		if err = appendHosts(d, w, p); err != nil {
			rows.Close()
			return "", "", err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", "", err
	}
	name := filepath.Base(filepath.Clean(path))
	content, err := nginx.RenderExistingConfigWithHostsNamed(name, name, domain, fullWeb, php, app, int(port.Int64), hosts)
	return name, content, err
}

// Tokenize configuration without executing nginx -T (which opens log files).
// Quoting and escapes remain significant: unknown equivalent custom syntax is
// refused rather than guessed at by an admission path.
func nginxTokens(s string) ([]string, error) {
	var result []string
	for i := 0; i < len(s); {
		if unicode.IsSpace(rune(s[i])) {
			i++
			continue
		}
		if s[i] == '#' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		if strings.ContainsRune("{};", rune(s[i])) {
			result = append(result, s[i:i+1])
			i++
			continue
		}
		start := i
		var quote byte
		for i < len(s) {
			c := s[i]
			if c == '\\' {
				i += 2
				if i > len(s) {
					return nil, errors.New("invalid Nginx escape")
				}
				continue
			}
			if quote != 0 {
				if c == quote {
					quote = 0
				}
				i++
				continue
			}
			if c == '\'' || c == '"' {
				quote = c
				i++
				continue
			}
			if unicode.IsSpace(rune(c)) || strings.ContainsRune("{};#", rune(c)) {
				break
			}
			i++
		}
		if quote != 0 {
			return nil, errors.New("unterminated Nginx quote")
		}
		result = append(result, s[start:i])
	}
	return result, nil
}

// readNginxGraph proves inclusion in the http configuration and detects host
// claims from another included file. No daemon or configuration is changed.
func readNginxGraph(path string, stack []string, files map[string][]string, hosts map[string]string, active map[string]bool) error {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if err = trustedDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	if len(files) > 256 || active[real] {
		return errors.New("unsupported Nginx include graph")
	}
	active[real] = true
	defer delete(active, real)
	b, err := readProtected(real, 2<<20)
	if err != nil {
		return err
	}
	tokens, err := nginxTokens(string(b))
	if err != nil {
		return err
	}
	guard := false
	if real == "/etc/nginx/sites-available/-fluxo-default" && len(stack) == 1 && stack[0] == "http" {
		for _, explicit := range []bool{false, true} {
			expected, e := nginx.RenderExistingDefaultServerConfig(explicit)
			if e == nil && sameNginxConfig(string(b), expected) == nil {
				guard = true
			}
		}
		if !guard {
			return errors.New("review the existing Fluxo unknown-host guard")
		}
	}
	if old, ok := files[real]; ok && !reflect.DeepEqual(old, stack) {
		return errors.New("Nginx file included in ambiguous contexts")
	}
	files[real] = append([]string{}, stack...)
	var directive []string
	depth := len(stack)
	for _, token := range tokens {
		switch token {
		case "{":
			if len(directive) == 0 {
				return errors.New("invalid Nginx block")
			}
			stack = append(stack, directive[0])
			directive = nil
		case "}":
			if len(directive) != 0 || len(stack) <= depth {
				return errors.New("invalid Nginx nesting")
			}
			stack = stack[:len(stack)-1]
		case ";":
			if len(directive) > 0 && directive[0] == "include" {
				if len(directive) != 2 || strings.ContainsAny(directive[1], "$\"'\\") {
					return errors.New("review custom Nginx include")
				}
				pattern := directive[1]
				if !filepath.IsAbs(pattern) {
					pattern = filepath.Join("/etc/nginx", pattern)
				}
				paths, e := filepath.Glob(pattern)
				if e != nil {
					return e
				}
				for _, p := range paths {
					if err = readNginxGraph(p, append([]string{}, stack...), files, hosts, active); err != nil {
						return err
					}
				}
			}
			if len(directive) > 0 && directive[0] == "server_name" {
				for _, name := range directive[1:] {
					if name == "_" || name == `""` {
						continue
					}
					if guard && name == "~^.*$" {
						continue
					}
					if strings.ContainsAny(name, "~*$\"'") || strings.HasPrefix(name, ".") {
						return errors.New("review custom wildcard/regex Nginx host claims")
					}
					if err = addNginxHostClaim(hosts, name, real); err != nil {
						return err
					}
				}
			}
			directive = nil
		default:
			directive = append(directive, token)
		}
	}
	if len(directive) != 0 || len(stack) != depth {
		return errors.New("incomplete Nginx configuration")
	}
	return nil
}

func addNginxHostClaim(hosts map[string]string, name, path string) error {
	name = strings.ToLower(name)
	if prior, ok := hosts[name]; ok && prior != path {
		return fmt.Errorf("Nginx hostname %s is claimed by multiple files", name)
	}
	hosts[name] = path
	return nil
}

func sameNginxConfig(actual, expected string) error {
	a, err := nginxTokens(actual)
	if err != nil {
		return err
	}
	b, err := nginxTokens(expected)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(a, b) {
		return errors.New("vhost differs from the current domain/root/runtime contract; preserve and review custom intent")
	}
	return nil
}

func nginxHTTPIncluded(files map[string][]string, path string) bool {
	stack, ok := files[path]
	return ok && len(stack) == 1 && stack[0] == "http"
}
