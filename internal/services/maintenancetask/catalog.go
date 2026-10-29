// Package maintenancetask coordinates Fluxo's fixed system maintenance jobs.
// Root execution is never authorized by a name or an arbitrary database command.
package maintenancetask

import (
	"fluxo/internal/safeinput"
	sitepkg "fluxo/internal/services/site"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
)

type Kind string

const (
	PackageCleanup            Kind = "package-cleanup"
	CertificateRenewal        Kind = "certificate-renewal"
	ComposerUpdate            Kind = "composer-update"
	ApplicationJob            Kind = "application-job"
	PackageCleanupCommand          = "/usr/bin/flock -w 120 /run/lock/fluxo-packages.lock /usr/bin/apt-get autoremove -y && /usr/bin/flock -w 120 /run/lock/fluxo-packages.lock /usr/bin/apt-get autoclean"
	CertificateRenewalCommand      = "certbot renew --quiet"
	ComposerUpdateCommand          = "/usr/bin/flock -n /run/lock/fluxo-composer.lock /usr/bin/env COMPOSER_ALLOW_SUPERUSER=1 /usr/local/bin/composer self-update --2 --stable --no-interaction --no-ansi"
)

type Contract struct {
	ID         int    `json:"id"`
	Kind       Kind   `json:"kind"`
	Expression string `json:"expression"`
	Command    string `json:"command"`
	SiteID     int    `json:"site_id"`
	User       string `json:"user"`
	Directory  string `json:"directory"`
}

// Qualify retains arbitrary valid schedules but only the exact three generated
// root/global command contracts. Missing or orphaned site rows are not globals.
func Qualify(id, siteID int, expression, command, user string) (Contract, bool) {
	if id <= 0 || siteID != 0 || user != "root" || !safeinput.ValidateCronExpression(expression) {
		return Contract{}, false
	}
	var kind Kind
	switch command {
	case PackageCleanupCommand:
		kind = PackageCleanup
	case CertificateRenewalCommand:
		kind = CertificateRenewal
	case ComposerUpdateCommand:
		kind = ComposerUpdate
	default:
		return Contract{}, false
	}
	return Contract{ID: id, Kind: kind, Expression: expression, Command: command, User: "root", Directory: "/root"}, true
}

var directoryPattern = regexp.MustCompile(`^/[A-Za-z0-9_./-]+$`)

// QualifyApplication is name-independent. The fixed root helper drops to the
// recorded application account before evaluating its intentionally supplied
// cron shell command; arbitrary root commands never enter this contract.
func QualifyApplication(id, siteID int, directory, expression, command, username string) (Contract, bool) {
	if id <= 0 || siteID < 0 || !safeinput.ValidateCronUser(username, false) || !safeinput.ValidateCronExpression(expression) || command == "" || len(command) > 65536 || safeinput.HasControlChars(command) {
		return Contract{}, false
	}
	if siteID == 0 {
		u, err := user.Lookup(username)
		if err != nil || u.Uid == "0" {
			return Contract{}, false
		}
		if directory == "" {
			directory = u.HomeDir
		}
		if directory != u.HomeDir {
			return Contract{}, false
		}
	} else if !strings.HasPrefix(directory, "/home/fluxo/") {
		return Contract{}, false
	}
	if !directoryPattern.MatchString(directory) || filepath.Clean(directory) != directory {
		return Contract{}, false
	}
	return Contract{ID: id, Kind: ApplicationJob, SiteID: siteID, Directory: directory, User: username, Command: command, Expression: expression}, true
}

func validContract(c Contract) bool {
	if c.Kind == ApplicationJob {
		return c.ID > 0 && c.SiteID >= 0 && safeinput.ValidateCronUser(c.User, false) && safeinput.ValidateCronExpression(c.Expression) && c.Command != "" && len(c.Command) <= 65536 && !safeinput.HasControlChars(c.Command) && directoryPattern.MatchString(c.Directory) && filepath.Clean(c.Directory) == c.Directory && (c.SiteID == 0 || strings.HasPrefix(c.Directory, "/home/fluxo/"))
	}
	var current Contract
	var ok bool
	current, ok = Qualify(c.ID, c.SiteID, c.Expression, c.Command, c.User)
	return ok && current == c
}

// QualifySite binds the actual active deployment layout. Orphan rows stay invalid.
func QualifySite(id, siteID int, path, strategy, appType, appDirectory, expression, command, username string) (Contract, bool) {
	if siteID == 0 {
		if c, ok := Qualify(id, siteID, expression, command, username); ok {
			return c, true
		}
		return QualifyApplication(id, siteID, "", expression, command, username)
	}
	if path == "" || appType == "" {
		return Contract{}, false
	}
	dir := sitepkg.ActiveSitePath(path, strategy)
	if appType == "python" {
		normalized, err := sitepkg.NormalizeAppDirectory(appDirectory)
		if err != nil {
			return Contract{}, false
		}
		dir = filepath.Join(dir, normalized)
	}
	return QualifyApplication(id, siteID, dir, expression, command, username)
}
