package releaseinfo

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	ComposerVersion = "2.10.2"
	ComposerSHA256  = "5ee7125f8a30a34d246cefdc0bc85b8a783b28f2aec968994118512350d28027"
	WPCLIVersion    = "2.12.0"
)

var toolVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func InstallerToolVersions() (string, string, string, error) {
	composer := strings.TrimSpace(ComposerVersion)
	composerSHA256 := strings.ToLower(strings.TrimSpace(ComposerSHA256))
	wpCLI := strings.TrimSpace(WPCLIVersion)
	if !toolVersionPattern.MatchString(composer) {
		return "", "", "", fmt.Errorf("invalid embedded Composer version %q", ComposerVersion)
	}
	if !sha256Pattern.MatchString(composerSHA256) {
		return "", "", "", fmt.Errorf("invalid embedded Composer SHA-256 %q", ComposerSHA256)
	}
	if !toolVersionPattern.MatchString(wpCLI) {
		return "", "", "", fmt.Errorf("invalid embedded WP-CLI version %q", WPCLIVersion)
	}
	return composer, composerSHA256, wpCLI, nil
}
