package git

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"fluxo/internal/safeinput"
	"fluxo/internal/syscmd"
	"golang.org/x/crypto/ssh"
)

// APIError deliberately excludes GitHub's response body and request credentials.
type APIError struct{ Status int }

func (e *APIError) Error() string {
	switch e.Status {
	case 401:
		return "GitHub rejected the token; update or reconnect it"
	case 403:
		return "GitHub denied access; check token permissions, organization policy and API rate limits"
	case 404:
		return "GitHub repository is unavailable to this token"
	case 422:
		return "GitHub rejected the deploy key; check repository key permissions and existing key registrations"
	default:
		return fmt.Sprintf("GitHub API returned status %d", e.Status)
	}
}

type Identity struct {
	Username  string
	ExpiresAt *time.Time
}

func parseTokenExpiry(value string) *time.Time {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05 MST", time.RFC1123} {
		if t, err := time.Parse(layout, value); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

func (p *GitHubProvider) connectionRequest(ctx context.Context, method, path string, payload any, result any) (http.Header, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.webhookAPIURL(path), body)
	if err != nil {
		return nil, errors.New("cannot prepare GitHub request")
	}
	req.Header.Set("Authorization", "Bearer "+p.PAT)
	req.Header.Set("Accept", "application/vnd.github+json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.webhookHTTPClient().Do(req)
	if err != nil {
		return nil, errors.New("GitHub could not be reached; retry the connection check")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.Header, &APIError{Status: resp.StatusCode}
	}
	if result != nil {
		if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(result); err != nil {
			return nil, errors.New("GitHub returned an unreadable response")
		}
	}
	return resp.Header, nil
}

func (p *GitHubProvider) GetIdentity(ctx context.Context) (Identity, error) {
	var user struct {
		Login string `json:"login"`
	}
	headers, err := p.connectionRequest(ctx, http.MethodGet, "/user", nil, &user)
	if err != nil {
		return Identity{}, err
	}
	if user.Login == "" {
		return Identity{}, errors.New("GitHub returned no account identity")
	}
	return Identity{Username: user.Login, ExpiresAt: parseTokenExpiry(headers.Get("GitHub-Authentication-Token-Expiration"))}, nil
}

// EnsureDeployKey re-registers an existing site's public key; it never rotates a private key.
func (p *GitHubProvider) EnsureDeployKey(ctx context.Context, repo, publicKey string) (int64, error) {
	if !safeinput.ValidateRepoFullName(repo) {
		return 0, errors.New("invalid repository")
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return 0, errors.New("invalid site public key")
	}
	for page := 1; ; page++ {
		var keys []struct {
			ID  int64  `json:"id"`
			Key string `json:"key"`
		}
		_, err := p.connectionRequest(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/keys?per_page=100&page=%d", repo, page), nil, &keys)
		if err != nil {
			return 0, err
		}
		for _, existing := range keys {
			parsed, _, _, _, e := ssh.ParseAuthorizedKey([]byte(existing.Key))
			if e == nil && bytes.Equal(parsed.Marshal(), key.Marshal()) {
				return existing.ID, nil
			}
		}
		if len(keys) < 100 {
			break
		}
	}
	var created struct {
		ID int64 `json:"id"`
	}
	_, err = p.connectionRequest(ctx, http.MethodPost, "/repos/"+repo+"/keys", map[string]any{"title": "Fluxo Deploy Key", "key": publicKey, "read_only": true}, &created)
	if err != nil {
		return 0, err
	}
	if created.ID <= 0 {
		return 0, errors.New("GitHub returned no deploy key ID")
	}
	return created.ID, nil
}

// DeployPublicKey derives the public key from the managed private key. A stale
// .pub file must never cause the wrong key to be registered during recovery.
func DeployPublicKey(siteID int) (string, error) {
	deployKeysMu.Lock()
	defer deployKeysMu.Unlock()
	directory, err := openDeployKeyDirectory(false)
	if err != nil {
		return "", errors.New("site SSH directory is unavailable")
	}
	defer directory.Close()
	private, _, err := directory.ReadFile(deployKeyName(siteID))
	if err != nil || private == nil {
		return "", errors.New("site private deploy key is missing or unreadable")
	}
	signer, err := ssh.ParsePrivateKey(private)
	if err != nil {
		return "", errors.New("site private deploy key is invalid")
	}
	return string(ssh.MarshalAuthorizedKey(signer.PublicKey())), nil
}

// DeploySSHCommand pins the site identity, disables agent/config fallbacks and
// fails without prompting. Git invokes this environment value through a shell.
func DeploySSHCommand(path string) string {
	quoted := "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
	return "ssh -F /dev/null -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=10 -i " + quoted
}

func CheckDeployAccess(ctx context.Context, siteID int, repo string) error {
	if !safeinput.ValidateRepoFullName(repo) {
		return errors.New("invalid repository")
	}
	if _, err := DeployPublicKey(siteID); err != nil {
		return err
	}
	_, err := syscmd.RunEnvAsUser(ctx, 15*time.Second, "fluxo", []string{
		"GIT_SSH_COMMAND=" + DeploySSHCommand(GetSSHKeyPath(siteID)), "GIT_TERMINAL_PROMPT=0", "SSH_AUTH_SOCK=",
	}, "git", "ls-remote", "--", "git@github.com:"+repo+".git", "HEAD")
	if err != nil {
		return errors.New("SSH repository access failed; check the site's deploy key, repository access and server connectivity")
	}
	return nil
}
