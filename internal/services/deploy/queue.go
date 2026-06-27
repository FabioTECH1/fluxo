package deploy

import (
	"context"
	"database/sql"
	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/services/git"
	"fluxo/internal/syscmd"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
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
		err := database.DB.QueryRow("SELECT id FROM deployments WHERE site_id = ? AND status = 'pending' ORDER BY id ASC LIMIT 1", siteID).Scan(&deployID)
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
	_, err := database.DB.Exec("UPDATE deployments SET status = 'running', updated_at = CURRENT_TIMESTAMP WHERE id = ?", deployID)
	if err != nil {
		log.Printf("Failed to update status to running for deployment %d: %v", deployID, err)
		return
	}

	// 2. Fetch site info
	var strategy, domain, repo, branch, phpVer, appType, deployScript string
	err = database.DB.QueryRow("SELECT deployment_strategy, domain, repository, branch, php_version, app_type, deploy_script FROM sites WHERE id = ?", siteID).Scan(&strategy, &domain, &repo, &branch, &phpVer, &appType, &deployScript)
	if err != nil {
		log.Printf("Site not found in queue worker: %d", siteID)
		database.DB.Exec("UPDATE deployments SET status = 'failed', output = 'Site not found.' WHERE id = ?", deployID)
		return
	}

	// Check if this is a rollback deployment
	var targetCommitHash string
	database.DB.QueryRow("SELECT target_commit_hash FROM deployments WHERE id = ?", deployID).Scan(&targetCommitHash)

	// 3. Setup database credentials
	var dbName, dbUser, dbEngine, dbConn, dbPort string
	database.DB.QueryRow("SELECT name, username, engine FROM databases WHERE site_id = ? LIMIT 1", siteID).Scan(&dbName, &dbUser, &dbEngine)

	var dbPass string
	database.DB.QueryRow("SELECT fluxo_db_password FROM users LIMIT 1").Scan(&dbPass)
	dbPass = config.Decrypt(dbPass)

	if dbEngine == "postgres" || dbEngine == "pgsql" {
		dbConn = "pgsql"
		dbPort = "5432"
	} else {
		dbConn = "mysql"
		dbPort = "3306"
	}
	var script string
	if targetCommitHash != "" {
		script = GenerateRollbackScript(strategy, appType)
	} else if deployScript != "" {
		script = deployScript
	} else {
		script = GenerateDeployScript(strategy, appType)
	}
	privKeyPath := git.GetSSHKeyPath(siteID)
	repoURL := "git@github.com:" + repo + ".git"

	envMap := map[string]string{
		"FLUXO_PHP_VERSION": phpVer,
		"FLUXO_PHP":         "php" + phpVer,
		"FLUXO_COMPOSER":    "php" + phpVer + " /usr/local/bin/composer",
		"FLUXO_SITE_PATH":   "/home/fluxo/" + domain,
		"FLUXO_BRANCH":      branch,
		"FLUXO_REPO":        repoURL,
		"FLUXO_DOMAIN":      domain,
		"FLUXO_DB_CONN":     dbConn,
		"FLUXO_DB_PORT":     dbPort,
		"FLUXO_DB_NAME":     dbName,
		"FLUXO_DB_USER":     dbUser,
		"FLUXO_DB_PASS":     dbPass,
	}

	if targetCommitHash != "" {
		envMap["FLUXO_TARGET_COMMIT"] = targetCommitHash
	}

	if (appType == "php" || appType == "laravel") && phpVer != "" {
		script += "\n\nsudo systemctl reload php$FLUXO_PHP_VERSION-fpm\n"
	}
	script += "\necho \"Deployment complete.\"\n"

	// 4. Run deployment script
	output, err := RunScript(context.Background(), siteID, script, privKeyPath, envMap, Broadcaster)

	// 5. Fetch latest commit metadata
	var commitHash, commitMessage string
	gitPath := "/home/fluxo/" + domain
	if strategy == "zero-downtime" {
		gitPath += "/current"
	}
	commitLog, _ := syscmd.RunEnvAsUser(context.Background(), 5*time.Second, "fluxo", []string{"HOME=/home/fluxo"}, "git", "-C", gitPath, "log", "-1", "--format=%H|%s|%an")
	parts := strings.SplitN(strings.TrimSpace(commitLog), "|", 3)
	if len(parts) == 3 {
		commitHash = parts[0]
		commitMessage = parts[1] + " by " + parts[2]
	} else {
		commitHash = strings.TrimSpace(commitLog)
	}

	status := "success"
	if err != nil {
		status = "failed"
	}

	database.DB.Exec("UPDATE deployments SET status = ?, output = ?, commit_hash = ?, commit_message = ?, branch = ? WHERE id = ?", status, output, commitHash, commitMessage, branch, deployID)

	// Logging activity
	database.DB.Exec("INSERT INTO activity (site_id, type, summary) VALUES (?, ?, ?)", siteID, "deployment", "Deployment #"+strconv.FormatInt(deployID, 10)+" "+status)
}

// RemoveQueue deletes the queue entry for a site. Safe to call after site deletion.
func RemoveQueue(siteID int) {
	queuesMu.Lock()
	delete(queues, siteID)
	queuesMu.Unlock()
}
