package git

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestIdentityExpiryAndSafeErrors(t *testing.T) {
	for _, test := range []struct {
		header string
		want   bool
	}{{"2026-11-30 15:04:05 UTC", true}, {"2026-11-30T15:04:05Z", true}, {"", false}, {"not-a-date", false}} {
		t.Run(test.header, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer secret-token" {
					t.Error("token not supplied")
				}
				w.Header().Set("GitHub-Authentication-Token-Expiration", test.header)
				fmt.Fprint(w, `{"id":101,"login":"alice"}`)
			}))
			defer server.Close()
			provider := NewGitHubProvider("secret-token")
			provider.apiBaseURL = server.URL
			identity, err := provider.GetIdentity(context.Background())
			if err != nil || identity.ID != 101 || identity.Username != "alice" || (identity.ExpiresAt != nil) != test.want {
				t.Fatalf("identity=%+v error=%v", identity, err)
			}
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "secret-token response body", 401) }))
	defer server.Close()
	provider := NewGitHubProvider("secret-token")
	provider.apiBaseURL = server.URL
	_, err := provider.GetIdentity(context.Background())
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.Status != 401 || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("unsafe/wrong API error: %v", err)
	}
}
func testPublicKey(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
}
func TestEnsureDeployKeyDoesNotDuplicateExistingKey(t *testing.T) {
	key := testPublicKey(t)
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts++
			t.Error("existing key must not be recreated")
		}
		fmt.Fprintf(w, `[{"id":77,"key":%q}]`, key+" old-comment")
	}))
	defer server.Close()
	p := NewGitHubProvider("token")
	p.apiBaseURL = server.URL
	id, err := p.EnsureDeployKey(context.Background(), "owner/repo", key+" new-comment")
	if err != nil || id != 77 || posts != 0 {
		t.Fatalf("id=%d err=%v posts=%d", id, err, posts)
	}
}
func TestEnsureDeployKeyRegistersMissingKeyAndRejectsForbidden(t *testing.T) {
	key := testPublicKey(t)
	for _, status := range []int{201, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					fmt.Fprint(w, `[]`)
					return
				}
				w.WriteHeader(status)
				fmt.Fprint(w, `{"id":88}`)
			}))
			defer server.Close()
			p := NewGitHubProvider("token")
			p.apiBaseURL = server.URL
			id, err := p.EnsureDeployKey(context.Background(), "owner/repo", key)
			if status == 201 && (err != nil || id != 88) {
				t.Fatalf("id=%d err=%v", id, err)
			}
			if status == 403 && err == nil {
				t.Fatal("permission failure ignored")
			}
		})
	}
}
func TestDeployPublicKeyUsesPrivateKeyNotStalePublicFile(t *testing.T) {
	t.Setenv("FLUXO_ENV", "")
	t.Setenv("HOME", t.TempDir())
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal(err)
	}
	path := GetSSHKeyPath(9)
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path+".pub", []byte("stale public key"), 0644); err != nil {
		t.Fatal(err)
	}
	public, err := DeployPublicKey(9)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	if public != string(ssh.MarshalAuthorizedKey(signer.PublicKey())) {
		t.Fatal("wrong public key")
	}
	if _, err = DeployPublicKey(10); !errors.Is(err, ErrDeployKeyUnavailable) {
		t.Fatal("missing private key did not produce typed evidence", err)
	}
	command := DeploySSHCommand("/home/fluxo/key' name")
	for _, value := range []string{"IdentitiesOnly=yes", "BatchMode=yes", "-F /dev/null", "'\\''"} {
		if !strings.Contains(command, value) {
			t.Fatalf("command missing %q", value)
		}
	}
}
func TestIdentityCancellationIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	p := NewGitHubProvider("token")
	p.apiBaseURL = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := p.GetIdentity(ctx); err == nil {
		t.Fatal("canceled request succeeded")
	}
}

func TestDeployAccessIgnoresGitRewriteConfiguration(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "ssh")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	rewrite := "[url \"https://example.invalid/\"]\n insteadOf = git@github.com:\n"
	config := filepath.Join(dir, "config")
	if err := os.WriteFile(config, []byte(rewrite), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	if out, err := exec.Command("git", "-C", dir, "config", "url.https://example.invalid/.insteadOf", "git@github.com:").CombinedOutput(); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	t.Setenv("GIT_CONFIG_SYSTEM", config)
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "url.https://example.invalid/.insteadOf")
	t.Setenv("GIT_CONFIG_VALUE_0", "git@github.com:")
	t.Setenv("GIT_DIR", filepath.Join(dir, ".git"))
	ordinary := exec.Command("git", "ls-remote", "--get-url", "git@github.com:alice/app.git")
	if out, err := ordinary.Output(); err != nil || strings.TrimSpace(string(out)) != "https://example.invalid/alice/app.git" {
		t.Fatalf("fixture did not rewrite remote: %s %v", out, err)
	}
	cmd := exec.Command("/usr/bin/env", deployAccessArgs("alice/app", stub)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "git@github.com\n") || !strings.Contains(string(out), "git-upload-pack 'alice/app.git'") {
		t.Fatalf("expected the offline SSH stub to receive the original repository: %s %v", out, err)
	}
}
