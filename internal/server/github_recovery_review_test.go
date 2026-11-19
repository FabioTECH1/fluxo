package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"fluxo/internal/database"
	"fluxo/internal/services/git"
)

func TestGitHubRecoveryReviewShowsStoredAndProposedWithoutMutation(t *testing.T) {
	s, proposed, _, _ := legacyRecoveryFixture(t)
	proposed.id, proposed.username = 202, "new-owner"
	before, err := readGitHubAccount(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/v1/github/accounts/1/recovery-review", strings.NewReader(`{"token":"replacement"}`))
	r.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	s.handleReviewGitHubRecovery()(w, r)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var review githubRecoveryReview
	if err = json.Unmarshal(w.Body.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	if !review.NeedsRecovery || review.Stored.Username != "alice" || review.Stored.GitHubUserID != 0 || review.Proposed.Username != "new-owner" || review.Proposed.GitHubUserID != 202 || len(review.Sites) != 1 || review.Sites[0].Repository != "alice/app" {
		t.Fatalf("wrong review: %+v", review)
	}
	if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "replacement") || strings.Contains(w.Body.String(), "old-token") {
		t.Fatal("review cached or exposed credentials")
	}
	after, err := readGitHubAccount(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	if string(beforeJSON) != string(afterJSON) || after.Token != before.Token || proposed.registrations != 0 {
		t.Fatal("review changed account, links or keys")
	}
}

func TestGitHubRecoveryReviewKeepsOrdinaryOwnershipChecks(t *testing.T) {
	for _, scenario := range []string{"valid-old", "mismatched-old", "known-owner", "forbidden-old", "invalid-new"} {
		t.Run(scenario, func(t *testing.T) {
			s, proposed, saved, _ := legacyRecoveryFixture(t)
			expected := 200
			switch scenario {
			case "valid-old":
				saved.id, saved.username, saved.identityErr = proposed.id, proposed.username, nil
			case "mismatched-old":
				saved.id, saved.identityErr, expected = 999, nil, 409
			case "known-owner":
				database.DB.Exec("UPDATE github_accounts SET github_user_id=101 WHERE id=1")
				expected = 409
			case "forbidden-old":
				saved.identityErr, expected = &git.APIError{Status: 403}, 503
			case "invalid-new":
				proposed.identityErr, expected = &git.APIError{Status: 401}, 400
			}
			w := accountRequest(s.handleReviewGitHubRecovery(), "POST", "/api/v1/github/accounts/1/recovery-review", `{"token":"replacement"}`)
			if w.Code != expected {
				t.Fatalf("%d want %d: %s", w.Code, expected, w.Body.String())
			}
			if scenario == "valid-old" {
				var review githubRecoveryReview
				if err := json.Unmarshal(w.Body.Bytes(), &review); err != nil || review.NeedsRecovery {
					t.Fatal("ordinary replacement should not require recovery", err)
				}
			}
		})
	}
}

func TestGitHubRecoveryApprovalBindsReviewedNumericIdentity(t *testing.T) {
	for _, scenario := range []string{"same", "changed", "zero", "negative"} {
		t.Run(scenario, func(t *testing.T) {
			s, proposed, _, body := legacyRecoveryFixture(t)
			id, expected := proposed.id, 200
			switch scenario {
			case "changed":
				id, expected = id+1, 409
			case "zero":
				id, expected = 0, 400
			case "negative":
				id, expected = -1, 400
			}
			body.LegacyRecovery.GitHubUserID = &id
			w := legacyRecoveryRequest(t, s, body, true, 7)
			if w.Code != expected {
				t.Fatalf("%d want %d: %s", w.Code, expected, w.Body.String())
			}
		})
	}
}
