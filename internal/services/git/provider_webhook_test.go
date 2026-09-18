package git

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func webhookProviderForTest(server *httptest.Server) *GitHubProvider {
	provider := NewGitHubProvider("test-token")
	provider.apiBaseURL = server.URL
	provider.httpClient = server.Client()
	return provider
}

func TestGitHubWebhookLifecycle(t *testing.T) {
	var patched bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization header = %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/app/hooks":
			if r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("page") != "1" {
				t.Fatalf("unexpected pagination: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id": 42, "active": true, "events": []string{"push"},
				"config": map[string]string{"url": "https://203.0.113.10:9595/api/v1/github/webhook"},
			}})
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/acme/app/hooks/42":
			var payload struct {
				Active bool              `json:"active"`
				Events []string          `json:"events"`
				Config map[string]string `json:"config"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if !payload.Active || len(payload.Events) != 1 || payload.Events[0] != "push" || payload.Config["secret"] != "current-secret" {
				t.Fatalf("unexpected update payload: %+v", payload)
			}
			patched = true
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 42})
		case r.Method == http.MethodDelete && r.URL.Path == "/repos/acme/app/hooks/42":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := webhookProviderForTest(server)
	hooks, err := provider.ListWebhooks("acme/app")
	if err != nil || len(hooks) != 1 || hooks[0].ID != 42 {
		t.Fatalf("ListWebhooks() = %+v, %v", hooks, err)
	}
	if err := provider.UpdateWebhook("acme/app", 42, hooks[0].Config.URL, "current-secret"); err != nil {
		t.Fatal(err)
	}
	if !patched {
		t.Fatal("webhook was not updated")
	}
	if err := provider.RemoveWebhook("acme/app", 42); err != nil {
		t.Fatal(err)
	}
}

func TestRegisterWebhookResolvesExistingID(t *testing.T) {
	callback := "https://203.0.113.10:9595/api/v1/github/webhook"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusUnprocessableEntity)
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id": 91, "config": map[string]string{"url": callback},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := webhookProviderForTest(server)
	id, err := provider.RegisterWebhook("acme/app", callback, "secret")
	if err != nil || id != 91 {
		t.Fatalf("RegisterWebhook() = %d, %v; want 91", id, err)
	}
}

func TestHasWebhookDeliveryVerifiesGUID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/repos/acme/app/hooks/42/deliveries" || r.URL.Query().Get("per_page") != "100" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 1, "guid": "11111111-2222-3333-4444-555555555555"},
		})
	}))
	defer server.Close()

	provider := webhookProviderForTest(server)
	verified, err := provider.HasWebhookDelivery("acme/app", 42, "11111111-2222-3333-4444-555555555555")
	if err != nil || !verified {
		t.Fatalf("HasWebhookDelivery() = %v, %v; want true", verified, err)
	}
	verified, err = provider.HasWebhookDelivery("acme/app", 42, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	if err != nil || verified {
		t.Fatalf("HasWebhookDelivery() = %v, %v; want false", verified, err)
	}
}
