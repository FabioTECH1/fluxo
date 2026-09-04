package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"fluxo/internal/database"
)

func setupSiteVhostTest(t *testing.T) int {
	t.Helper()
	previousDB := database.DB
	previousRender := renderSiteVhostDefault
	previousInstall := installSiteVhost
	if err := database.InitDB(filepath.Join(t.TempDir(), "fluxo.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	result, err := database.DB.Exec(
		"INSERT INTO sites (domain, path, php_version, app_type, web_root) VALUES (?, ?, ?, ?, ?)",
		"example.com", "/home/fluxo/example.com", "8.4", "php", "/public",
	)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("site ID: %v", err)
	}
	t.Cleanup(func() {
		_ = database.DB.Close()
		database.DB = previousDB
		renderSiteVhostDefault = previousRender
		installSiteVhost = previousInstall
	})
	return int(siteID)
}

func vhostRequest(method, target string, siteID int, payload any) *http.Request {
	var body bytes.Buffer
	if payload != nil {
		_ = json.NewEncoder(&body).Encode(payload)
	}
	request := httptest.NewRequest(method, target, &body)
	request.SetPathValue("id", strconv.Itoa(siteID))
	return request
}

func TestNormalizeSiteVhost(t *testing.T) {
	config, err := normalizeSiteVhost("server {\r\n    listen 80;\r\n}")
	if err != nil {
		t.Fatalf("normalize vhost: %v", err)
	}
	if config != "server {\n    listen 80;\n}\n" {
		t.Fatalf("normalized vhost = %q", config)
	}
	for _, invalid := range []string{"", " \n\t", "server {\x00}"} {
		if _, err := normalizeSiteVhost(invalid); err == nil {
			t.Fatalf("invalid vhost %q was accepted", invalid)
		}
	}
	if _, err := normalizeSiteVhost(strings.Repeat("x", maxSiteVhostSize+1)); err == nil {
		t.Fatal("oversized vhost was accepted")
	}
}

func TestDecodeVhostPayloadAllowsEscapedConfigWithinDecodedLimit(t *testing.T) {
	config := strings.Repeat(`\\`, maxSiteVhostSize/2)
	request := vhostRequest(http.MethodPut, "/api/v1/sites/1/vhost", 1, updateSiteVhostRequest{
		Config: config, ExpectedRevision: "revision",
	})
	recorder := httptest.NewRecorder()
	var payload updateSiteVhostRequest
	if err := decodeVhostPayload(recorder, request, &payload); err != nil {
		t.Fatalf("decode escaped vhost payload: %v", err)
	}
	if payload.Config != config {
		t.Fatal("decoded escaped vhost changed")
	}
}

func TestUpdateSiteVhostPersistsOnlyAfterActivation(t *testing.T) {
	siteID := setupSiteVhostTest(t)
	oldConfig := "server { listen 80; server_name example.com; }\n"
	if err := database.SaveSiteVhostOverride(siteID, oldConfig); err != nil {
		t.Fatalf("seed override: %v", err)
	}
	current, err := loadSiteVhostState(siteID)
	if err != nil {
		t.Fatalf("load current state: %v", err)
	}

	newConfig := "server {\r\n    listen 8080;\r\n}"
	installed := ""
	installSiteVhost = func(_ context.Context, name, config string) error {
		if name != "example.com" {
			t.Fatalf("config name = %q", name)
		}
		installed = config
		return nil
	}
	recorder := httptest.NewRecorder()
	request := vhostRequest(http.MethodPut, "/api/v1/sites/1/vhost", siteID, updateSiteVhostRequest{
		Config: newConfig, ExpectedRevision: current.Revision,
	})
	(&Server{}).handleUpdateSiteVhost().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	want := "server {\n    listen 8080;\n}\n"
	if installed != want {
		t.Fatalf("installed config = %q", installed)
	}
	override, err := database.GetSiteVhostOverride(siteID)
	if err != nil || override == nil || override.Config != want {
		t.Fatalf("saved override = %#v, err = %v", override, err)
	}
}

func TestUpdateSiteVhostLeavesStoredConfigWhenActivationFails(t *testing.T) {
	siteID := setupSiteVhostTest(t)
	oldConfig := "server { listen 80; }\n"
	if err := database.SaveSiteVhostOverride(siteID, oldConfig); err != nil {
		t.Fatalf("seed override: %v", err)
	}
	current, err := loadSiteVhostState(siteID)
	if err != nil {
		t.Fatalf("load current state: %v", err)
	}
	installSiteVhost = func(context.Context, string, string) error {
		return errors.New("nginx config test failed")
	}

	recorder := httptest.NewRecorder()
	request := vhostRequest(http.MethodPut, "/api/v1/sites/1/vhost", siteID, updateSiteVhostRequest{
		Config: "server { broken on; }", ExpectedRevision: current.Revision,
	})
	(&Server{}).handleUpdateSiteVhost().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("update status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	override, err := database.GetSiteVhostOverride(siteID)
	if err != nil || override == nil || override.Config != oldConfig {
		t.Fatalf("override after failure = %#v, err = %v", override, err)
	}
}

func TestUpdateSiteVhostRejectsStaleRevision(t *testing.T) {
	siteID := setupSiteVhostTest(t)
	oldConfig := "server { listen 80; }\n"
	if err := database.SaveSiteVhostOverride(siteID, oldConfig); err != nil {
		t.Fatalf("seed override: %v", err)
	}
	installCalled := false
	installSiteVhost = func(context.Context, string, string) error {
		installCalled = true
		return nil
	}

	recorder := httptest.NewRecorder()
	request := vhostRequest(http.MethodPut, "/api/v1/sites/1/vhost", siteID, updateSiteVhostRequest{
		Config: "server { listen 8080; }", ExpectedRevision: "stale",
	})
	(&Server{}).handleUpdateSiteVhost().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("update status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if installCalled {
		t.Fatal("stale vhost was installed")
	}
}

func TestRestoreSiteVhostGeneratesCurrentDefaultAndDeletesOverride(t *testing.T) {
	siteID := setupSiteVhostTest(t)
	customConfig := "server { listen 8080; }\n"
	if err := database.SaveSiteVhostOverride(siteID, customConfig); err != nil {
		t.Fatalf("seed override: %v", err)
	}
	current, err := loadSiteVhostState(siteID)
	if err != nil {
		t.Fatalf("load current state: %v", err)
	}
	defaultConfig := "server { listen 80; server_name example.com; }\n"
	renderSiteVhostDefault = func(gotSiteID int) (renderedSiteVhost, error) {
		if gotSiteID != siteID {
			t.Fatalf("render site ID = %d", gotSiteID)
		}
		return renderedSiteVhost{ConfigName: "example.com", Content: defaultConfig}, nil
	}
	installed := ""
	installSiteVhost = func(_ context.Context, _ string, config string) error {
		installed = config
		return nil
	}

	recorder := httptest.NewRecorder()
	request := vhostRequest(http.MethodPost, "/api/v1/sites/1/vhost/restore", siteID, restoreSiteVhostRequest{
		ExpectedRevision: current.Revision,
	})
	(&Server{}).handleRestoreSiteVhost().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("restore status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if installed != defaultConfig {
		t.Fatalf("installed default = %q", installed)
	}
	override, err := database.GetSiteVhostOverride(siteID)
	if err != nil || override != nil {
		t.Fatalf("override after restore = %#v, err = %v", override, err)
	}
	var response siteVhostResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode restore response: %v", err)
	}
	if response.Customized || response.Config != defaultConfig || !strings.HasSuffix(response.Path, "/example.com") {
		t.Fatalf("restore response = %#v", response)
	}
}

func TestRestoreSiteVhostLeavesOverrideWhenActivationFails(t *testing.T) {
	siteID := setupSiteVhostTest(t)
	customConfig := "server { listen 8080; }\n"
	if err := database.SaveSiteVhostOverride(siteID, customConfig); err != nil {
		t.Fatalf("seed override: %v", err)
	}
	current, err := loadSiteVhostState(siteID)
	if err != nil {
		t.Fatalf("load current state: %v", err)
	}
	renderSiteVhostDefault = func(int) (renderedSiteVhost, error) {
		return renderedSiteVhost{ConfigName: "example.com", Content: "server { listen 80; }\n"}, nil
	}
	installSiteVhost = func(context.Context, string, string) error {
		return errors.New("Nginx reload failed")
	}

	recorder := httptest.NewRecorder()
	request := vhostRequest(http.MethodPost, "/api/v1/sites/1/vhost/restore", siteID, restoreSiteVhostRequest{
		ExpectedRevision: current.Revision,
	})
	(&Server{}).handleRestoreSiteVhost().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("restore status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	override, err := database.GetSiteVhostOverride(siteID)
	if err != nil || override == nil || override.Config != customConfig {
		t.Fatalf("override after failed restore = %#v, err = %v", override, err)
	}
}

func TestRegenerateNginxPreservesDurableVhostOverride(t *testing.T) {
	siteID := setupSiteVhostTest(t)
	previousRender := renderRegeneratedSiteVhost
	previousInstall := installRegeneratedSiteVhost
	t.Cleanup(func() {
		renderRegeneratedSiteVhost = previousRender
		installRegeneratedSiteVhost = previousInstall
	})

	customConfig := "server { listen 8080; }\n"
	if err := database.SaveSiteVhostOverride(siteID, customConfig); err != nil {
		t.Fatalf("seed override: %v", err)
	}
	renderRegeneratedSiteVhost = func(gotSiteID int) (renderedSiteVhost, error) {
		if gotSiteID != siteID {
			t.Fatalf("render site ID = %d", gotSiteID)
		}
		return renderedSiteVhost{ConfigName: "example.com", Content: "server { listen 80; }\n"}, nil
	}
	installed := ""
	installRegeneratedSiteVhost = func(_ context.Context, name, config string) error {
		if name != "example.com" {
			t.Fatalf("config name = %q", name)
		}
		installed = config
		return nil
	}

	if err := regenerateNginxForSiteWithError(siteID); err != nil {
		t.Fatalf("regenerate Nginx: %v", err)
	}
	if installed != customConfig {
		t.Fatalf("regeneration installed %q instead of custom override", installed)
	}
}
