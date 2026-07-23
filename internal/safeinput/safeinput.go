package safeinput

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	domainRegex    = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+$`)
	phpVersionRe   = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)
	dbIdentRe      = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	repoFullNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	gitRefRe       = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._/-]*$`)
	ufwProfileRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,63}$`)
)

const ManagedSitesRoot = "/home/fluxo"

func HasControlChars(s string) bool {
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' || r < 0x20 {
			return true
		}
	}
	return false
}

func ValidateDomain(s string) bool {
	return domainRegex.MatchString(s)
}

func ValidatePHPVersion(s string) bool {
	return phpVersionRe.MatchString(s)
}

func ValidateRepoFullName(s string) bool {
	return repoFullNameRe.MatchString(s)
}

func ValidateDBIdent(s string) bool {
	return s != "" && dbIdentRe.MatchString(s)
}

func ValidateGitRef(s string) bool {
	if s == "" || HasControlChars(s) || strings.ContainsAny(s, " \t") {
		return false
	}
	if strings.HasPrefix(s, "-") || strings.Contains(s, "..") || strings.Contains(s, "@{") {
		return false
	}
	return gitRefRe.MatchString(s)
}

func ValidateCronUser(s string, allowRoot bool) bool {
	switch s {
	case "fluxo", "www-data":
		return true
	case "root":
		return allowRoot
	default:
		return false
	}
}

func ValidateSystemSignal(s string) bool {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "SIGHUP", "SIGINT", "SIGTERM", "SIGQUIT", "SIGKILL":
		return true
	default:
		return false
	}
}

func ValidatePortNumber(port int) bool {
	return port > 0 && port <= 65535
}

func ValidateFirewallAction(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "allow", "deny":
		return true
	default:
		return false
	}
}

func ValidateFirewallPortSpec(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || HasControlChars(s) || strings.HasPrefix(s, "-") {
		return false
	}

	base := s
	if strings.Contains(s, "/") {
		parts := strings.Split(s, "/")
		if len(parts) != 2 || (parts[1] != "tcp" && parts[1] != "udp") {
			return false
		}
		base = parts[0]
	}

	if strings.Contains(base, ":") {
		parts := strings.Split(base, ":")
		if len(parts) != 2 {
			return false
		}
		start, err1 := strconv.Atoi(parts[0])
		end, err2 := strconv.Atoi(parts[1])
		return err1 == nil && err2 == nil && ValidatePortNumber(start) && ValidatePortNumber(end) && start <= end
	}

	if n, err := strconv.Atoi(base); err == nil {
		return ValidatePortNumber(n)
	}

	return ufwProfileRe.MatchString(s)
}

func ValidateFirewallSource(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "Any") || strings.EqualFold(s, "Anywhere") {
		return true
	}
	if HasControlChars(s) || strings.HasPrefix(s, "-") {
		return false
	}
	if ip := net.ParseIP(s); ip != nil {
		return true
	}
	_, _, err := net.ParseCIDR(s)
	return err == nil
}

func GenerateSecretHex(bytesLen int) (string, error) {
	if bytesLen <= 0 {
		bytesLen = 32
	}
	b := make([]byte, bytesLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func EscapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func NormalizeWebRoot(siteDir, webRoot string) (string, error) {
	root := strings.TrimSpace(webRoot)
	if root == "" || root == "/" {
		return siteDir, nil
	}
	if HasControlChars(root) {
		return "", fmt.Errorf("invalid web root")
	}
	if strings.HasPrefix(root, "~/") || strings.Contains(root, "\\") {
		return "", fmt.Errorf("invalid web root")
	}

	clean := filepath.Clean(root)
	if clean == "." || clean == "/" {
		return siteDir, nil
	}
	if filepath.IsAbs(clean) {
		clean = strings.TrimPrefix(clean, string(filepath.Separator))
	}
	if clean == "" || clean == "." || strings.Contains(clean, "..") {
		return "", fmt.Errorf("invalid web root")
	}

	resolved := filepath.Join(siteDir, clean)
	rel, err := filepath.Rel(siteDir, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid web root")
	}
	return resolved, nil
}

// NormalizeManagedSitePath validates a stored site root without tying it to
// the site's current public domain. The directory name remains stable when a
// domain alias is promoted to primary.
func NormalizeManagedSitePath(storedPath string) (string, error) {
	clean := filepath.Clean(strings.TrimSpace(storedPath))
	if storedPath == "" || clean != storedPath || !filepath.IsAbs(clean) {
		return "", fmt.Errorf("invalid managed site path")
	}
	if filepath.Dir(clean) != ManagedSitesRoot || !ValidateDomain(filepath.Base(clean)) {
		return "", fmt.Errorf("site path is outside the managed site directory")
	}
	return clean, nil
}

func ValidateCronExpression(expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" || HasControlChars(expr) {
		return false
	}
	if strings.HasPrefix(expr, "@") {
		switch expr {
		case "@yearly", "@annually", "@monthly", "@weekly", "@daily", "@hourly", "@reboot":
			return true
		default:
			return false
		}
	}
	return len(strings.Fields(expr)) == 5
}
