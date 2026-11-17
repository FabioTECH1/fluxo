package maintenancetask

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/syscmd"
)

func unitProperties(ctx context.Context, unit string, properties string) (map[string]string, error) {
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", unit, "--property="+properties)
	if err != nil {
		return nil, err
	}
	p := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			if _, exists := p[k]; exists {
				return nil, errors.New("Duplicate systemd property")
			}
			p[k] = v
		}
	}
	return p, nil
}

func qualifyNginx(ctx context.Context, p *profile) error {
	if err := packageInputs(ctx, p, "nginx", "nginx-common"); err != nil {
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
	return nginxConfig(p, "/etc/nginx/nginx.conf", visited, 0)
}

func nginxConfig(p *profile, path string, visited map[string]bool, depth int) error {
	if depth > 32 {
		return errors.New("Nginx include depth exceeds the supported profile")
	}
	if err := trustedDatabaseDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	p.External["nginx-include:"+path] = real
	if !strings.HasPrefix(real, "/etc/nginx/") {
		if _, known := jammyNginxModules[real]; known {
			return qualifyNginxVendorModule(p, real)
		}
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
			if err = nginxConfig(p, child, visited, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// These stock Jammy modules contain no embedded scripting interpreter. Other
// executable declarations still require an explicitly reviewed profile.
var jammyNginxModules = map[string]string{
	"/usr/share/nginx/modules-available/mod-http-geoip2.conf":       "ngx_http_geoip2_module.so",
	"/usr/share/nginx/modules-available/mod-http-image-filter.conf": "ngx_http_image_filter_module.so",
	"/usr/share/nginx/modules-available/mod-http-xslt-filter.conf":  "ngx_http_xslt_filter_module.so",
	"/usr/share/nginx/modules-available/mod-mail.conf":              "ngx_mail_module.so",
	"/usr/share/nginx/modules-available/mod-stream.conf":            "ngx_stream_module.so",
	"/usr/share/nginx/modules-available/mod-stream-geoip2.conf":     "ngx_stream_geoip2_module.so",
}

func nginxVendorModule(p *profile, path string, raw []byte) (string, error) {
	module, known := jammyNginxModules[path]
	if !known || !strings.HasPrefix(p.External["certbot-platform-profile"], "ubuntu-22.04:") || p.Files[path] != digest(raw) || string(raw) != "load_module modules/"+module+";\n" {
		return "", errors.New("Nginx module declaration differs from its reviewed packaged Jammy profile")
	}
	return module, nil
}

func qualifyNginxVendorModule(p *profile, path string) error {
	raw, err := readRegular(path, 4096)
	if err != nil {
		return err
	}
	module, err := nginxVendorModule(p, path, raw)
	if err != nil {
		return err
	}
	if p.External["nginx-module-prefix"] == "" {
		// -V exits before configuration parsing. The executable and its package
		// closure were verified first; use the clean maintenance environment.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var output bytes.Buffer
		if err = syscmd.RunMaintenance(ctx, "", &output, nil, "/usr/sbin/nginx", "-V"); err != nil {
			return err
		}
		if err = nginxModulePrefix(output.String()); err != nil {
			return err
		}
		p.External["nginx-module-prefix"] = "/usr/share/nginx"
		p.External["nginx-build-info"] = digest(output.Bytes())
	}
	runtimePath := "/usr/share/nginx/modules/" + module
	real, err := filepath.EvalSymlinks(runtimePath)
	if err != nil || real != "/usr/lib/nginx/modules/"+module {
		return errors.New("Nginx runtime module path differs from the reviewed package location")
	}
	payload, err := readRegularInput(real, 128<<20, true)
	if err != nil {
		return err
	}
	if p.Files[real] != digest(payload) {
		return errors.New("Nginx module payload is missing or changed from the verified package closure")
	}
	p.External["nginx-module:"+runtimePath] = real
	return nil
}

func nginxModulePrefix(buildInfo string) error {
	prefixes := []string{}
	for _, arg := range strings.Fields(buildInfo) {
		if strings.HasPrefix(arg, "--prefix=") {
			prefixes = append(prefixes, arg)
		}
	}
	if len(prefixes) != 1 || prefixes[0] != "--prefix=/usr/share/nginx" {
		return errors.New("Nginx compiled module prefix needs compatibility review")
	}
	return nil
}

// nginxStatements tokenizes syntax rather than searching comments/quoted data
// for executable directives. Block headers are separate statements too.
func nginxStatements(raw string) ([][]string, error) {
	var result [][]string
	var statement []string
	var token strings.Builder
	quote := rune(0)
	escaped := false
	comment := false
	flush := func() {
		if token.Len() > 0 {
			statement = append(statement, token.String())
			token.Reset()
		}
	}
	for _, c := range raw {
		if comment {
			if c == '\n' {
				comment = false
			}
			continue
		}
		if escaped {
			token.WriteRune(c)
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				token.WriteRune(c)
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '#':
			flush()
			comment = true
		case ' ', '\t', '\r', '\n':
			flush()
		case ';', '{', '}':
			flush()
			if len(statement) > 0 {
				result = append(result, statement)
				statement = nil
			}
		default:
			token.WriteRune(c)
		}
	}
	if quote != 0 || escaped || token.Len() > 0 || len(statement) > 0 {
		return nil, errors.New("Incomplete Nginx configuration syntax")
	}
	return result, nil
}
