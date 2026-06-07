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

func NewGitHubProvider(pat string) *GitHubProvider {
	return &GitHubProvider{PAT: pat}
}

type Repository struct {
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
	CloneURL string `json:"clone_url"`
	SSHURL   string `json:"ssh_url"`
}

// ListRepositories fetches all repositories for the authenticated user
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

// InjectDeployKey adds a deploy key to the specified repository
func (p *GitHubProvider) InjectDeployKey(repoFullName, publicKey string) error {
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
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		return fmt.Errorf("failed to inject deploy key: status %d", resp.StatusCode)
	}

	return nil
}
