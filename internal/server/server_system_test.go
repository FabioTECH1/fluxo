package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/services/hostsystem"
)

func TestServerSystemAuthorizationAndStrictRequests(t *testing.T) {
	s := NewServer(nil, t.TempDir(), false)
	for _, route := range []struct{ method, path string }{{"GET", "/api/v1/server/system"}, {"PUT", "/api/v1/server/system/security-updates"}, {"POST", "/api/v1/server/system/reboots"}, {"GET", "/api/v1/server/system/reboots/00000000-0000-4000-8000-000000000001/services"}} {
		r := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("%s %d", route.path, w.Code)
		}
	}
	s.hostSystemAdmission = func(context.Context) (func(), error) { t.Fatal("admitted malformed request"); return nil, nil }
	for _, req := range []struct{ path, body string }{{"security-updates", `{}`}, {"security-updates", `{"enabled":null}`}, {"security-updates", `{"enabled":"true"}`}, {"security-updates", `{"enabled":true,"extra":1}`}, {"reboots", `{"boot_id":"wrong","confirm":true}`}, {"reboots", `{"boot_id":"00000000-0000-4000-8000-000000000001","confirm":false}`}} {
		method := "PUT"
		if req.path == "reboots" {
			method = "POST"
		}
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, httptest.NewRequest(method, "/api/v1/server/system/"+req.path, strings.NewReader(req.body)))
		if w.Code != 400 {
			t.Fatalf("%s %d %s", req.body, w.Code, w.Body.String())
		}
	}
}
func TestServerSystemRebootAcceptedAndDuplicate(t *testing.T) {
	root := t.TempDir()
	if e := os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	boot := "00000000-0000-4000-8000-000000000001"
	for _, dir := range []string{"run/lock", "var/lib/fluxo", "proc/sys/kernel/random"} {
		if e := os.MkdirAll(filepath.Join(root, dir), 0700); e != nil {
			t.Fatal(e)
		}
	}
	os.WriteFile(filepath.Join(root, "proc/sys/kernel/random/boot_id"), []byte(boot), 0600)
	s := NewServer(nil, t.TempDir(), false)
	held := false
	calls := 0
	loseState := false
	s.hostSystemAdmission = func(context.Context) (func(), error) { held = true; return func() { held = false }, nil }
	s.hostSystem = &hostsystem.Host{Root: root, Run: func(ctx context.Context, name string, a ...string) (string, error) {
		if !held {
			t.Fatal("schedule outside admission")
		}
		if name == "/usr/bin/systemctl" {
			return "LoadState=not-found\n", nil
		}
		if name == "/usr/bin/systemd-run" {
			calls++
			if loseState {
				return "", os.Remove(filepath.Join(root, "var/lib/fluxo/server-reboot.json"))
			}
			var v hostsystem.Reboot
			p := filepath.Join(root, "var/lib/fluxo/server-reboot.json")
			b, _ := os.ReadFile(p)
			json.Unmarshal(b, &v)
			v.Phase = "accepted"
			b, _ = json.Marshal(v)
			return "", os.WriteFile(p, b, 0600)
		}
		return "", errors.New("unexpected command")
	}}
	request := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/server/system/reboots", strings.NewReader(`{"boot_id":"`+boot+`","confirm":true}`)))
		return w
	}
	w := request()
	if w.Code != 202 || held {
		t.Fatalf("%d %s held=%v", w.Code, w.Body.String(), held)
	}
	w = request()
	if w.Code != 409 || calls != 1 {
		t.Fatalf("duplicate %d %s", w.Code, w.Body.String())
	}
	// A status read never calls the scheduling command, including after failure.
	previous, _ := os.ReadFile(filepath.Join(root, "var/lib/fluxo/server-reboot.json"))
	boot = "00000000-0000-4000-8000-000000000002"
	os.WriteFile(filepath.Join(root, "proc/sys/kernel/random/boot_id"), []byte(boot), 0600)
	s.hostSystemAdmission = func(context.Context) (func(), error) { return nil, errors.New("package installation active") }
	w = request()
	if w.Code != 409 || calls != 1 {
		t.Fatal(w.Code, calls)
	}
	var refusal map[string]any
	json.Unmarshal(w.Body.Bytes(), &refusal)
	after, _ := os.ReadFile(filepath.Join(root, "var/lib/fluxo/server-reboot.json"))
	if refusal["code"] != "restart_not_scheduled" || string(after) != string(previous) {
		t.Fatalf("blocked attempt must preserve the older completed record: %s", w.Body.String())
	}
	// Losing the acknowledgement/state after scheduling must never become a
	// definite refusal. Preserve the original known request identity.
	s.hostSystemAdmission = func(context.Context) (func(), error) { held = true; return func() { held = false }, nil }
	loseState = true
	w = request()
	var uncertain struct {
		Code   string            `json:"code"`
		Reboot hostsystem.Reboot `json:"reboot"`
	}
	json.Unmarshal(w.Body.Bytes(), &uncertain)
	if w.Code != 503 || uncertain.Code != "restart_unconfirmed" || uncertain.Reboot.ID == "" || uncertain.Reboot.Phase != "preparing" || calls != 2 || held {
		t.Fatalf("lost state is not a refusal: %d %s", w.Code, w.Body.String())
	}
}

func TestRestartSchedulingErrorOutcomes(t *testing.T) {
	for _, tc := range []struct {
		phase, code string
		status      int
	}{{"failed", "restart_failed", 409}, {"preparing", "restart_unconfirmed", 503}, {"accepted", "restart_unconfirmed", 503}} {
		w := httptest.NewRecorder()
		restartSchedulingError(w, hostsystem.Reboot{ID: "kept-request", Phase: tc.phase}, errors.New("recorded failure"))
		var body struct {
			Code   string            `json:"code"`
			Reboot hostsystem.Reboot `json:"reboot"`
		}
		json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != tc.status || body.Code != tc.code || body.Reboot.ID != "kept-request" {
			t.Fatalf("%s: %d %s", tc.phase, w.Code, w.Body.String())
		}
	}
}
