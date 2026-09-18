package git

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type GitHubProvider struct {
	PAT        string
	apiBaseURL string
	httpClient *http.Client
}

// NewGitHubProvider creates a GitHub API client with the given personal access token.
func NewGitHubProvider(pat string) *GitHubProvider {
	return &GitHubProvider{
		PAT:        pat,
		apiBaseURL: "https://api.github.com",
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *GitHubProvider) webhookAPIURL(path string) string {
	baseURL := p.apiBaseURL
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	return strings.TrimRight(baseURL, "/") + path
}

func (p *GitHubProvider) webhookHTTPClient() *http.Client {
	if p.httpClient != nil {
		return p.httpClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

type Repository struct {
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
	CloneURL string `json:"clone_url"`
	SSHURL   string `json:"ssh_url"`
}

// ListRepositories fetches all repositories for the authenticated user.
func (p *GitHubProvider) ListRepositories() ([]Repository, error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/user/repos?per_page=100&sort=updated", nil)
	req.Header.Set("Authorization", "Bearer "+p.PAT)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("github api error: status %d", resp.StatusCode)
	}

	var repos []Repository
	if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
		return nil, err
	}

	return repos, nil
}

// InjectDeployKey adds a read-only deploy key to the specified repository.
// Returns the GitHub key ID on success.
func (p *GitHubProvider) InjectDeployKey(repoFullName, publicKey string) (int64, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/keys", repoFullName)

	payload := map[string]interface{}{
		"title":     "Fluxo Deploy Key",
		"key":       publicKey,
		"read_only": true,
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+p.PAT)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		return 0, fmt.Errorf("failed to inject deploy key: status %d", resp.StatusCode)
	}

	var result struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("failed to parse deploy key response: %w", err)
	}

	return result.ID, nil
}

type Branch struct {
	Name string `json:"name"`
}

// Webhook is the subset of a GitHub repository webhook used by Fluxo's
// reconciliation process. GitHub does not return webhook secrets.
type Webhook struct {
	ID     int64    `json:"id"`
	Active bool     `json:"active"`
	Events []string `json:"events"`
	Config struct {
		URL         string `json:"url"`
		ContentType string `json:"content_type"`
		InsecureSSL string `json:"insecure_ssl"`
	} `json:"config"`
	LastResponse struct {
		Code int `json:"code"`
	} `json:"last_response"`
}

// WebhookDelivery is the identity GitHub assigns to one webhook delivery.
// The GUID is also sent in the X-GitHub-Delivery request header.
type WebhookDelivery struct {
	ID   int64  `json:"id"`
	GUID string `json:"guid"`
}

// ListBranches fetches branches for the specified repository.
func (p *GitHubProvider) ListBranches(repoFullName string) ([]Branch, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/branches?per_page=100", repoFullName)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+p.PAT)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("github api error: status %d", resp.StatusCode)
	}

	var branches []Branch
	if err := json.NewDecoder(resp.Body).Decode(&branches); err != nil {
		return nil, err
	}

	return branches, nil
}

// RegisterWebhook adds a push webhook to the specified repository.
// Returns the GitHub webhook ID on success.
func (p *GitHubProvider) RegisterWebhook(repoFullName, webhookURL, secret string) (int64, error) {
	url := p.webhookAPIURL(fmt.Sprintf("/repos/%s/hooks", repoFullName))

	payload := map[string]interface{}{
		"name":   "web",
		"active": true,
		"events": []string{"push"},
		"config": map[string]string{
			"url":          webhookURL,
			"content_type": "json",
			"secret":       secret,
			"insecure_ssl": "1",
		},
	}

	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+p.PAT)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := p.webhookHTTPClient().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// GitHub returns 422 when an identical callback already exists. Resolve the
	// existing ID so callers never lose ownership of a usable hook.
	if resp.StatusCode != 201 && resp.StatusCode != 422 {
		return 0, fmt.Errorf("failed to register webhook: status %d", resp.StatusCode)
	}

	var result struct {
		ID int64 `json:"id"`
	}
	if resp.StatusCode == 201 {
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return 0, fmt.Errorf("failed to parse webhook response: %w", err)
		}
		return result.ID, nil
	}

	hooks, err := p.ListWebhooks(repoFullName)
	if err != nil {
		return 0, fmt.Errorf("webhook already exists but its ID could not be resolved: %w", err)
	}
	for _, hook := range hooks {
		if hook.Config.URL == webhookURL {
			return hook.ID, nil
		}
	}
	return 0, fmt.Errorf("webhook already exists but GitHub did not return a matching callback")
}

// ListWebhooks returns repository webhooks visible to the connected account.
func (p *GitHubProvider) ListWebhooks(repoFullName string) ([]Webhook, error) {
	const pageSize = 100
	var hooks []Webhook
	for page := 1; ; page++ {
		url := p.webhookAPIURL(fmt.Sprintf("/repos/%s/hooks?per_page=%d&page=%d", repoFullName, pageSize, page))
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+p.PAT)
		req.Header.Set("Accept", "application/vnd.github.v3+json")

		resp, err := p.webhookHTTPClient().Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to list webhooks: status %d", resp.StatusCode)
		}

		var pageHooks []Webhook
		decodeErr := json.NewDecoder(resp.Body).Decode(&pageHooks)
		resp.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("failed to parse webhook list: %w", decodeErr)
		}
		hooks = append(hooks, pageHooks...)
		if len(pageHooks) < pageSize {
			return hooks, nil
		}
	}
}

// HasWebhookDelivery verifies through GitHub's API that a delivery GUID belongs
// to a specific repository webhook. This is required because webhook request
// headers are not covered by the payload HMAC signature.
func (p *GitHubProvider) HasWebhookDelivery(repoFullName string, hookID int64, deliveryGUID string) (bool, error) {
	url := p.webhookAPIURL(fmt.Sprintf("/repos/%s/hooks/%d/deliveries?per_page=100", repoFullName, hookID))
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+p.PAT)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := p.webhookHTTPClient().Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("failed to list webhook deliveries: status %d", resp.StatusCode)
	}

	var deliveries []WebhookDelivery
	if err := json.NewDecoder(resp.Body).Decode(&deliveries); err != nil {
		return false, fmt.Errorf("failed to parse webhook deliveries: %w", err)
	}
	for _, delivery := range deliveries {
		if strings.EqualFold(strings.TrimSpace(delivery.GUID), strings.TrimSpace(deliveryGUID)) {
			return true, nil
		}
	}
	return false, nil
}

// UpdateWebhook makes an existing callback active, push-only, and signed with
// the current Fluxo secret.
func (p *GitHubProvider) UpdateWebhook(repoFullName string, hookID int64, webhookURL, secret string) error {
	url := p.webhookAPIURL(fmt.Sprintf("/repos/%s/hooks/%d", repoFullName, hookID))
	payload := map[string]interface{}{
		"active": true,
		"events": []string{"push"},
		"config": map[string]string{
			"url":          webhookURL,
			"content_type": "json",
			"secret":       secret,
			"insecure_ssl": "1",
		},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPatch, url, bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+p.PAT)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := p.webhookHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to update webhook: status %d", resp.StatusCode)
	}
	return nil
}

// RemoveDeployKey deletes a deploy key from the specified repository by its GitHub ID.
func (p *GitHubProvider) RemoveDeployKey(repoFullName string, keyID int64) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/keys/%d", repoFullName, keyID)
	req, _ := http.NewRequest("DELETE", url, nil)
	req.Header.Set("Authorization", "Bearer "+p.PAT)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		return fmt.Errorf("failed to remove deploy key: status %d", resp.StatusCode)
	}

	return nil
}

// RemoveWebhook deletes a webhook from the specified repository by its GitHub ID.
func (p *GitHubProvider) RemoveWebhook(repoFullName string, hookID int64) error {
	url := p.webhookAPIURL(fmt.Sprintf("/repos/%s/hooks/%d", repoFullName, hookID))
	req, _ := http.NewRequest("DELETE", url, nil)
	req.Header.Set("Authorization", "Bearer "+p.PAT)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := p.webhookHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("failed to remove webhook: status %d", resp.StatusCode)
	}

	return nil
}

// GetAuthenticatedUsername fetches the username (login) of the authenticated user.
func (p *GitHubProvider) GetAuthenticatedUsername() (string, error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+p.PAT)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("github api error: status %d", resp.StatusCode)
	}

	var user struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return "", err
	}

	return user.Login, nil
}
