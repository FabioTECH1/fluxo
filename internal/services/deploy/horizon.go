package deploy

import (
	"strings"

	"fluxo/internal/database"
)

const (
	HorizonDaemonName     = "Laravel Horizon"
	HorizonDaemonSelector = "(name = ? OR command LIKE '% artisan horizon' OR command LIKE '% artisan horizon %')"
	HorizonTerminateLine  = `cd "$FLUXO_ACTIVE_SITE_PATH" && $FLUXO_PHP artisan horizon:terminate`
)

func IsHorizonEnabled(siteID int) bool {
	var count int
	_ = database.DB.QueryRow("SELECT COUNT(*) FROM daemons WHERE site_id = ? AND "+HorizonDaemonSelector, siteID, HorizonDaemonName).Scan(&count)
	return count > 0
}

func WithHorizonTerminate(script string) string {
	for _, line := range strings.Split(strings.ReplaceAll(script, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == HorizonTerminateLine {
			return script
		}
	}
	return strings.TrimRight(script, "\r\n") + "\n\n" + HorizonTerminateLine + "\n"
}

func WithoutHorizonTerminate(script string) string {
	lines := strings.Split(strings.ReplaceAll(script, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == HorizonTerminateLine {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func ApplyHorizonDeploymentHook(script string, enabled bool) string {
	if !enabled {
		return script
	}
	return WithHorizonTerminate(script)
}
