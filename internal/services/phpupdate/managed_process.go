package phpupdate

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const ManagedProcessPolicy = 2
const managedProcessProtocol = "managed-service-v1"

// An omitted policy retains historical natural-only handling. It never grants
// permission to signal an old, already-reviewed custom command.
type ManagedStopPolicy struct {
	Signal  string `json:"signal"`
	Seconds int    `json:"seconds"`
}

func (p *ManagedStopPolicy) Validate() error {
	if p == nil {
		return nil
	}
	if p.Seconds < 1 || p.Seconds > 86400 {
		return errors.New("Managed process shutdown timeout must be between 1 and 86400 seconds")
	}
	switch p.Signal {
	case "SIGHUP", "SIGINT", "SIGQUIT", "SIGTERM":
		return nil
	default:
		return errors.New("Managed process maintenance requires a graceful stop signal; SIGKILL is not used automatically")
	}
}

func isManagedProcess(c *Consumer) bool {
	return c != nil && c.Kind == "" && c.Protocol == managedProcessProtocol
}

func managedStopPolicy(c *Consumer) (*ManagedStopPolicy, error) {
	if !isManagedProcess(c) {
		return nil, nil
	}
	p := &ManagedStopPolicy{Signal: c.StopSignal, Seconds: c.StopSeconds}
	return p, p.Validate()
}

func managedSignal(p *ManagedStopPolicy) string {
	if p != nil {
		return p.Signal
	}
	return "SIGTERM"
}

func ManagedKillMode(p *ManagedStopPolicy) string {
	if p != nil {
		return "control-group"
	}
	return "mixed"
}

func systemdSignal(signal string) string {
	switch signal {
	case "1", "SIGHUP":
		return "SIGHUP"
	case "2", "SIGINT":
		return "SIGINT"
	case "3", "SIGQUIT":
		return "SIGQUIT"
	case "15", "SIGTERM":
		return "SIGTERM"
	}
	return signal
}

var managedTimePart = regexp.MustCompile(`^([0-9]+)(us|ms|s|min|h|d)$`)

func managedTimeoutMatches(value string, seconds int) bool {
	var total time.Duration
	units := map[string]time.Duration{"us": time.Microsecond, "ms": time.Millisecond, "s": time.Second, "min": time.Minute, "h": time.Hour, "d": 24 * time.Hour}
	for _, part := range strings.Fields(value) {
		m := managedTimePart.FindStringSubmatch(part)
		if m == nil {
			return false
		}
		n, err := strconv.ParseUint(m[1], 10, 32)
		if err != nil || n > uint64((24*time.Hour)/units[m[2]]) {
			return false
		}
		total += time.Duration(n) * units[m[2]]
		if total > 24*time.Hour {
			return false
		}
	}
	return total == time.Duration(seconds)*time.Second
}

// Direct declared PHP scripts and Artisan commands share their managed service
// shutdown policy without an Artisan action whitelist. Ambiguous shell/systemd
// expansion retains its existing review requirement.
func managedPHPArguments(branch, command string) ([]string, bool) {
	if !ValidBranch(branch) || len(command) > 2048 || strings.ContainsAny(command, "\\\"'\n\r\x00") {
		return nil, false
	}
	args := strings.Fields(command)
	if len(args) < 2 || len(args) > 128 || strings.HasPrefix(args[1], "-") || (args[0] != "php"+branch && args[0] != "/usr/bin/php"+branch) {
		return nil, false
	}
	for _, arg := range args[1:] {
		if !artisanLiteral.MatchString(arg) {
			return nil, false
		}
	}
	args[0] = "php" + branch
	return args, true
}

func customProcessProtocol(branch string, c *Consumer, site Site, policy int, signal string) {
	if policy == ManagedProcessPolicy && customArtisanSite(*c, site) {
		if _, ok := managedPHPArguments(branch, c.Command); ok {
			c.Protocol, c.StopSignal = managedProcessProtocol, strings.ToUpper(strings.TrimSpace(signal))
			if c.StopSignal == "" {
				c.StopSignal = "SIGTERM"
			}
			if c.StopSeconds <= 0 {
				c.StopSeconds = 15
			}
			return
		}
	}
	if qualifyCustomQueue(branch, *c, site) {
		c.Protocol = "laravel_queue"
	} else if customArtisanSite(*c, site) {
		if _, ok := customArtisanArguments(branch, c.Command); ok {
			c.Protocol = "artisan_natural"
		}
	}
}

func managedStopNote(c Consumer, label string) string {
	return fmt.Sprintf("Background process %s: send its configured %s signal to the service group and wait up to %d seconds, within the operation's remaining time budget. In-progress work follows the application's shutdown behaviour. Previously running processes restart after verification; stopped processes stay stopped. No forced kill is used.", label, c.StopSignal, c.StopSeconds)
}

func MaintenanceDrainWindow(units []MaintenanceRuntime) time.Duration {
	window := 5 * time.Minute
	for _, u := range units {
		if u.ManagedStop != nil && time.Duration(u.ManagedStop.Seconds)*time.Second > window {
			window = time.Duration(u.ManagedStop.Seconds) * time.Second
		}
	}
	return window
}

func MaintenanceStopExpired(u MaintenanceRuntime, elapsed time.Duration, label string) error {
	if u.ManagedStop == nil || elapsed < time.Duration(u.ManagedStop.Seconds)*time.Second {
		return nil
	}
	return fmt.Errorf("%s did not stop within %d seconds after %s. Maintenance remains paused; no work was force-killed. Let the application finish its shutdown, then review recovery", label, u.ManagedStop.Seconds, u.ManagedStop.Signal)
}

func inspectImpactForPlan(ctx context.Context, p Plan) (Impact, error) {
	return inspectImpactPolicy(ctx, p.Version, p.Impact.MaintenancePolicy)
}
