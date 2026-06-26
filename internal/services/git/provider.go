package git

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

type GitHubProvider struct {
	PAT string
}

// NewGitHubProvider creates a GitHub API client with the given personal access token.
func NewGitHubProvider(pat string) *GitHubProvider {
	return &GitHubProvider{PAT: pat}
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

	client := &http.Client{}
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

	client := &http.Client{}
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

// ListBranches fetches branches for the specified repository.
func (p *GitHubProvider) ListBranches(repoFullName string) ([]Branch, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/branches?per_page=100", repoFullName)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+p.PAT)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{}
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
	url := fmt.Sprintf("https://api.github.com/repos/%s/hooks", repoFullName)

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

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// 201 Created or 422 Unprocessable Entity (already exists)
	if resp.StatusCode != 201 && resp.StatusCode != 422 {
		return 0, fmt.Errorf("failed to register webhook: status %d", resp.StatusCode)
	}

	var result struct {
		ID int64 `json:"id"`
	}
	if resp.StatusCode == 201 {
		json.NewDecoder(resp.Body).Decode(&result)
	}

	return result.ID, nil
}

// RemoveDeployKey deletes a deploy key from the specified repository by its GitHub ID.
func (p *GitHubProvider) RemoveDeployKey(repoFullName string, keyID int64) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/keys/%d", repoFullName, keyID)
	req, _ := http.NewRequest("DELETE", url, nil)
	req.Header.Set("Authorization", "Bearer "+p.PAT)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{}
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
	url := fmt.Sprintf("https://api.github.com/repos/%s/hooks/%d", repoFullName, hookID)
	req, _ := http.NewRequest("DELETE", url, nil)
	req.Header.Set("Authorization", "Bearer "+p.PAT)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 {
		return fmt.Errorf("failed to remove webhook: status %d", resp.StatusCode)
	}

	return nil
}
