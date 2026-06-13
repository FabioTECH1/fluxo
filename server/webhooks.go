package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"fluxo/database"
	"fluxo/services/deploy"
	"fluxo/services/git"
	"fluxo/syscmd"
)

type githubWebhookPayload struct {
	Ref        string `json:"ref"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func (s *Server) handleGitHubWebhook() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// 1. Get webhook secret from database
		var secret string
		err := database.DB.QueryRow("SELECT webhook_secret FROM users LIMIT 1").Scan(&secret)
		if err != nil || secret == "" {
			http.Error(w, "Webhook secret not configured", http.StatusInternalServerError)
			return
		}

		// 2. Read raw payload for signature verification
		payloadBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Error reading body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// 3. Verify signature
		signature := r.Header.Get("X-Hub-Signature-256")
		if signature == "" || !strings.HasPrefix(signature, "sha256=") {
			http.Error(w, "Missing or invalid signature", http.StatusUnauthorized)
			return
		}

		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(payloadBytes)
		expectedMAC := hex.EncodeToString(mac.Sum(nil))

		if !hmac.Equal([]byte(strings.TrimPrefix(signature, "sha256=")), []byte(expectedMAC)) {
			http.Error(w, "Signature mismatch", http.StatusUnauthorized)
			return
		}

		// 4. Parse payload
		var payload githubWebhookPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}

		// GitHub ref is typically "refs/heads/main"
		branch := strings.TrimPrefix(payload.Ref, "refs/heads/")
		repo := payload.Repository.FullName

		if branch == "" || repo == "" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Ignored: Missing branch or repo in payload"))
			return
		}

		// 5. Find matching sites with push_to_deploy enabled
		rows, err := database.DB.Query("SELECT id, domain, deploy_script, php_version, app_type FROM sites WHERE repository = ? AND branch = ? AND push_to_deploy = 1", repo, branch)
		if err != nil {
			http.Error(w, "Database query error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var matchedSites int
		for rows.Next() {
			var siteID int
			var domain, deployScript, phpVer, appType string
			if err := rows.Scan(&siteID, &domain, &deployScript, &phpVer, &appType); err != nil {
				continue
			}

			matchedSites++

			// 6. Trigger Deployment
			var deployID int
			err = database.DB.QueryRow("INSERT INTO deployments (site_id, status, trigger_source) VALUES (?, ?, ?) RETURNING id", siteID, "pending", "github_webhook").Scan(&deployID)
			if err != nil {
				continue
			}

			// Log activity
			database.DB.Exec("INSERT INTO activities (type, description, user, ip_address) VALUES (?, ?, ?, ?)",
				"deployment", fmt.Sprintf("Auto-deployment triggered via GitHub Webhook for Site %d", siteID), "system", r.RemoteAddr)

			go func(sID, dID int, siteDomain, siteRepo, siteBranch, script, php string) {
				privKeyPath := git.GetSSHKeyPath(sID)
				repoURL := "git@github.com:" + siteRepo + ".git"

				envMap := map[string]string{
					"FLUXO_PHP_VERSION": php,
					"FLUXO_PHP":         "php" + php,
					"FLUXO_COMPOSER":    "php" + php + " /usr/local/bin/composer",
					"FLUXO_SITE_PATH":   "/home/fluxo/" + siteDomain,
					"FLUXO_BRANCH":      siteBranch,
					"FLUXO_REPO":        repoURL,
					"FLUXO_DOMAIN":      siteDomain,
				}

				// Append under-the-hood final commands
				if (appType == "php" || appType == "laravel") && php != "" {
					script += "\n\nsudo systemctl reload php$FLUXO_PHP_VERSION-fpm\n"
				}
				script += "\necho \"Deployment complete.\"\n"

				// Execute script
				output, err := deploy.RunScript(context.Background(), sID, script, privKeyPath, envMap, nil)

				// Fetch commit metadata
				var commitHash, commitMessage string
				commitLog, _ := syscmd.RunEnvAsUser(context.Background(), 5*time.Second, "fluxo", []string{"HOME=/home/fluxo"}, "git", "-C", "/home/fluxo/"+siteDomain, "log", "-1", "--format=%H|%s|%an")
				parts := strings.SplitN(strings.TrimSpace(commitLog), "|", 3)
				if len(parts) == 3 {
					commitHash = parts[0]
					commitMessage = parts[1]
				}

				status := "success"
				if err != nil {
					status = "failed"
				}

				database.DB.Exec("UPDATE deployments SET status = ?, output = ?, commit_hash = ?, commit_message = ?, branch = ?, trigger_source = 'github_webhook' WHERE id = ?", status, output, commitHash, commitMessage, siteBranch, dID)
			}(siteID, deployID, domain, repo, branch, deployScript, phpVer)
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Triggered deployments for %d sites", matchedSites)
	}
}
