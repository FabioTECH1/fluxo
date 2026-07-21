package deploy

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	"fluxo/internal/services/daemon"
	"fluxo/internal/services/git"
	"fluxo/internal/services/site"
	"fluxo/internal/syscmd"
)

// Broadcaster is a global interface hook set by server package to stream logs to WebSockets.
var Broadcaster LogBroadcaster

type SiteQueue struct {
	mu      sync.Mutex
	ch      chan struct{}
	running bool
}

var (
	queues   = make(map[int]*SiteQueue)
	queuesMu sync.Mutex
)

// Enqueue registers a site queue worker (if not already running) and signals it to process pending jobs.
func Enqueue(siteID int) {
	queuesMu.Lock()
	q, exists := queues[siteID]
	if !exists {
		q = &SiteQueue{
			ch: make(chan struct{}, 100),
		}
		queues[siteID] = q
	}
	queuesMu.Unlock()

	q.mu.Lock()
	if !q.running {
		q.running = true
		go q.workerLoop(siteID)
	}
	q.mu.Unlock()

	select {
	case q.ch <- struct{}{}:
	default:
	}
}

// workerLoop processes pending deployments for a site sequentially.
func (q *SiteQueue) workerLoop(siteID int) {
	for {
		// Try to grab the oldest pending deployment
		var deployID int64
		err := database.DB.QueryRow(`SELECT d.id FROM deployments d
			JOIN sites s ON s.id = d.site_id
			WHERE d.site_id = ? AND d.status = 'pending' AND COALESCE(s.deletion_status, '') = ''
			ORDER BY d.id ASC LIMIT 1`, siteID).Scan(&deployID)
		if err == sql.ErrNoRows {
			// No pending deployments, stop worker
			q.mu.Lock()
			q.running = false
			q.mu.Unlock()
			return
		}
		if err != nil {
			log.Printf("Queue worker error querying deployments: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}

		// Process this deployment
		processDeployment(deployID, siteID)
	}
}

// processDeployment runs a single deployment: updates status, executes script, records result.
func processDeployment(deployID int64, siteID int) {
	// 1. Transition status to running
	result, err := database.DB.Exec(`UPDATE deployments
		SET status = 'running', updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'pending'
		AND EXISTS (SELECT 1 FROM sites WHERE id = ? AND COALESCE(deletion_status, '') = '')`, deployID, siteID)
	if err != nil {
		log.Printf("Failed to update status to running for deployment %d: %v", deployID, err)
		return
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return
	}

	// 2. Fetch site info
	var strategy, domain, repo, branch, phpVer, appType, deployScript, scriptMode, webRoot string
	var nodePreset, nodeMode, packageManager, buildCommand, startCommand, staticOutputDir, deletionStatus string
	var appPortValue sql.NullInt64
	var exposeEnv bool
	err = database.DB.QueryRow("SELECT deployment_strategy, domain, repository, branch, php_version, app_type, app_port, deploy_script, COALESCE(deploy_script_mode, 'legacy'), COALESCE(expose_env, 0), web_root, node_preset, node_mode, package_manager, build_command, start_command, static_output_dir, COALESCE(deletion_status, '') FROM sites WHERE id = ?", siteID).Scan(&strategy, &domain, &repo, &branch, &phpVer, &appType, &appPortValue, &deployScript, &scriptMode, &exposeEnv, &webRoot, &nodePreset, &nodeMode, &packageManager, &buildCommand, &startCommand, &staticOutputDir, &deletionStatus)
	if err != nil {
		log.Printf("Site not found in queue worker: %d", siteID)
		database.DB.Exec("UPDATE deployments SET status = 'failed', output = 'Site not found.' WHERE id = ?", deployID)
		return
	}
	if deletionStatus != "" {
		database.DB.Exec("UPDATE deployments SET status = 'failed', output = 'Deployment cancelled because site deletion started.' WHERE id = ?", deployID)
		return
	}
	repo = strings.TrimSpace(repo)
	branch = strings.TrimSpace(branch)
	if repo != "" && !safeinput.ValidateRepoFullName(repo) {
		database.DB.Exec("UPDATE deployments SET status = 'failed', output = 'Invalid repository configuration.' WHERE id = ?", deployID)
		return
	}
	if repo != "" && !safeinput.ValidateGitRef(branch) {
		database.DB.Exec("UPDATE deployments SET status = 'failed', output = 'Invalid branch configuration.' WHERE id = ?", deployID)
		return
	}
	if !safeinput.ValidatePHPVersion(phpVer) {
		phpVer = "8.4"
	}
	if appType != "php" && appType != "laravel" && appType != "html" && appType != "node" && appType != "wordpress" {
		appType = "php"
	}
	appPort := 0
	if appPortValue.Valid {
		appPort = int(appPortValue.Int64)
	}
	sitePath := "/home/fluxo/" + domain
	activeSitePath := site.ActiveSitePath(sitePath, strategy)
	resolvedWebRoot, err := safeinput.NormalizeWebRoot(sitePath, webRoot)
	if err != nil {
		database.DB.Exec("UPDATE deployments SET status = 'failed', output = 'Invalid web root configuration.' WHERE id = ?", deployID)
		return
	}
	if appType == "node" {
		nodePreset = site.NormalizeNodePreset(nodePreset)
		nodeMode = site.NormalizeNodeMode(nodeMode)
		packageManager = site.NormalizePackageManager(packageManager)
		staticOutputDir = site.NormalizeStaticOutputDir(nodePreset, staticOutputDir)
		if buildCommand == "" {
			buildCommand = site.DefaultNodeBuildCommand(nodePreset, packageManager)
		}
		if startCommand == "" {
			startCommand = site.DefaultNodeStartCommand(nodePreset, packageManager)
		}
	}

	// Check if this is a rollback deployment
	var targetCommitHash string
	database.DB.QueryRow("SELECT target_commit_hash FROM deployments WHERE id = ?", deployID).Scan(&targetCommitHash)

	// Database configuration belongs to the site's environment file after
	// provisioning. Deployments deliberately do not infer or rewrite DB_* values.
	managed := scriptMode == ScriptModeManaged
	var script string
	if managed {
		script = GenerateManagedLifecycle(strategy)
	} else {
		if targetCommitHash != "" {
			script = GenerateRollbackScript(strategy, appType)
		} else if deployScript != "" {
			script = deployScript
		} else {
			script = GenerateDeployScript(strategy, appType)
		}
		if strings.TrimSpace(script) == "" {
			database.DB.Exec("UPDATE deployments SET status = 'failed', output = 'No deployment script is configured for this site.' WHERE id = ?", deployID)
			return
		}
		script = ApplyHorizonDeploymentHook(script, IsHorizonEnabled(siteID))
	}
	privKeyPath := git.GetSSHKeyPath(siteID)
	repoURL := ""
	if repo != "" {
		repoURL = "git@github.com:" + repo + ".git"
	}
	releaseID := time.Now().Format("20060102150405") + "-" + strconv.FormatInt(deployID, 10)
	deployPath := sitePath
	if managed && strategy == "zero-downtime" {
		deployPath = filepath.Join(sitePath, "releases", releaseID)
		if relativeWebRoot, relErr := filepath.Rel(sitePath, resolvedWebRoot); relErr == nil {
			resolvedWebRoot = filepath.Join(deployPath, relativeWebRoot)
		}
	}

	envMap := map[string]string{
		"FLUXO_PHP_VERSION":      phpVer,
		"FLUXO_PHP":              "php" + phpVer,
		"FLUXO_COMPOSER":         "php" + phpVer + " /usr/local/bin/composer",
		"FLUXO_SITE_PATH":        sitePath,
		"FLUXO_ACTIVE_SITE_PATH": activeSitePath,
		"FLUXO_DEPLOY_PATH":      deployPath,
		"FLUXO_WEB_ROOT":         resolvedWebRoot,
		"FLUXO_BRANCH":           branch,
		"FLUXO_REPO":             repoURL,
		"FLUXO_DOMAIN":           domain,
		"FLUXO_APP_TYPE":         appType,
		"FLUXO_APP_PORT":         strconv.Itoa(appPort),
		"FLUXO_RELEASE_ID":       releaseID,
	}
	if managed {
		envMap["FLUXO_MANAGED_LIFECYCLE"] = "1"
	}
	if managed && strategy == "zero-downtime" {
		envMap["FLUXO_RELEASE_DIRECTORY"] = deployPath
	}
	if appType == "node" {
		envMap["FLUXO_NODE_PRESET"] = nodePreset
		envMap["FLUXO_NODE_MODE"] = nodeMode
		envMap["FLUXO_PACKAGE_MANAGER"] = packageManager
		envMap["FLUXO_NODE_INSTALL_COMMAND"] = site.PackageInstallCommand(packageManager)
		envMap["FLUXO_NODE_BUILD_COMMAND"] = buildCommand
		envMap["FLUXO_NODE_START_COMMAND"] = site.RenderNodeStartCommand(startCommand, appPort)
		envMap["FLUXO_STATIC_OUTPUT_DIR"] = staticOutputDir
	}

	if targetCommitHash != "" {
		envMap["FLUXO_TARGET_COMMIT"] = targetCommitHash
	}
	if exposeEnv {
		if envErr := exposeSiteEnvironment(filepath.Join(sitePath, ".env"), envMap); envErr != nil {
			database.DB.Exec("UPDATE deployments SET status = 'failed', output = ? WHERE id = ?", "Unable to expose the site environment: "+envErr.Error(), deployID)
			return
		}
	}

	if !managed && (appType == "php" || appType == "laravel" || appType == "wordpress") && phpVer != "" {
		script += "\n\nsudo systemctl reload php$FLUXO_PHP_VERSION-fpm\n"
	}
	if !managed {
		script += "\necho \"Deployment complete.\"\n"
	}

	previousCurrent := ""
	if managed && strategy == "zero-downtime" {
		previousCurrent, _ = currentReleaseTarget(sitePath)
	}

	// 4. Run deployment script and managed hooks within the documented limit.
	deployCtx, cancelDeployment := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancelDeployment()
	applicationCommands := ""
	if managed {
		applicationCommands = deployScript
	}
	output, err := RunScript(deployCtx, siteID, script, applicationCommands, privKeyPath, envMap, Broadcaster)

	status := "success"
	if err != nil {
		status = "failed"
		if managed && strategy == "zero-downtime" {
			if managedReleaseIsActive(sitePath, releaseID) {
				output += "\nThe deployment phase failed after activation; restoring the previous release.\n"
				if rollbackErr := rollbackManagedActivation(sitePath, previousCurrent, releaseID, deployID, siteID); rollbackErr != nil {
					output += "Rollback incomplete: " + rollbackErr.Error() + "\n"
				}
			} else {
				cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 30*time.Second)
				if cleanupErr := removeManagedRelease(cleanupCtx, sitePath, releaseID); cleanupErr != nil {
					output += "\nWarning: failed release cleanup was incomplete: " + cleanupErr.Error() + "\n"
				}
				cancelCleanup()
			}
		}
	} else if managed {
		hookOutput, hookErr := runManagedRuntimeHooks(deployCtx, siteID, strategy, appType, nodeMode, appPort, privKeyPath, envMap)
		output += hookOutput
		if hookErr == nil && deployCtx.Err() != nil {
			hookErr = fmt.Errorf("deployment deadline reached: %w", deployCtx.Err())
		}
		if hookErr != nil {
			status = "failed"
			output += "\nManaged runtime hook failed: " + hookErr.Error() + "\n"
			if strategy == "zero-downtime" {
				if rollbackErr := rollbackManagedActivation(sitePath, previousCurrent, releaseID, deployID, siteID); rollbackErr != nil {
					output += "Rollback incomplete: " + rollbackErr.Error() + "\n"
				} else if previousCurrent == "" {
					output += "Deactivated the failed first release.\n"
				} else {
					output += "Restored the previous release after a runtime hook failure.\n"
				}
			}
		}
		if strategy == "zero-downtime" && status == "success" {
			cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 30*time.Second)
			cleanupErr := cleanupManagedReleases(cleanupCtx, sitePath, 5)
			cancelCleanup()
			if cleanupErr != nil {
				output += "\nWarning: unable to clean old releases: " + cleanupErr.Error() + "\n"
			}
		}
	} else if appType == "node" && nodeMode == "server" {
		if restartErr := restartNodeDaemon(context.Background(), siteID); restartErr != nil {
			status = "failed"
			output += "\nFailed to restart Node.js daemon: " + restartErr.Error() + "\n"
		} else if healthErr := waitForTCP(context.Background(), appPort); healthErr != nil {
			status = "failed"
			output += "\nNode.js application health check failed: " + healthErr.Error() + "\n"
		}
	}

	// Fetch metadata after managed hooks because a critical ZDD runtime failure
	// may have restored the previous current release.
	var commitHash, commitMessage, commitAuthor string
	gitPath := filepath.Join(sitePath)
	if strategy == "zero-downtime" {
		gitPath = filepath.Join(sitePath, "current")
	}
	if repo != "" {
		commitLog, _ := syscmd.RunEnvAsUser(context.Background(), 5*time.Second, "fluxo", []string{"HOME=/home/fluxo"}, "git", "-C", gitPath, "log", "-1", "--format=%H|%s|%an")
		parts := strings.SplitN(strings.TrimSpace(commitLog), "|", 3)
		if len(parts) == 3 {
			commitHash = parts[0]
			commitMessage = parts[1]
			commitAuthor = parts[2]
		} else {
			commitHash = strings.TrimSpace(commitLog)
		}
	}
	if managed {
		if status == "success" {
			output += "\nDeployment completed successfully.\n"
		} else {
			output += "\nDeployment finished with errors.\n"
		}
	}

	database.DB.Exec("UPDATE deployments SET status = ?, output = ?, commit_hash = ?, commit_message = ?, commit_author = ?, branch = ? WHERE id = ?", status, output, commitHash, commitMessage, commitAuthor, branch, deployID)
	if managed && Broadcaster != nil {
		if status == "success" {
			Broadcaster.BroadcastLog(siteID, "Deployment completed successfully.")
		} else {
			Broadcaster.BroadcastLog(siteID, "Deployment finished with errors.")
		}
	}

	// Logging activity
	database.DB.Exec("INSERT INTO activity (site_id, type, summary) VALUES (?, ?, ?)", siteID, "deployment", "Deployment #"+strconv.FormatInt(deployID, 10)+" "+status)
}

func restartNodeDaemon(ctx context.Context, siteID int) error {
	var daemonID int
	err := database.DB.QueryRow("SELECT id FROM daemons WHERE site_id = ? AND name = 'Node.js' ORDER BY id ASC LIMIT 1", siteID).Scan(&daemonID)
	if err != nil {
		return err
	}
	return daemon.RestartAndWait(ctx, daemonID)
}

func rollbackManagedActivation(sitePath, previousCurrent, releaseID string, deployID int64, siteID int) error {
	if previousCurrent != "" {
		if err := restoreCurrentRelease(sitePath, previousCurrent, deployID); err != nil {
			return err
		}
	} else {
		currentPath := filepath.Join(sitePath, "current")
		target, err := os.Readlink(currentPath)
		if err != nil {
			return err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(sitePath, target)
		}
		expected := filepath.Join(sitePath, "releases", releaseID)
		if filepath.Clean(target) != filepath.Clean(expected) {
			return fmt.Errorf("current does not point to the failed release")
		}
		if err := os.Remove(currentPath); err != nil {
			return err
		}
	}

	rollbackCtx, cancelRollback := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelRollback()
	if previousCurrent != "" {
		if err := restartSiteDaemonsAfterRollback(rollbackCtx, siteID); err != nil {
			return fmt.Errorf("release restored, but a background process failed to restart: %w", err)
		}
	} else if err := stopSiteDaemonsAfterFailedFirstRelease(rollbackCtx, siteID); err != nil {
		return fmt.Errorf("failed first release was deactivated, but a background process failed to stop: %w", err)
	}
	if err := removeManagedRelease(rollbackCtx, sitePath, releaseID); err != nil {
		return fmt.Errorf("release restored, but failed release cleanup failed: %w", err)
	}
	return nil
}

func managedReleaseIsActive(sitePath, releaseID string) bool {
	target, err := os.Readlink(filepath.Join(sitePath, "current"))
	if err != nil {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(sitePath, target)
	}
	expected := filepath.Join(sitePath, "releases", releaseID)
	return filepath.Clean(target) == filepath.Clean(expected)
}

// RemoveQueue deletes the queue entry for a site. Safe to call after site deletion.
func RemoveQueue(siteID int) {
	queuesMu.Lock()
	delete(queues, siteID)
	queuesMu.Unlock()
}
