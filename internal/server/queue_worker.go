package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/daemon"
	"fluxo/internal/services/deploy"
	sitepkg "fluxo/internal/services/site"
	"fluxo/internal/syscmd"
)

const (
	queueWorkerDaemonName  = deploy.QueueWorkerDaemonName
	queueWorkerManagedKind = deploy.QueueWorkerManagedKind
	queueRestartLine       = deploy.QueueRestartLine
)

var (
	queueConnectionPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	queueNamePattern       = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
)

type queueWorkerConfig struct {
	Connection     string `json:"connection"`
	Queues         string `json:"queues"`
	Processes      int    `json:"processes"`
	SleepSeconds   int    `json:"sleep_seconds"`
	Tries          int    `json:"tries"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	BackoffSeconds int    `json:"backoff_seconds"`
	MemoryMB       int    `json:"memory_mb"`
	MaxTimeSeconds int    `json:"max_time_seconds"`
	Force          bool   `json:"force"`
}

func defaultQueueWorkerConfig() queueWorkerConfig {
	return queueWorkerConfig{
		Connection:     "database",
		Queues:         "default",
		Processes:      1,
		SleepSeconds:   3,
		Tries:          3,
		TimeoutSeconds: 60,
		BackoffSeconds: 0,
		MemoryMB:       128,
		MaxTimeSeconds: 3600,
	}
}

func loadQueueWorkerConfig(siteID int) (queueWorkerConfig, bool, error) {
	config := defaultQueueWorkerConfig()
	var enabled int
	err := database.DB.QueryRow(`SELECT connection, queues, processes, sleep_seconds, tries,
		timeout_seconds, backoff_seconds, memory_mb, max_time_seconds, force, enabled
		FROM laravel_queue_workers WHERE site_id = ?`, siteID).Scan(
		&config.Connection, &config.Queues, &config.Processes, &config.SleepSeconds, &config.Tries,
		&config.TimeoutSeconds, &config.BackoffSeconds, &config.MemoryMB, &config.MaxTimeSeconds,
		&config.Force, &enabled,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return config, false, nil
	}
	return config, enabled != 0, err
}

func validateQueueWorkerConfig(config queueWorkerConfig) (queueWorkerConfig, error) {
	config.Connection = strings.TrimSpace(config.Connection)
	if !queueConnectionPattern.MatchString(config.Connection) || strings.EqualFold(config.Connection, "sync") || strings.EqualFold(config.Connection, "null") {
		return config, fmt.Errorf("choose an asynchronous queue connection")
	}

	queueNames := strings.Split(config.Queues, ",")
	normalizedQueues := make([]string, 0, len(queueNames))
	for _, queue := range queueNames {
		queue = strings.TrimSpace(queue)
		if queue == "" {
			continue
		}
		if !queueNamePattern.MatchString(queue) {
			return config, fmt.Errorf("queue names may only contain letters, numbers, dots, colons, underscores, and hyphens")
		}
		normalizedQueues = append(normalizedQueues, queue)
	}
	if len(normalizedQueues) == 0 {
		normalizedQueues = []string{"default"}
	}
	if len(normalizedQueues) > 20 {
		return config, fmt.Errorf("at most 20 queues may be configured")
	}
	config.Queues = strings.Join(normalizedQueues, ",")

	if config.Processes < 1 || config.Processes > 16 {
		return config, fmt.Errorf("processes must be between 1 and 16")
	}
	if config.SleepSeconds < 0 || config.SleepSeconds > 60 {
		return config, fmt.Errorf("sleep must be between 0 and 60 seconds")
	}
	if config.Tries < 0 || config.Tries > 100 {
		return config, fmt.Errorf("tries must be between 0 and 100")
	}
	if config.TimeoutSeconds < 1 || config.TimeoutSeconds > 86400 {
		return config, fmt.Errorf("timeout must be between 1 and 86400 seconds")
	}
	if config.BackoffSeconds < 0 || config.BackoffSeconds > 86400 {
		return config, fmt.Errorf("backoff must be between 0 and 86400 seconds")
	}
	if config.MemoryMB < 32 || config.MemoryMB > 4096 {
		return config, fmt.Errorf("memory must be between 32 and 4096 MB")
	}
	if config.MaxTimeSeconds < 0 || config.MaxTimeSeconds > 86400 {
		return config, fmt.Errorf("max runtime must be between 0 and 86400 seconds")
	}
	return config, nil
}

func queueWorkerCommand(phpVersion string, config queueWorkerConfig) string {
	parts := []string{
		"php" + phpVersion,
		"artisan",
		"queue:work",
		config.Connection,
		"--queue=" + config.Queues,
		"--sleep=" + strconv.Itoa(config.SleepSeconds),
		"--tries=" + strconv.Itoa(config.Tries),
		"--timeout=" + strconv.Itoa(config.TimeoutSeconds),
		"--backoff=" + strconv.Itoa(config.BackoffSeconds),
		"--memory=" + strconv.Itoa(config.MemoryMB),
	}
	if config.MaxTimeSeconds > 0 {
		parts = append(parts, "--max-time="+strconv.Itoa(config.MaxTimeSeconds))
	}
	if config.Force {
		parts = append(parts, "--force")
	}
	return strings.Join(parts, " ")
}

func saveQueueWorkerConfig(siteID, daemonID int, enabled bool, config queueWorkerConfig) error {
	_, err := database.DB.Exec(`INSERT INTO laravel_queue_workers
		(site_id, daemon_id, enabled, connection, queues, processes, sleep_seconds, tries,
		 timeout_seconds, backoff_seconds, memory_mb, max_time_seconds, force)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(site_id) DO UPDATE SET daemon_id = excluded.daemon_id,
		 enabled = excluded.enabled, connection = excluded.connection, queues = excluded.queues,
		 processes = excluded.processes, sleep_seconds = excluded.sleep_seconds, tries = excluded.tries,
		 timeout_seconds = excluded.timeout_seconds, backoff_seconds = excluded.backoff_seconds,
		 memory_mb = excluded.memory_mb, max_time_seconds = excluded.max_time_seconds,
		 force = excluded.force, updated_at = CURRENT_TIMESTAMP`,
		siteID, daemonID, enabled, config.Connection, config.Queues, config.Processes,
		config.SleepSeconds, config.Tries, config.TimeoutSeconds, config.BackoffSeconds,
		config.MemoryMB, config.MaxTimeSeconds, config.Force,
	)
	return err
}

func readDotEnvValue(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		name, value, found := strings.Cut(strings.TrimSpace(strings.TrimPrefix(trimmed, "export ")), "=")
		if !found || strings.TrimSpace(name) != key {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
			value = value[1 : len(value)-1]
		}
		return strings.TrimSpace(value)
	}
	return ""
}

func horizonQueueConnectionReady(sitePath string) error {
	connection := readDotEnvValue(filepath.Join(sitePath, ".env"), "QUEUE_CONNECTION")
	if connection == "" {
		connection = readDotEnvValue(filepath.Join(sitePath, ".env"), "QUEUE_DRIVER")
	}
	if connection != "redis" {
		return fmt.Errorf("Horizon requires QUEUE_CONNECTION=redis in the site environment")
	}
	return nil
}

func validateQueueWorkerBackend(ctx context.Context, siteID int, config queueWorkerConfig) error {
	var sitePath, phpVersion, strategy string
	if err := database.DB.QueryRow("SELECT path, php_version, deployment_strategy FROM sites WHERE id = ?", siteID).Scan(&sitePath, &phpVersion, &strategy); err != nil {
		return err
	}
	if phpVersion == "" {
		phpVersion = "8.4"
	}
	directory := sitepkg.ActiveSitePath(sitePath, strategy)
	php := "php" + phpVersion
	if _, err := syscmd.RunAsUserInDir(ctx, 30*time.Second, "fluxo", directory, php, "artisan", "config:clear"); err != nil {
		return fmt.Errorf("clear Laravel configuration cache: %w", err)
	}
	queueName := strings.Split(config.Queues, ",")[0]
	const queueProbe = `require 'vendor/autoload.php'; $app = require 'bootstrap/app.php'; $app->make(\Illuminate\Contracts\Console\Kernel::class)->bootstrap(); $queue = $app->make('queue')->connection(getenv('FLUXO_QUEUE_PREFLIGHT_CONNECTION')); $queue->size(getenv('FLUXO_QUEUE_PREFLIGHT_NAME'));`
	_, err := syscmd.RunEnvAsUserInDir(ctx, 30*time.Second, "fluxo", directory, []string{
		"FLUXO_QUEUE_PREFLIGHT_CONNECTION=" + config.Connection,
		"FLUXO_QUEUE_PREFLIGHT_NAME=" + queueName,
	}, php, "-r", queueProbe)
	return err
}

func addQueueRestartToDeployScript(siteID int) error {
	var script sql.NullString
	var strategy, appType, scriptMode string
	if err := database.DB.QueryRow("SELECT deploy_script, deployment_strategy, app_type, COALESCE(deploy_script_mode, 'legacy') FROM sites WHERE id = ?", siteID).Scan(&script, &strategy, &appType, &scriptMode); err != nil {
		return err
	}
	if scriptMode == deploy.ScriptModeManaged {
		return nil
	}
	current := script.String
	if current == "" {
		current = deploy.GenerateDeployScript(strategy, appType)
	}
	_, err := database.DB.Exec("UPDATE sites SET deploy_script = ? WHERE id = ?", deploy.WithQueueRestart(current), siteID)
	return err
}

func removeQueueRestartFromDeployScript(siteID int) error {
	var script sql.NullString
	var scriptMode string
	if err := database.DB.QueryRow("SELECT deploy_script, COALESCE(deploy_script_mode, 'legacy') FROM sites WHERE id = ?", siteID).Scan(&script, &scriptMode); err != nil {
		return err
	}
	if scriptMode == deploy.ScriptModeManaged || !script.Valid || script.String == "" {
		return nil
	}
	_, err := database.DB.Exec("UPDATE sites SET deploy_script = ? WHERE id = ?", deploy.WithoutQueueRestart(script.String), siteID)
	return err
}

func queueWorkerDaemonIDs(siteID int) ([]int, error) {
	rows, err := database.DB.Query("SELECT id FROM daemons WHERE site_id = ? AND COALESCE(managed_kind, '') = ? ORDER BY id", siteID, queueWorkerManagedKind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func disableQueueWorkerRuntime(ctx context.Context, siteID int) error {
	ids, err := queueWorkerDaemonIDs(siteID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := daemon.Delete(ctx, id); err != nil {
			return err
		}
		if _, err := database.DB.Exec("DELETE FROM daemons WHERE id = ?", id); err != nil {
			return err
		}
	}
	if err := removeQueueRestartFromDeployScript(siteID); err != nil {
		return err
	}
	_, err = database.DB.Exec("UPDATE laravel_queue_workers SET enabled = 0, daemon_id = 0, updated_at = CURRENT_TIMESTAMP WHERE site_id = ?", siteID)
	return err
}

func activateQueueWorker(ctx context.Context, siteID int, config queueWorkerConfig, updateEnvironment bool) (err error) {
	var sitePath, phpVersion, strategy string
	if err := database.DB.QueryRow("SELECT path, php_version, deployment_strategy FROM sites WHERE id = ?", siteID).Scan(&sitePath, &phpVersion, &strategy); err != nil {
		return err
	}
	if phpVersion == "" {
		phpVersion = "8.4"
	}
	directory := sitepkg.ActiveSitePath(sitePath, strategy)
	command := queueWorkerCommand(phpVersion, config)
	stopSeconds := config.TimeoutSeconds + 30

	var envSnapshot envFileSnapshot
	if updateEnvironment {
		envPath := filepath.Join(sitePath, ".env")
		envSnapshot, err = mergeEnvSettings(ctx, envPath, []envSetting{{key: "QUEUE_CONNECTION", value: config.Connection}})
		if err != nil {
			return fmt.Errorf("update queue environment: %w", err)
		}
		environmentCommitted := false
		defer func() {
			if !environmentCommitted && err != nil {
				restoreCtx, cancelRestore := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancelRestore()
				if restoreErr := restoreEnvFile(restoreCtx, envPath, envSnapshot); restoreErr != nil {
					log.Printf("Failed to restore queue environment for site %d: %v", siteID, restoreErr)
				}
			}
		}()
		if _, err = syscmd.RunAsUserInDir(ctx, 30*time.Second, "fluxo", directory, "php"+phpVersion, "artisan", "config:clear"); err != nil {
			return fmt.Errorf("clear Laravel configuration cache: %w", err)
		}
		defer func() {
			if err == nil {
				environmentCommitted = true
			}
		}()
	}

	res, err := database.DB.Exec(`INSERT INTO daemons
		(site_id, name, managed_kind, command, directory, user, instances, start_seconds, stop_seconds, stop_signal, restart_on_deploy)
		VALUES (?, ?, ?, ?, ?, 'fluxo', ?, 1, ?, 'SIGTERM', 1)`,
		siteID, queueWorkerDaemonName, queueWorkerManagedKind, command, directory, config.Processes, stopSeconds,
	)
	if err != nil {
		return err
	}
	daemonID64, err := res.LastInsertId()
	if err != nil {
		return err
	}
	daemonID := int(daemonID64)
	cleanup := func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), time.Minute)
		defer cancelCleanup()
		_ = daemon.Delete(cleanupCtx, daemonID)
		_, _ = database.DB.Exec("DELETE FROM daemons WHERE id = ?", daemonID)
	}
	if err = daemon.GenerateServiceFile(daemonID, command, directory, "fluxo", 1, stopSeconds, "SIGTERM"); err != nil {
		cleanup()
		return err
	}
	if err = daemon.EnableAndStart(ctx, daemonID); err != nil {
		cleanup()
		return err
	}
	if err = daemon.WaitHealthy(ctx, daemonID); err != nil {
		cleanup()
		return err
	}
	if err = addQueueRestartToDeployScript(siteID); err != nil {
		cleanup()
		return err
	}
	if err = saveQueueWorkerConfig(siteID, daemonID, true, config); err != nil {
		_ = removeQueueRestartFromDeployScript(siteID)
		cleanup()
		return err
	}
	if _, err = database.DB.Exec("UPDATE daemons SET status = 'active' WHERE id = ?", daemonID); err != nil {
		_ = removeQueueRestartFromDeployScript(siteID)
		cleanup()
		_ = saveQueueWorkerConfig(siteID, 0, false, config)
		return err
	}
	return nil
}

func restoreQueueWorker(ctx context.Context, siteID int, config queueWorkerConfig, updateEnvironment bool) error {
	if err := activateQueueWorker(ctx, siteID, config, updateEnvironment); err != nil {
		return fmt.Errorf("restore Laravel queue worker: %w", err)
	}
	return nil
}

func restoreQueueWorkerDetached(siteID int, config queueWorkerConfig, updateEnvironment bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return restoreQueueWorker(ctx, siteID, config, updateEnvironment)
}

func syncQueueWorkerDaemonForSite(ctx context.Context, siteID int) error {
	if !deploy.IsQueueWorkerEnabled(siteID) {
		return nil
	}
	config, _, err := loadQueueWorkerConfig(siteID)
	if err != nil {
		return err
	}
	config, err = validateQueueWorkerConfig(config)
	if err != nil {
		return err
	}
	var sitePath, phpVersion, appType, strategy string
	if err := database.DB.QueryRow("SELECT path, php_version, app_type, deployment_strategy FROM sites WHERE id = ?", siteID).Scan(&sitePath, &phpVersion, &appType, &strategy); err != nil {
		return err
	}
	if appType != "laravel" && appType != "php" {
		return fmt.Errorf("queue workers require a Laravel site")
	}
	if phpVersion == "" {
		phpVersion = "8.4"
	}
	directory := sitepkg.ActiveSitePath(sitePath, strategy)
	command := queueWorkerCommand(phpVersion, config)
	stopSeconds := config.TimeoutSeconds + 30
	ids, err := queueWorkerDaemonIDs(siteID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := database.DB.Exec(`UPDATE daemons SET name = ?, managed_kind = ?, command = ?,
			directory = ?, user = 'fluxo', instances = ?, start_seconds = 1,
			stop_seconds = ?, stop_signal = 'SIGTERM', restart_on_deploy = 1 WHERE id = ?`,
			queueWorkerDaemonName, queueWorkerManagedKind, command, directory, config.Processes, stopSeconds, id); err != nil {
			return err
		}
		if err := daemon.GenerateServiceFile(id, command, directory, "fluxo", 1, stopSeconds, "SIGTERM"); err != nil {
			return err
		}
		if err := daemon.Reload(ctx); err != nil {
			return err
		}
		if err := daemon.RestartAndWait(ctx, id); err != nil {
			return err
		}
	}
	return addQueueRestartToDeployScript(siteID)
}

// POST /api/v1/sites/{id}/features/queue-worker/enable
func (s *Server) handleEnableQueueWorker() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		if !requireLaravelSite(w, siteID) {
			return
		}
		if isHorizonEnabled(siteID) {
			http.Error(w, "Disable Laravel Horizon before enabling the standard queue worker", http.StatusConflict)
			return
		}

		var requested queueWorkerConfig
		if err := json.NewDecoder(r.Body).Decode(&requested); err != nil {
			http.Error(w, "Invalid queue worker settings", http.StatusBadRequest)
			return
		}
		config, err := validateQueueWorkerConfig(requested)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := validateQueueWorkerBackend(r.Context(), siteID, config); err != nil {
			log.Printf("Queue backend preflight failed for site %d: %v", siteID, err)
			http.Error(w, "The selected queue connection could not be used with the site's current configuration", http.StatusUnprocessableEntity)
			return
		}

		previous, wasEnabled, err := loadQueueWorkerConfig(siteID)
		if err != nil {
			http.Error(w, "Failed to load queue worker settings", http.StatusInternalServerError)
			return
		}
		wasEnabled = wasEnabled || deploy.IsQueueWorkerEnabled(siteID)
		if wasEnabled {
			if err := disableQueueWorkerRuntime(r.Context(), siteID); err != nil {
				http.Error(w, "Failed to stop the existing queue worker: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}

		if err := activateQueueWorker(r.Context(), siteID, config, true); err != nil {
			if wasEnabled {
				if restoreErr := restoreQueueWorkerDetached(siteID, previous, false); restoreErr != nil {
					log.Printf("Failed to restore queue worker for site %d: %v", siteID, restoreErr)
					http.Error(w, "Failed to update queue worker and restore its previous configuration", http.StatusInternalServerError)
					return
				}
			}
			http.Error(w, "Failed to enable queue worker: "+err.Error(), http.StatusInternalServerError)
			return
		}

		LogActivity(siteID, "feature", "Laravel queue worker enabled")
		w.WriteHeader(http.StatusCreated)
	}
}

// POST /api/v1/sites/{id}/features/queue-worker/disable
func (s *Server) handleDisableQueueWorker() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, _ := strconv.Atoi(r.PathValue("id"))
		if err := disableQueueWorkerRuntime(r.Context(), siteID); err != nil {
			http.Error(w, "Failed to disable queue worker: "+err.Error(), http.StatusInternalServerError)
			return
		}
		LogActivity(siteID, "feature", "Laravel queue worker disabled")
		w.WriteHeader(http.StatusNoContent)
	}
}
