package git

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRepositoryBranchLookupIsExactReadOnlyAndRejectsRedirects(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer fixture-token" || r.URL.EscapedPath() != "/repos/Company/project/branches/feature%2Fone" {
			t.Error("lookup changed transport, identity or saved branch", r.Method, r.URL.EscapedPath())
		}
		fmt.Fprint(w, `{"name":"feature/one"}`)
	}))
	defer server.Close()
	p := NewGitHubProvider("fixture-token")
	p.apiBaseURL = server.URL
	if err := p.CheckRepositoryBranch(context.Background(), "Company/project", "feature/one"); err != nil || calls != 1 {
		t.Fatal(err, calls)
	}
	for _, repo := range []string{"https://other.invalid/repo", "company/repo\n", "../../repo"} {
		if err := p.CheckRepositoryBranch(context.Background(), repo, "main"); err == nil {
			t.Fatal("unsafe input accepted", repo)
		}
	}
	if calls != 1 {
		t.Fatal("invalid input made a request")
	}
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("repository redirect followed")
		fmt.Fprint(w, `{"name":"main"}`)
	}))
	defer destination.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer redirect.Close()
	p.apiBaseURL = redirect.URL
	var apiErr *APIError
	if err := p.CheckRepositoryBranch(context.Background(), "company/repo", "main"); !errors.As(err, &apiErr) || apiErr.Status != 302 {
		t.Fatal("redirect could qualify an unrelated destination", err)
	}
}

func TestRepositoryBranchLookupDoesNotExposeResponseBodies(t *testing.T) {
	for _, status := range []int{200, 401, 403, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				fmt.Fprint(w, `{"name":"wrong-branch","detail":"fixture-token must not be exposed"}`)
			}))
			defer server.Close()
			p := NewGitHubProvider("fixture-token")
			p.apiBaseURL = server.URL
			err := p.CheckRepositoryBranch(context.Background(), "company/repo", "main")
			if err == nil || strings.Contains(err.Error(), "fixture-token") {
				t.Fatal("unverified branch or response contents accepted", err)
			}
		})
	}
}
