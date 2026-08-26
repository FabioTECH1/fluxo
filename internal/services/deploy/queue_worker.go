package deploy

import (
	"strings"

	"fluxo/internal/database"
)

const (
	QueueWorkerDaemonName  = "Laravel Queue Worker"
	QueueWorkerManagedKind = "laravel_queue"
	QueueRestartMarker     = "# Fluxo managed Laravel Queue Worker restart"
	QueueRestartLine       = `cd "$FLUXO_ACTIVE_SITE_PATH" && $FLUXO_PHP artisan queue:restart`
)

func IsQueueWorkerEnabled(siteID int) bool {
	var count int
	_ = database.DB.QueryRow(`SELECT COUNT(*) FROM daemons
		WHERE site_id = ? AND COALESCE(managed_kind, '') = ?`, siteID, QueueWorkerManagedKind).Scan(&count)
	return count > 0
}

func WithQueueRestart(script string) string {
	for _, line := range strings.Split(strings.ReplaceAll(script, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == QueueRestartMarker || trimmed == QueueRestartLine {
			return script
		}
	}
	return strings.TrimRight(script, "\r\n") + "\n\n" + QueueRestartMarker + "\n" + QueueRestartLine + "\n"
}

func WithoutQueueRestart(script string) string {
	normalized := strings.ReplaceAll(script, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	markerFound := false
	for _, line := range lines {
		if strings.TrimSpace(line) == QueueRestartMarker {
			markerFound = true
			break
		}
	}
	if !markerFound {
		return script
	}

	kept := make([]string, 0, len(lines))
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		if strings.TrimSpace(line) == QueueRestartMarker {
			if index+1 < len(lines) && strings.TrimSpace(lines[index+1]) == QueueRestartLine {
				index++
			}
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func ApplyQueueWorkerDeploymentHook(script string, enabled bool) string {
	if !enabled {
		return WithoutQueueRestart(script)
	}
	return WithQueueRestart(script)
}
