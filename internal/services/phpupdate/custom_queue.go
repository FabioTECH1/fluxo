package phpupdate

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	sitepkg "fluxo/internal/services/site"
)

var queueConnection = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
var queueName = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
var queueInteger = regexp.MustCompile(`^[0-9]+$`)
var queueOptionBounds = map[string][2]int{"--sleep": {0, 60}, "--tries": {0, 100}, "--timeout": {1, 86400}, "--backoff": {0, 86400}, "--memory": {32, 4096}, "--max-time": {0, 86400}}

// This is a maintenance protocol, not ownership adoption. Keep Kind empty for
// custom records. Never rewrite their command, settings, feature config or unit.
func qualifyCustomQueue(branch string, c Consumer, site Site) bool {
	if !customArtisanSite(c, site) {
		return false
	}
	_, ok := customQueueArguments(branch, c.Command)
	return ok
}

func customArtisanSite(c Consumer, site Site) bool {
	return c.Kind == "" && c.SiteID > 0 && c.SiteID == site.ID && (site.AppType == "laravel" || site.AppType == "php") && filepath.IsAbs(site.Path) && strings.HasPrefix(filepath.Clean(site.Path), "/home/fluxo/") && c.Directory == sitepkg.ActiveSitePath(site.Path, site.Strategy)
}

var artisanAction = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]*(?::[A-Za-z0-9_.-]+)*$`)
var artisanLiteral = regexp.MustCompile(`^[A-Za-z0-9_./:@=,+-]+$`)

// Other direct Artisan invocations need no guessed shutdown protocol: their
// entire execution domain must exit naturally before packages can change.
// Literal arguments only; wrappers, interpreter flags and systemd/shell
// expansion are not silently normalized into a different invocation.
func customArtisanArguments(branch, command string) ([]string, bool) {
	if !ValidBranch(branch) || len(command) > 2048 || strings.ContainsAny(command, "\\\"'\n\r\x00") {
		return nil, false
	}
	args := strings.Fields(command)
	if len(args) < 3 || len(args) > 128 || (args[0] != "php"+branch && args[0] != "/usr/bin/php"+branch) || args[1] != "artisan" || !artisanAction.MatchString(args[2]) {
		return nil, false
	}
	for _, arg := range args[3:] {
		if !artisanLiteral.MatchString(arg) {
			return nil, false
		}
	}
	args[0] = "php" + branch
	return args, true
}

func isNaturalConsumer(c *Consumer) bool {
	return c != nil && c.Kind == "" && c.Protocol == "artisan_natural"
}

func isQueueConsumer(c *Consumer) bool {
	return c != nil && (c.Kind == "laravel_queue" && c.Protocol == "" || c.Kind == "" && c.Protocol == "laravel_queue")
}

// Recognize only the fixed Artisan queue:work grammar already exposed by the
// managed feature, including older commands which use the application's default
// connection. Names, substrings, wrappers, shell syntax and unknown options do
// not establish a graceful queue lifecycle. Live unit/process checks follow.
func customQueueArguments(branch, command string) ([]string, bool) {
	if !ValidBranch(branch) || len(command) > 2048 || strings.ContainsAny(command, "\\\"'\n\r\x00") {
		return nil, false
	}
	args := strings.Fields(command)
	if len(args) < 3 || (args[0] != "php"+branch && args[0] != "/usr/bin/php"+branch) || args[1] != "artisan" || args[2] != "queue:work" {
		return nil, false
	}
	args[0] = "php" + branch
	i := 3
	if i < len(args) && !strings.HasPrefix(args[i], "--") {
		connection := args[i]
		if !queueConnection.MatchString(connection) || strings.EqualFold(connection, "sync") || strings.EqualFold(connection, "null") {
			return nil, false
		}
		i++
	}
	seen := map[string]bool{}
	for _, option := range args[i:] {
		key, value, hasValue := strings.Cut(option, "=")
		if seen[key] {
			return nil, false
		}
		seen[key] = true
		if key == "--force" && !hasValue {
			continue
		}
		if !hasValue {
			return nil, false
		}
		if key == "--queue" {
			queues := strings.Split(value, ",")
			if len(queues) > 20 {
				return nil, false
			}
			for _, name := range queues {
				if !queueName.MatchString(name) {
					return nil, false
				}
			}
			continue
		}
		bounds, ok := queueOptionBounds[key]
		if !ok || !queueInteger.MatchString(value) {
			return nil, false
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < bounds[0] || n > bounds[1] {
			return nil, false
		}
	}
	return args, true
}
