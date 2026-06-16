package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"fluxo/internal/database"
	"fluxo/internal/services/deploy"
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

			// 6. Create pending deployment record
			_, err = database.DB.Exec("INSERT INTO deployments (site_id, status, trigger_source) VALUES (?, ?, ?)", siteID, "pending", "github_webhook")
			if err != nil {
				continue
			}

			matchedSites++

			// Log activity
			database.DB.Exec("INSERT INTO activity (site_id, type, summary) VALUES (?, ?, ?)",
				siteID, "deployment", fmt.Sprintf("Auto-deployment triggered via GitHub Webhook for Site %d", siteID))

			deploy.Enqueue(siteID)
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Triggered deployments for %d sites", matchedSites)
	}
}
