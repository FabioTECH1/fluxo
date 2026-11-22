package nginx

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unicode"

	"fluxo/internal/sitepolicy"
	"golang.org/x/crypto/bcrypt"
)

var networkUsername = regexp.MustCompile(`^[A-Za-z0-9_.@-]{1,64}$`)
var networkPath = regexp.MustCompile(`^/[A-Za-z0-9/_.~+-]*$`)
var networkHash = regexp.MustCompile(`^\$2a\$12\$[./A-Za-z0-9]{53}$`)

const networkAuthRoot = "/etc/nginx/fluxo-auth"

func ValidateProtection(username, password string) error {
	if !networkUsername.MatchString(username) {
		return errors.New("Username must contain 1–64 letters, numbers, _, ., @ or -")
	}
	for _, r := range password {
		if unicode.IsControl(r) {
			return errors.New("Password must not contain control characters")
		}
	}
	if len(password) < 8 || len(password) > 72 || strings.ContainsAny(password, "\x00\r\n") {
		return errors.New("Password must contain 8–72 UTF-8 bytes without control line breaks")
	}
	return nil
}
func HashProtection(password string) (string, error) {
	b, e := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(b), e
}
func ValidateRedirect(source, destination string, status int) error {
	if len(source) > 1024 || !networkPath.MatchString(source) || strings.HasPrefix(source, "//") || strings.TrimSuffix(path.Clean(source), "/") != strings.TrimSuffix(source, "/") {
		return errors.New("Use a canonical exact source path without query, encoding or regular expressions")
	}
	if source == "/.well-known/acme-challenge" || strings.HasPrefix(source, "/.well-known/acme-challenge/") {
		return errors.New("ACME challenge paths cannot be redirected")
	}
	if status != 301 && status != 302 {
		return errors.New("Redirect type must be 301 or 302")
	}
	if destination == "" || len(destination) > 2048 || strings.ContainsAny(destination, "\r\n\t\x00\"'\\$;{}") {
		return errors.New("Invalid redirect destination")
	}
	for _, r := range destination {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return errors.New("Invalid redirect destination")
		}
	}
	u, e := url.Parse(destination)
	if e != nil || u.User != nil || u.Opaque != "" {
		return errors.New("Use a local path or an http/https URL without credentials")
	}
	if strings.HasPrefix(destination, "/") {
		if strings.HasPrefix(destination, "//") || u.Host != "" || path.Clean(u.Path) == path.Clean(source) {
			return errors.New("Redirect destination must be local and must not loop back to its source")
		}
		return nil
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" {
		return errors.New("Use a local path or an http/https URL")
	}
	if net.ParseIP(u.Hostname()) == nil && !regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9.-]*[a-zA-Z0-9])?$`).MatchString(u.Hostname()) {
		return errors.New("Invalid redirect destination host")
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return errors.New("Invalid redirect destination port")
		}
	}
	return nil
}
func ValidateNetwork(s sitepolicy.SiteNetworkSettings) error {
	if s.Format != 1 || len(s.Redirects) > 100 || len(s.Wildcards) > 100 {
		return errors.New("Unsupported network settings")
	}
	if s.Enabled && (!networkUsername.MatchString(s.Username) || !networkHash.MatchString(s.PasswordHash)) {
		return errors.New("Saved protection credentials need recovery")
	}
	seen := map[string]bool{}
	ids := map[string]bool{}
	for _, r := range s.Redirects {
		if r.ID == "" || seen[r.Source] || ids[r.ID] {
			return errors.New("Duplicate redirect source")
		}
		seen[r.Source] = true
		ids[r.ID] = true
		if e := ValidateRedirect(r.Source, r.Destination, r.Status); e != nil {
			return e
		}
	}
	return nil
}
func AuthFilePath(siteID int, s sitepolicy.SiteNetworkSettings) string {
	sum := sha256.Sum256([]byte(s.Username + ":" + s.PasswordHash + "\n"))
	return filepath.Join(networkAuthRoot, fmt.Sprintf("site-%d-%s.htpasswd", siteID, hex.EncodeToString(sum[:])))
}

// Immutable, protected credential files allow failed activation/crash recovery
// to keep the previous password usable. Neither filenames nor contents contain
// the plaintext. Old files remain referenced by retained operation evidence.
func EnsureAuthFile(siteID int, s sitepolicy.SiteNetworkSettings) error {
	if !s.Enabled {
		return nil
	}
	for p := filepath.Dir(networkAuthRoot); ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil {
			return e
		}
		v, ok := st.Sys().(*syscall.Stat_t)
		if !ok || !st.IsDir() || v.Uid != 0 || st.Mode().Perm()&0022 != 0 {
			return errors.New("Unsafe credential storage parent")
		}
		if p == "/" {
			break
		}
	}
	return ensureAuthFile(networkAuthRoot, siteID, s, 0, 33)
}
func ensureAuthFile(dir string, siteID int, s sitepolicy.SiteNetworkSettings, uid, gid int) error {
	if !s.Enabled {
		return nil
	}
	if siteID <= 0 {
		return errors.New("Invalid site")
	}
	if e := ValidateNetwork(s); e != nil {
		return e
	}
	created := false
	if e := os.Mkdir(dir, 0750); e == nil {
		created = true
	} else if !os.IsExist(e) {
		return e
	}
	st, e := os.Lstat(dir)
	if e != nil {
		return e
	}
	v, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.IsDir() || int(v.Uid) != uid || st.Mode().Perm()&0022 != 0 {
		return errors.New("Unsafe Nginx credential directory")
	}
	marker := filepath.Join(dir, ".fluxo-network-v1")
	if !created {
		fd, me := syscall.Open(marker, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		if me != nil {
			return errors.New("Credential storage is not initialized by Fluxo; review it over SSH")
		}
		f := os.NewFile(uintptr(fd), marker)
		info, me := f.Stat()
		if me != nil {
			f.Close()
			return me
		}
		v, ok := info.Sys().(*syscall.Stat_t)
		body, me := io.ReadAll(io.LimitReader(f, 128))
		f.Close()
		if !ok || !info.Mode().IsRegular() || int(v.Uid) != uid || v.Nlink != 1 || info.Mode().Perm() != 0600 || me != nil || string(body) != "Fluxo managed site credentials v1\n" {
			return errors.New("Unsafe credential storage marker")
		}
	} else {
		f, me := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if me != nil {
			return me
		}
		if _, me = f.WriteString("Fluxo managed site credentials v1\n"); me == nil {
			me = f.Sync()
		}
		f.Close()
		if me != nil {
			return me
		}
	}
	if e = os.Chown(dir, uid, gid); e != nil {
		return e
	}
	if e = os.Chmod(dir, 0750); e != nil {
		return e
	}
	body := []byte(s.Username + ":" + s.PasswordHash + "\n")
	filename := filepath.Join(dir, filepath.Base(AuthFilePath(siteID, s)))
	fd, e := syscall.Open(filename, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0640)
	if os.IsExist(e) {
		readFD, e := syscall.Open(filename, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		f := os.NewFile(uintptr(readFD), filename)
		if e != nil {
			return e
		}
		defer f.Close()
		info, e := f.Stat()
		if e != nil {
			return e
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || int(stat.Uid) != uid || int(stat.Gid) != gid || stat.Nlink != 1 || info.Mode().Perm() != 0640 {
			return errors.New("Unsafe existing credential file")
		}
		b, e := io.ReadAll(io.LimitReader(f, 1024))
		if e != nil || string(b) != string(body) {
			return errors.New("Credential file differs from saved state")
		}
		return nil
	}
	if e != nil {
		return e
	}
	f := os.NewFile(uintptr(fd), filename)
	defer f.Close()
	if e = f.Chown(uid, gid); e == nil {
		e = f.Chmod(0640)
	}
	if e == nil {
		_, e = f.Write(body)
	}
	if e == nil {
		e = f.Sync()
	}
	if e != nil {
		return e
	}
	d, e := os.Open(dir)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}

// ApplyNetworkSettings operates only on freshly generated managed server blocks.
// Redirect matching uses the original path (ignoring query), so internal PHP
// front-controller redirects cannot accidentally match a different user path.
// Inputs are exact literals; no user-provided regex or Nginx variables exist.
func ApplyNetworkSettings(config string, siteID int, s sitepolicy.SiteNetworkSettings) (string, error) {
	if e := ValidateNetwork(s); e != nil {
		return "", e
	}
	if !s.Enabled && len(s.Redirects) == 0 {
		return config, nil
	}
	tokens, e := lexTrafficConfig(config)
	if e != nil {
		return "", e
	}
	tree, e := parseTrafficSyntax(tokens, true)
	if e != nil {
		return "", e
	}
	type insertion struct {
		at   int
		text string
	}
	var inserts []insertion
	for _, server := range tree.children {
		if server.name != "server" {
			return "", errors.New("Network settings require managed server blocks")
		}
		var text strings.Builder
		if s.Enabled {
			fmt.Fprintf(&text, "\n    auth_basic \"Protected site\";\n    auth_basic_user_file %s;\n", AuthFilePath(siteID, s))
		}
		// WWW redirect and TLS guard blocks have no application content. Preserve
		// their existing routing rather than folding path rules into WWW behavior.
		app := false
		for _, child := range server.children {
			if child.name == "location" {
				for _, d := range child.directives {
					if d[0].value == "try_files" || d[0].value == "proxy_pass" || d[0].value == "fastcgi_pass" {
						app = true
					}
				}
			}
		}
		if app {
			for _, r := range s.Redirects {
				literal := strings.NewReplacer(".", "[.]", "+", "[+]").Replace(r.Source)
				fmt.Fprintf(&text, "    if ($request_uri ~ \"^%s([?].*)?$\") { return %d %s; }\n", literal, r.Status, strconv.Quote(r.Destination))
			}
		}
		inserts = append(inserts, insertion{server.open, text.String()})
		if s.Enabled {
			for _, child := range server.children {
				prefix := config[child.start:child.open]
				if child.name == "location" && strings.Contains(prefix, "/.well-known/acme-challenge/") {
					inserts = append(inserts, insertion{child.open, "\n        auth_basic off;"})
				}
			}
		}
	}
	sort.Slice(inserts, func(i, j int) bool { return inserts[i].at > inserts[j].at })
	for _, in := range inserts {
		config = config[:in.at] + in.text + config[in.at:]
	}
	return config, nil
}

// ServerNames reads only server_name directives, never location regexes. It is
// used to preserve explicit operator hostname ownership without adopting rules.
func ServerNames(raw string) ([]string, error) {
	tokens, e := lexTrafficConfig(raw)
	if e != nil {
		return nil, e
	}
	tree, e := parseTrafficSyntax(tokens, false)
	if e != nil {
		return nil, e
	}
	out := []string{}
	var walk func(*trafficBlock)
	walk = func(b *trafficBlock) {
		if b.name == "server" {
			for _, d := range b.directives {
				if d[0].value == "server_name" {
					for _, v := range d[1:] {
						out = append(out, v.value)
					}
				}
			}
		}
		for _, child := range b.children {
			walk(child)
		}
	}
	walk(tree)
	return out, nil
}
