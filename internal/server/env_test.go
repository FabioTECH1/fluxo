package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
)

func TestEnvironmentCachePreferencePersistsPerSite(t *testing.T) {
	previous := database.DB
	dbPath := filepath.Join(t.TempDir(), "fluxo.db")
	if err := database.InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.DB.Close(); database.DB = previous })
	sitePath := t.TempDir()
	if _, err := database.DB.Exec("INSERT INTO sites (id, domain, path, app_type) VALUES (1, 'one.example', ?, 'laravel'), (2, 'two.example', ?, 'php')", sitePath, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	cacheCalls := 0
	cacheFails := false
	expectedDir := sitePath
	runCache := func(ctx context.Context, timeout time.Duration, user, dir, executable string, args ...string) (string, error) {
		cacheCalls++
		if user != "fluxo" || dir != expectedDir || executable != "php8.4" || strings.Join(args, " ") != "artisan config:cache" || timeout != 2*time.Minute {
			t.Fatalf("unexpected cache execution: %s %s %s %v", user, dir, executable, args)
		}
		if cacheFails {
			return "", errors.New("synthetic failure")
		}
		return "Configuration cached", nil
	}
	// Simulate a pre-preference database, then apply startup migrations.
	if _, err := database.DB.Exec("ALTER TABLE sites DROP COLUMN cache_config_after_env_save"); err != nil {
		t.Fatal(err)
	}
	database.DB.Close()
	if err := database.InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	save := func(id, payload string, want int) map[string]string {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
		req.SetPathValue("id", id)
		response := httptest.NewRecorder()
		s.handleUpdateEnvWithCacheRunner(runCache).ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("save: status %d body %s", response.Code, response.Body.String())
		}
		var result map[string]string
		if want == 200 {
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
		}
		return result
	}
	read := func(id string, want bool) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetPathValue("id", id)
		response := httptest.NewRecorder()
		s.handleGetEnv().ServeHTTP(response, req)
		var result struct {
			Content string `json:"content"`
			Cache   bool   `json:"cache_config_after_save"`
		}
		if response.Code != 200 {
			t.Fatalf("read status %d", response.Code)
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.Cache != want {
			t.Fatalf("site %s preference = %v, want %v", id, result.Cache, want)
		}
	}
	read("1", false)
	result := save("1", `{"content":"APP_NAME=Example","cache_config_after_save":true}`, 200)
	if result["cache_status"] != "success" || cacheCalls != 1 {
		t.Fatalf("cache result: %v, calls %d", result, cacheCalls)
	}
	read("1", true)
	read("2", false)
	// Old clients omit the new optional field without resetting the preference.
	save("1", `{"content":"APP_NAME=Updated"}`, 200)
	read("1", true)
	database.DB.Close()
	if err := database.InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	read("1", true)
	save("2", `{"content":"APP_NAME=Other","cache_config_after_save":true}`, 400)
	if _, err := database.DB.Exec("UPDATE sites SET app_type = NULL WHERE id = 2"); err != nil {
		t.Fatal(err)
	}
	save("2", `{"content":"APP_NAME=Legacy","cache_config_after_save":false}`, 200)
	if _, err := database.DB.Exec("UPDATE sites SET deployment_strategy = 'zero-downtime' WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	expectedDir = filepath.Join(sitePath, "current")
	cacheFails = true
	result = save("1", `{"content":"APP_NAME=Updated"}`, 200)
	if result["status"] != "saved" || result["cache_status"] != "failed" || result["cache_error"] == "" {
		t.Fatalf("failed cache result: %v", result)
	}
	read("1", true)
	before := cacheCalls
	save("1", `{"content":"APP_NAME=Updated","cache_config_after_save":false}`, 200)
	if cacheCalls != before {
		t.Fatal("disabled preference ran cache command")
	}
	read("1", false)
	// A failed file save must not persist the new preference.
	if _, err := database.DB.Exec("UPDATE sites SET path = ? WHERE id = 1", filepath.Join(sitePath, "missing")); err != nil {
		t.Fatal(err)
	}
	save("1", `{"content":"APP_NAME=Failed","cache_config_after_save":true}`, 500)
	read("1", false)
	if cacheCalls != before {
		t.Fatal("failed save ran cache command")
	}
	var history int
	if err := database.DB.QueryRow("SELECT COUNT(*) FROM commands").Scan(&history); err != nil {
		t.Fatal(err)
	}
	if history != 0 {
		t.Fatalf("automatic rebuild created %d command history entries", history)
	}
}
