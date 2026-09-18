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
	"strconv"
	"strings"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	"fluxo/internal/services/deploy"
	"log"
)

type githubWebhookPayload struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func recordObservedGitHubWebhook(repository string, hookID int64) bool {
	repository = strings.TrimSpace(repository)
	if !safeinput.ValidateRepoFullName(repository) || hookID <= 0 {
		return false
	}
	_, err := database.DB.Exec(`
		INSERT INTO github_webhook_observations (repository, hook_id, last_seen_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(repository, hook_id) DO UPDATE SET last_seen_at = CURRENT_TIMESTAMP`, repository, hookID)
	return err == nil
}

func (s *Server) verifyAndObserveGitHubWebhook(repository, rawHookID, deliveryGUID string) bool {
	repository = strings.TrimSpace(repository)
	hookID, err := strconv.ParseInt(strings.TrimSpace(rawHookID), 10, 64)
	if !safeinput.ValidateRepoFullName(repository) || err != nil || hookID <= 0 ||
		!githubDeliveryGUIDPattern.MatchString(strings.TrimSpace(deliveryGUID)) {
		return false
	}
	var alreadyObserved int
	if err := database.DB.QueryRow(`
		SELECT COUNT(*) FROM github_webhook_observations
		WHERE repository = ? AND hook_id = ?`, repository, hookID).Scan(&alreadyObserved); err == nil && alreadyObserved > 0 {
		return true
	}

	rows, err := database.DB.Query(`
		SELECT DISTINCT COALESCE(github_account_id, 0)
		FROM sites
		WHERE repository = ? AND push_to_deploy = 1 AND COALESCE(deletion_status, '') = ''
		ORDER BY github_account_id DESC`, repository)
	if err != nil {
		return false
	}
	var accountIDs []int
	for rows.Next() {
		var accountID int
		if rows.Scan(&accountID) == nil {
			accountIDs = append(accountIDs, accountID)
		}
	}
	rows.Close()

	for _, accountID := range accountIDs {
		token, err := loadGitHubToken(accountID)
		if err != nil {
			continue
		}
		verified, err := s.githubWebhookProvider(token).HasWebhookDelivery(repository, hookID, deliveryGUID)
		if err != nil {
			log.Printf("Warning: failed to verify GitHub webhook delivery %s for hook %d: %v", deliveryGUID, hookID, err)
			continue
		}
		if verified {
			return recordObservedGitHubWebhook(repository, hookID)
		}
	}
	return false
}

func insertWebhookDeployment(siteID int, branch, targetCommit string) (bool, error) {
	domainMutationMu.Lock()
	defer domainMutationMu.Unlock()
	result, err := database.DB.Exec(`INSERT INTO deployments
		(site_id, status, trigger_source, webhook_commit_hash, branch)
		SELECT ?, 'pending', 'github_webhook', ?, ?
		WHERE EXISTS (SELECT 1 FROM sites WHERE id = ? AND COALESCE(deletion_status, '') = '')
		  AND (? = '' OR NOT EXISTS (
			SELECT 1 FROM deployments
			WHERE site_id = ? AND trigger_source = 'github_webhook'
			  AND webhook_commit_hash = ?
			  AND created_at >= datetime('now', '-2 minutes')
		  ))`, siteID, targetCommit, branch, siteID, targetCommit, siteID, targetCommit)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return err == nil && affected == 1, err
}

// handleGitHubWebhook validates a GitHub webhook signature and triggers deployments.
func (s *Server) handleGitHubWebhook() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Get webhook secret from database
		var secret string
		err := database.DB.QueryRow("SELECT webhook_secret FROM users LIMIT 1").Scan(&secret)
		if err != nil || secret == "" {
			http.Error(w, "Webhook secret not configured", http.StatusInternalServerError)
			return
		}
		secret = config.Decrypt(secret)

		// Read raw payload for signature verification
		payloadBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Error reading body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Verify signature
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

		// Parse payload
		var payload githubWebhookPayload
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}

		branch := strings.TrimSpace(strings.TrimPrefix(payload.Ref, "refs/heads/"))
		repo := strings.TrimSpace(payload.Repository.FullName)
		targetCommit := strings.TrimSpace(payload.After)
		if strings.Trim(targetCommit, "0") == "" {
			targetCommit = ""
		}

		log.Printf("Webhook received: repo=%q branch=%q", repo, branch)
		rawHookID := r.Header.Get("X-GitHub-Hook-ID")
		deliveryGUID := r.Header.Get("X-GitHub-Delivery")
		if rawHookID != "" && deliveryGUID != "" {
			defer func() {
				go func() {
					if s.verifyAndObserveGitHubWebhook(repo, rawHookID, deliveryGUID) {
						s.reconcileRepositoryGitHubWebhook(context.Background(), repo)
					}
				}()
			}()
		}

		if branch == "" || repo == "" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Ignored: Missing branch or repo in payload"))
			return
		}

		// Find matching sites with push_to_deploy enabled
		rows, err := database.DB.Query("SELECT id FROM sites WHERE repository = ? AND branch = ? AND push_to_deploy = 1 AND COALESCE(deletion_status, '') = ''", repo, branch)
		if err != nil {
			http.Error(w, "Database query error", http.StatusInternalServerError)
			return
		}
		var siteIDs []int
		for rows.Next() {
			var siteID int
			if err := rows.Scan(&siteID); err != nil {
				rows.Close()
				http.Error(w, "Database query error", http.StatusInternalServerError)
				return
			}
			siteIDs = append(siteIDs, siteID)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			http.Error(w, "Database query error", http.StatusInternalServerError)
			return
		}
		if err := rows.Close(); err != nil {
			http.Error(w, "Database query error", http.StatusInternalServerError)
			return
		}

		var matchedSites int
		for _, siteID := range siteIDs {
			// Create pending deployment record
			inserted, err := insertWebhookDeployment(siteID, branch, targetCommit)
			if err != nil {
				log.Printf("Webhook insert error for site %d: %v", siteID, err)
				continue
			}
			if !inserted {
				continue
			}

			matchedSites++

			database.DB.Exec("INSERT INTO activity (site_id, type, summary) VALUES (?, ?, ?)",
				siteID, "deployment", fmt.Sprintf("Auto-deployment triggered via GitHub Webhook for Site %d", siteID))

			deploy.Enqueue(siteID)
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Triggered deployments for %d sites", matchedSites)
	}
}
