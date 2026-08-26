package deploy

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/daemon"
	sitepkg "fluxo/internal/services/site"
	"fluxo/internal/syscmd"
)

var environmentKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// exposeSiteEnvironment adds literal dotenv values to a deployment's clean
// environment. It intentionally does not perform shell expansion.
func exposeSiteEnvironment(path string, envMap map[string]string) error {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, value, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || !environmentKeyPattern.MatchString(key) || strings.HasPrefix(key, "FLUXO_") {
			continue
		}
		switch key {
		case "HOME", "USER", "PATH", "SHELL", "IFS", "ENV", "BASH_ENV", "SHELLOPTS", "GIT_SSH_COMMAND":
			continue
		}
		if strings.HasPrefix(key, "LD_") {
			continue
		}

		value = strings.TrimSpace(value)
		if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
			value = value[1 : len(value)-1]
		} else if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			if decoded, decodeErr := strconv.Unquote(value); decodeErr == nil {
				value = decoded
			}
		}
		envMap[key] = value
	}
	return scanner.Err()
}

func runManagedRuntimeHooks(ctx context.Context, siteID int, deploymentID int64, strategy, appType, nodeMode string, appPort int, privKeyPath string, envMap map[string]string) (string, error) {
	var output strings.Builder
	checkContext := func() error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("deployment deadline reached: %w", err)
		}
		return nil
	}

	if IsHorizonEnabled(siteID) {
		output.WriteString("\n[managed] Terminating Horizon so it can load the active release...\n")
		hookOutput, err := RunScript(ctx, siteID, deploymentID, "set -e\n"+HorizonTerminateLine+"\n", "", privKeyPath, envMap, Broadcaster)
		output.WriteString(hookOutput)
		if err != nil {
			return output.String(), fmt.Errorf("terminate Laravel Horizon: %w", err)
		}
	}
	if err := checkContext(); err != nil {
		return output.String(), err
	}

	if IsQueueWorkerEnabled(siteID) {
		output.WriteString("\n[managed] Restarting Laravel queue workers gracefully...\n")
		hookOutput, err := RunScript(ctx, siteID, deploymentID, "set -e\n"+QueueRestartLine+"\n", "", privKeyPath, envMap, Broadcaster)
		output.WriteString(hookOutput)
		if err != nil {
			return output.String(), fmt.Errorf("restart Laravel queue workers: %w", err)
		}
	}
	if err := checkContext(); err != nil {
		return output.String(), err
	}

	octaneEnabled := isOctaneDaemonEnabled(siteID)
	if strategy != "zero-downtime" && octaneEnabled {
		output.WriteString("\n[managed] Reloading Laravel Octane...\n")
		hookOutput, err := RunScript(ctx, siteID, deploymentID, "set -e\ncd \"$FLUXO_ACTIVE_SITE_PATH\"\n$FLUXO_PHP artisan octane:reload\n", "", privKeyPath, envMap, Broadcaster)
		output.WriteString(hookOutput)
		if err != nil {
			return output.String(), fmt.Errorf("reload Laravel Octane: %w", err)
		}
		if err := waitForTCP(ctx, appPort); err != nil {
			return output.String(), fmt.Errorf("verify Laravel Octane: %w", err)
		}
	}
	if err := checkContext(); err != nil {
		return output.String(), err
	}

	if appType == "node" && nodeMode == "server" {
		output.WriteString("\n[managed] Restarting Node.js application...\n")
		if err := restartNamedDaemon(ctx, siteID, "Node.js"); err != nil {
			return output.String(), fmt.Errorf("restart Node.js daemon: %w", err)
		}
		if err := waitForTCP(ctx, appPort); err != nil {
			return output.String(), fmt.Errorf("verify Node.js application: %w", err)
		}
	}
	if err := checkContext(); err != nil {
		return output.String(), err
	}

	if daemonOutput, err := restartManagedSiteDaemons(ctx, siteID); err != nil {
		output.WriteString(daemonOutput)
		return output.String(), err
	} else {
		output.WriteString(daemonOutput)
	}
	if err := checkContext(); err != nil {
		return output.String(), err
	}

	return output.String(), nil
}

func isOctaneDaemonEnabled(siteID int) bool {
	var count int
	_ = database.DB.QueryRow("SELECT COUNT(*) FROM daemons WHERE site_id = ? AND (name = 'Laravel Octane' OR command LIKE '%artisan octane:start%')", siteID).Scan(&count)
	return count > 0
}

func restartNamedDaemon(ctx context.Context, siteID int, name string) error {
	var daemonID int
	if err := database.DB.QueryRow("SELECT id FROM daemons WHERE site_id = ? AND name = ? ORDER BY id ASC LIMIT 1", siteID, name).Scan(&daemonID); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("managed daemon not found")
		}
		return err
	}
	return daemon.RestartAndWait(ctx, daemonID)
}

func restartManagedSiteDaemons(ctx context.Context, siteID int) (string, error) {
	rows, err := database.DB.Query(`SELECT id, COALESCE(name, '') FROM daemons
		WHERE site_id = ? AND COALESCE(restart_on_deploy, 1) = 1
		AND name NOT IN ('Node.js', 'Laravel Horizon', 'Laravel Octane', 'Nightwatch')
		AND COALESCE(managed_kind, '') != 'laravel_queue'
		AND command NOT LIKE '%artisan horizon%'
		AND command NOT LIKE '%artisan octane:start%'
		AND command NOT LIKE '%nightwatch:agent%' ORDER BY id ASC`, siteID)
	if err != nil {
		return "", fmt.Errorf("load restart-on-deploy processes: %w", err)
	}
	defer rows.Close()

	type managedDaemon struct {
		id   int
		name string
	}
	var daemons []managedDaemon
	for rows.Next() {
		var item managedDaemon
		if err := rows.Scan(&item.id, &item.name); err != nil {
			return "", err
		}
		daemons = append(daemons, item)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	var output strings.Builder
	for _, item := range daemons {
		label := item.name
		if label == "" {
			label = "background process #" + strconv.Itoa(item.id)
		}
		output.WriteString("\n[managed] Restarting " + label + "...\n")
		if err := daemon.RestartAndWait(ctx, item.id); err != nil {
			return output.String(), fmt.Errorf("restart %s: %w", label, err)
		}
	}
	return output.String(), nil
}

func restartSiteDaemonsAfterRollback(ctx context.Context, siteID int) error {
	if IsQueueWorkerEnabled(siteID) {
		var sitePath, phpVersion, strategy string
		if err := database.DB.QueryRow("SELECT path, php_version, deployment_strategy FROM sites WHERE id = ?", siteID).Scan(&sitePath, &phpVersion, &strategy); err != nil {
			return err
		}
		if phpVersion == "" {
			phpVersion = "8.4"
		}
		activePath := sitepkg.ActiveSitePath(sitePath, strategy)
		if _, err := syscmd.RunAsUserInDir(ctx, 30*time.Second, "fluxo", activePath, "php"+phpVersion, "artisan", "queue:restart"); err != nil {
			return fmt.Errorf("gracefully restart Laravel queue workers after rollback: %w", err)
		}
	}

	rows, err := database.DB.Query(`SELECT id FROM daemons WHERE site_id = ?
		AND (COALESCE(restart_on_deploy, 1) = 1 OR name IN ('Node.js', 'Laravel Horizon'))
		AND name != 'Nightwatch' AND command NOT LIKE '%nightwatch:agent%'
		AND COALESCE(managed_kind, '') != 'laravel_queue' ORDER BY id ASC`, siteID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	for _, id := range ids {
		if err := daemon.RestartAndWait(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func stopSiteDaemonsAfterFailedFirstRelease(ctx context.Context, siteID int) error {
	rows, err := database.DB.Query("SELECT id FROM daemons WHERE site_id = ? ORDER BY id ASC", siteID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	for _, id := range ids {
		if err := daemon.Stop(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func waitForTCP(ctx context.Context, port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid application port %d", port)
	}
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for {
		connection, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", address)
		if err == nil {
			_ = connection.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("port %d did not accept connections", port)
		case <-ticker.C:
		}
	}
}

func currentReleaseTarget(sitePath string) (string, error) {
	return os.Readlink(filepath.Join(sitePath, "current"))
}

func restoreCurrentRelease(sitePath, target string, deployID int64) error {
	if strings.TrimSpace(target) == "" {
		return fmt.Errorf("previous release target is empty")
	}
	temporary := filepath.Join(sitePath, ".current-restore-"+strconv.FormatInt(deployID, 10))
	_ = os.Remove(temporary)
	if err := os.Symlink(target, temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, filepath.Join(sitePath, "current")); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func removeManagedRelease(ctx context.Context, sitePath, releaseID string) error {
	releasesPath := filepath.Join(sitePath, "releases")
	releasePath := filepath.Join(releasesPath, releaseID)
	if filepath.Dir(releasePath) != releasesPath || filepath.Base(releasePath) != releaseID {
		return fmt.Errorf("invalid release path")
	}
	output, err := syscmd.RunAsUser(ctx, 30*time.Second, "fluxo", "rm", "-rf", "--", releasePath)
	if err != nil {
		return fmt.Errorf("remove release: %s: %w", strings.TrimSpace(output), err)
	}
	return nil
}

func cleanupManagedReleases(ctx context.Context, sitePath string, keep int) error {
	if keep < 1 {
		return fmt.Errorf("release retention must be positive")
	}
	releasesPath := filepath.Join(sitePath, "releases")
	entries, err := os.ReadDir(releasesPath)
	if err != nil {
		return err
	}

	type releaseEntry struct {
		name    string
		modTime time.Time
	}
	releases := make([]releaseEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		releases = append(releases, releaseEntry{name: entry.Name(), modTime: info.ModTime()})
	}
	sort.Slice(releases, func(i, j int) bool { return releases[i].modTime.After(releases[j].modTime) })
	if len(releases) <= keep {
		return nil
	}

	protected := map[string]bool{}
	if currentTarget, err := filepath.EvalSymlinks(filepath.Join(sitePath, "current")); err == nil {
		absoluteTarget, absErr := filepath.Abs(currentTarget)
		absoluteReleases, releasesErr := filepath.Abs(releasesPath)
		if absErr == nil && releasesErr == nil && filepath.Dir(absoluteTarget) == absoluteReleases {
			protected[filepath.Base(absoluteTarget)] = true
		}
	}
	for _, release := range releases {
		if len(protected) >= keep {
			break
		}
		protected[release.name] = true
	}

	for _, release := range releases {
		if protected[release.name] {
			continue
		}
		if err := removeManagedRelease(ctx, sitePath, release.name); err != nil {
			return err
		}
	}
	return nil
}
