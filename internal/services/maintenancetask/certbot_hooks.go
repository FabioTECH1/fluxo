package maintenancetask

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

const certbotDirectoryHookContract = "nginx-conditional-v1"
const certbotDirectoryHookExtendedContract = "nginx-conditional-v2"
const certbotDeployHookDirectory = "/etc/letsencrypt/renewal-hooks/deploy"

type certbotHook struct{ interpreter, family, contract string }

// This is a complete, static grammar, not a shell tokenizer or evaluator. Only
// one case arm, one quoted Certbot selector and the fixed Nginx test/reload are
// supported. In particular, no shell substitutions, delegation, environment
// assignments, functions, redirects, extra arms or commands can enter it.
var certbotHookCase = regexp.MustCompile(`\Acase ([^\n]+) in[ \t\n]+([^\n)]+)\)[ \t\n]*/usr/sbin/nginx -t[ \t]+&&[ \t\n]+/usr/bin/systemctl reload nginx[ \t\n]*;;[ \t\n]+esac\z`)
var certbotLineageName = regexp.MustCompile(`\A[A-Za-z0-9][A-Za-z0-9._-]{0,252}\z`)
var certbotHookName = regexp.MustCompile(`\A[A-Za-z0-9][A-Za-z0-9._-]{0,127}\z`)
var certbotDomainName = regexp.MustCompile(`\A[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+\z`)

func parseCertbotDirectoryHook(raw []byte) (certbotHook, error) {
	refused := errors.New("Certbot deploy hook differs from the reviewed conditional Nginx reload grammar")
	if len(raw) == 0 || len(raw) > 8192 {
		return certbotHook{}, refused
	}
	for _, c := range raw {
		if c != '\n' && c != '\t' && (c < 32 || c > 126) || c == '\\' {
			return certbotHook{}, refused
		}
	}
	lines := strings.Split(string(raw), "\n")
	var hook certbotHook
	hook.contract = certbotDirectoryHookContract
	switch lines[0] {
	case "#!/bin/sh":
		hook.interpreter = "/bin/sh"
	case "#!/usr/bin/sh":
		hook.interpreter = "/usr/bin/sh"
	default:
		return hook, refused
	}
	body := []string{}
	for _, line := range lines[1:] {
		line = strings.Trim(line, " \t")
		if line != "" && !strings.HasPrefix(line, "#") {
			body = append(body, line)
		}
	}
	if len(body) < 2 || body[0] != "set -eu" {
		return hook, refused
	}
	match := certbotHookCase.FindStringSubmatch(strings.Join(body[1:], "\n"))
	if match == nil {
		return hook, refused
	}
	selector, pattern := match[1], strings.Trim(match[2], " \t")
	if selector == `"${RENEWED_LINEAGE:-}"` {
		// Only the literal empty fallback is supported. Do not evaluate other
		// parameter operators, defaults, substitutions or variable names.
		selector = `"$RENEWED_LINEAGE"`
		hook.contract = certbotDirectoryHookExtendedContract
	}
	switch selector {
	case `"$RENEWED_LINEAGE"`, `"${RENEWED_LINEAGE}"`:
		if strings.HasPrefix(pattern, "*/") {
			name := strings.TrimPrefix(pattern, "*/")
			if !certbotLineageName.MatchString(name) || strings.Contains(name, "..") {
				return hook, refused
			}
			// One unquoted leading star followed by a literal slash/name.
			// The body remains the same fixed Nginx validation and reload.
			hook.family = "renewed-lineage-basename"
			hook.contract = certbotDirectoryHookExtendedContract
			break
		}
		literal := certbotHookLiteral(pattern)
		name := strings.TrimPrefix(literal, "/etc/letsencrypt/live/")
		if literal != "/etc/letsencrypt/live/"+name || !certbotLineageName.MatchString(name) || strings.Contains(name, "..") {
			return hook, refused
		}
		hook.family = "renewed-lineage-exact"
	case `"$RENEWED_DOMAINS"`, `"${RENEWED_DOMAINS}"`:
		if !certbotHookDomain(certbotHookLiteral(pattern)) {
			return hook, refused
		}
		hook.family = "renewed-domains-exact"
	case `" $RENEWED_DOMAINS "`, `" ${RENEWED_DOMAINS} "`:
		// Literal spaces enforce a complete domain within Certbot's space-
		// separated list. The two outside stars are the only permitted globs.
		if len(pattern) < 6 || !strings.HasPrefix(pattern, `*" `) || !strings.HasSuffix(pattern, ` "*`) || !certbotHookDomain(pattern[3:len(pattern)-3]) {
			return hook, refused
		}
		hook.family = "renewed-domains-member"
	default:
		return hook, refused
	}
	return hook, nil
}

func certbotHookLiteral(s string) string {
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[0] == s[len(s)-1] {
		return s[1 : len(s)-1]
	}
	return s
}

func certbotHookDomain(s string) bool {
	return len(s) <= 253 && certbotDomainName.MatchString(s)
}

func validateCertbotHookMode(info os.FileInfo) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || st.Uid != 0 || st.Gid != 0 || st.Nlink != 1 || info.Mode().Perm()&0022 != 0 || info.Mode().Perm()&0100 == 0 || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return errors.New("Certbot deploy hook must be a protected root:root, single-link executable file")
	}
	return nil
}

// readRegular also checks every ancestor, refuses symlinks/hardlinks and checks
// descriptor/path stability. The extra mode check restricts executable hooks;
// its before/after identity cannot be swapped around that protected read.
func inspectCertbotDirectoryHook(p *profile, path string) (certbotHook, error) {
	return inspectCertbotHookFile(p, path, readRegular, os.Lstat)
}

func inspectCertbotHookFile(p *profile, path string, read func(string, int64) ([]byte, error), lstat func(string) (os.FileInfo, error)) (certbotHook, error) {
	// Certbot invokes the filename through a shell. A safe script body cannot
	// authorize shell syntax in its pathname (including whitespace or quotes).
	if filepath.Dir(path) != certbotDeployHookDirectory || !certbotHookName.MatchString(filepath.Base(path)) || filepath.Clean(path) != path {
		return certbotHook{}, errors.New("Unsupported Certbot deploy hook path")
	}
	before, err := lstat(path)
	if err != nil {
		return certbotHook{}, err
	}
	if err = validateCertbotHookMode(before); err != nil {
		return certbotHook{}, err
	}
	raw, err := read(path, 8192)
	if err != nil {
		return certbotHook{}, err
	}
	after, err := lstat(path)
	if err != nil {
		return certbotHook{}, err
	}
	a, b := before.Sys().(*syscall.Stat_t), after.Sys().(*syscall.Stat_t)
	if a.Dev != b.Dev || a.Ino != b.Ino || a.Mode != b.Mode || a.Uid != b.Uid || a.Gid != b.Gid || a.Nlink != b.Nlink || a.Size != b.Size || a.Mtim != b.Mtim || a.Ctim != b.Ctim {
		return certbotHook{}, errors.New("Certbot deploy hook changed during inspection")
	}
	hook, err := parseCertbotDirectoryHook(raw)
	if err != nil {
		return hook, err
	}
	p.Files[path] = digest(raw)
	p.External["certbot-directory-hook:"+path] = hook.family + ":" + hook.interpreter
	return hook, nil
}

func qualifyCertbotDirectoryHooks(ctx context.Context, p *profile) error {
	hasHooks := false
	contract := certbotDirectoryHookContract
	for _, dir := range []string{"/etc/letsencrypt/renewal-hooks/pre", "/etc/letsencrypt/renewal-hooks/post", certbotDeployHookDirectory} {
		entries, err := p.directory(dir, true)
		if err != nil {
			return err
		}
		if len(entries) != 0 && dir != certbotDeployHookDirectory {
			return fmt.Errorf("Custom Certbot directory hooks need maintenance review: %s", dir)
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			hook, inspectErr := inspectCertbotDirectoryHook(p, path)
			if inspectErr != nil {
				return fmt.Errorf("Custom Certbot directory hook needs maintenance review: %s: %w", path, inspectErr)
			}
			if hook.contract == certbotDirectoryHookExtendedContract {
				contract = certbotDirectoryHookExtendedContract
			}
			hasHooks = true
		}
	}
	if !hasHooks {
		return nil // Keep existing empty-hook profile observations unchanged.
	}
	if err := qualifyCertbotHookPrograms(ctx, p); err != nil {
		return err
	}
	p.External["certbot-directory-hook-contract"] = contract
	// qualifyCertbot subsequently calls qualifyNginx: its packaged service,
	// master, executable, module/dependency closure and recursive configuration
	// remain mandatory even when the hook's selector would not match today.
	return nil
}

func qualifyCertbotHookPrograms(ctx context.Context, p *profile) error {
	// Certbot launches directory hooks using /bin/sh even for /usr/bin/sh
	// shebangs. Support only Ubuntu's merged-/usr, dash-backed shell layout.
	if err := trustedDatabaseDirectory("/usr/bin"); err != nil {
		return err
	}
	for path, targets := range map[string][]string{"/bin": {"usr/bin", "/usr/bin"}, "/usr/bin/sh": {"dash"}} {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || st.Gid != 0 || info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("Certbot hook shell link needs maintenance review: %s", path)
		}
		target, err := os.Readlink(path)
		if err != nil {
			return err
		}
		allowed := false
		for _, value := range targets {
			allowed = allowed || value == target
		}
		if !allowed {
			return fmt.Errorf("Certbot hook shell link needs maintenance review: %s", path)
		}
		p.External["certbot-hook-link:"+path] = target
	}
	if err := packageInputs(ctx, p, "dash", "systemd"); err != nil {
		return err
	}
	for _, path := range []string{"/usr/bin/dash", "/usr/bin/systemctl"} {
		raw, err := readRegular(path, 128<<20)
		if err != nil {
			return err
		}
		if p.Files[path] != digest(raw) {
			return fmt.Errorf("Certbot hook program is outside its verified package payload: %s", path)
		}
	}
	return nil
}
