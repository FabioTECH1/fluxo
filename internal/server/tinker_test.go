package server

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/siteexecution"
	"fluxo/internal/syscmd"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

func TestTinkerOrigin(t *testing.T) {
	for _, tc := range []struct {
		origin, host, remote, forwarded string
		tls, valid                      bool
	}{
		{"https://panel.test", "panel.test", "203.0.113.1:5", "", true, true},
		{"https://panel.test:9595", "panel.test:9595", "203.0.113.1:5", "", true, true},
		{"https://panel.test:9596", "panel.test:9595", "203.0.113.1:5", "", true, false},
		{"http://panel.test", "panel.test", "203.0.113.1:5", "", true, false},
		{"https://panel.test", "panel.test", "127.0.0.1:5", "https", false, true},
		{"https://panel.test", "panel.test", "203.0.113.1:5", "https", false, false},
		{"null", "panel.test", "127.0.0.1:5", "https", false, false},
		{"", "panel.test", "127.0.0.1:5", "https", false, false},
	} {
		r := httptest.NewRequest("GET", "https://"+tc.host+"/", nil)
		r.Host = tc.host
		r.RemoteAddr = tc.remote
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("X-Forwarded-Proto", tc.forwarded)
		if !tc.tls {
			r.TLS = nil
		}
		if got := tinkerOrigin(r) != ""; got != tc.valid {
			t.Errorf("%+v: got %v", tc, got)
		}
	}
}
func TestTinkerAttemptConcurrency(t *testing.T) {
	var m tinkerManager
	var wg sync.WaitGroup
	var allowed atomic.Int32
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if m.attempt("user:1", "ip:1") {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 5 {
		t.Fatalf("allowed %d attempts", allowed.Load())
	}
	if m.attempt("user:1", "ip:another") {
		t.Fatal("per-account limit bypassed")
	}
	if m.attempt("user:another", "ip:1") {
		t.Fatal("per-IP limit bypassed")
	}
}
func tinkerFixture(t *testing.T) (*Server, int, string, string) {
	t.Helper()
	s, id := setupCommandsTest(t)
	password := tinkerRandom()
	sum := sha256.Sum256([]byte(password))
	credential := hex.EncodeToString(sum[:])
	_, err := database.DB.Exec("INSERT INTO users(username,token_hash,token_version) VALUES('tinker-admin',?,3)", credential)
	if err != nil {
		t.Fatal(err)
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "tinker-admin", "ver": 3, "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte(credential))
	if err != nil {
		t.Fatal(err)
	}
	return s, id, password, token
}
func TestTinkerRejectsMissingClaimsAndRevocation(t *testing.T) {
	_, _, _, _ = tinkerFixture(t)
	r := httptest.NewRequest("POST", "https://panel.test/", nil)
	claims := jwt.MapClaims{"ver": float64(3), "exp": float64(time.Now().Add(time.Hour).Unix())}
	ctx := context.WithValue(r.Context(), userContextKey, "tinker-admin")
	ctx = context.WithValue(ctx, authClaimsContextKey, claims)
	r = r.WithContext(ctx)
	identity, err := tinkerIdentity(r)
	if err != nil {
		t.Fatal(err)
	}
	if !identity.valid() {
		t.Fatal("fresh identity rejected")
	}
	delete(claims, "ver")
	if _, err = tinkerIdentity(r); err == nil {
		t.Fatal("missing version accepted")
	}
	claims["ver"] = float64(3)
	delete(claims, "exp")
	if _, err = tinkerIdentity(r); err == nil {
		t.Fatal("missing expiry accepted")
	}
	_, err = database.DB.Exec("UPDATE users SET token_version=4 WHERE username='tinker-admin'")
	if err != nil {
		t.Fatal(err)
	}
	if identity.valid() {
		t.Fatal("revoked identity remained valid")
	}
}
func TestTinkerWrongPasswordAndOrigin(t *testing.T) {
	s, id, _, token := tinkerFixture(t)
	for _, tc := range []struct {
		origin, password string
		want             int
	}{{"https://panel.test", "wrong", 403}, {"https://evil.test", "wrong", 403}, {"", "wrong", 403}} {
		r := httptest.NewRequest("POST", fmt.Sprintf("https://panel.test/api/v1/sites/%d/tinker/sessions", id), strings.NewReader(`{"password":"`+tc.password+`"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Origin", tc.origin)
		r.SetPathValue("id", strconv.Itoa(id))
		w := httptest.NewRecorder()
		AuthMiddleware(s.handleCreateTinker()).ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("got %d: %s", w.Code, w.Body.String())
		}
	}
}
func TestTinkerMutationGate(t *testing.T) {
	release, ok := siteexecution.Read(823456)
	if !ok {
		t.Fatal("could not claim")
	}
	defer release()
	called := false
	s := &Server{}
	handler := s.tinkerMutationGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("PUT", "/api/v1/sites/823456", nil))
	if w.Code != 409 || called {
		t.Fatal("mutation passed active console")
	}
}
func TestTinkerTicketCannotReplayConcurrently(t *testing.T) {
	s, _, _, _ := tinkerFixture(t)
	// An invalidated identity fails immediately after atomic ticket consumption;
	// exactly one caller can reach that point, even for concurrent redemptions.
	ticket := tinkerRandom()
	digest := sha256.Sum256([]byte(ticket))
	id := tinkerRandom()
	session := &tinkerSession{id: id, username: "deleted-user", origin: "https://panel.test", digest: digest, expires: time.Now().Add(time.Minute), authExpires: time.Now().Add(time.Hour), done: make(chan struct{})}
	s.tinker.sessions = map[string]*tinkerSession{id: session}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("GET", "https://panel.test/api/v1/tinker/ws?session="+id+"&ticket="+ticket, nil)
			r.Header.Set("Origin", "https://panel.test")
			w := httptest.NewRecorder()
			s.handleTinkerSocket().ServeHTTP(w, r)
			if w.Code != 401 {
				t.Errorf("unexpected status %d", w.Code)
			}
		}()
	}
	wg.Wait()
	if !session.used {
		t.Fatal("ticket was not consumed")
	}
	select {
	case <-session.done:
	default:
		t.Fatal("cleanup did not complete")
	}
}

func TestVMTinkerPersistentConsole(t *testing.T) {
	requireTinkerVM(t)
	s, id, password, token := tinkerFixture(t)
	_, err := database.DB.Exec("UPDATE sites SET app_type='laravel',php_version='8.4',path=? WHERE id=?", tinkerVMApp(t), id)
	if err != nil {
		t.Fatal(err)
	}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("POST /api/v1/sites/{id}/tinker/sessions", s.handleCreateTinker())
	s.mux.HandleFunc("DELETE /api/v1/sites/{id}/tinker/sessions/{session_id}", s.handleDeleteTinker())
	s.mux.HandleFunc("GET /api/v1/tinker/ws", s.handleTinkerSocket())
	server := httptest.NewTLSServer(s)
	defer server.Close()
	client := server.Client()
	origin := server.URL
	grant := func() map[string]any {
		t.Helper()
		r, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/sites/%d/tinker/sessions", origin, id), strings.NewReader(`{"password":"`+password+`"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Origin", origin)
		resp, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		var g map[string]any
		if resp.StatusCode != 201 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("grant %d: %s", resp.StatusCode, b)
		}
		if e = json.NewDecoder(resp.Body).Decode(&g); e != nil {
			t.Fatal(e)
		}
		return g
	}
	g := grant()
	dialer := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	socketURL := "wss" + strings.TrimPrefix(origin, "https") + "/api/v1/tinker/ws?session=" + g["session_id"].(string) + "&ticket=" + g["ticket"].(string)
	ws, _, err := dialer.Dial(socketURL, http.Header{"Origin": []string{origin}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if other, resp, e := dialer.Dial(socketURL, http.Header{"Origin": []string{origin}}); e == nil {
		other.Close()
		t.Fatal("ticket replay succeeded")
	} else if resp == nil || resp.StatusCode != 401 {
		t.Fatal("wrong replay response")
	}
	readUntil := func(marker string) string {
		t.Helper()
		var b strings.Builder
		_ = ws.SetReadDeadline(time.Now().Add(20 * time.Second))
		for !strings.Contains(b.String(), marker) {
			_, data, e := ws.ReadMessage()
			if e != nil {
				t.Fatalf("waiting for %q: %v; output: %s", marker, e, b.String())
			}
			b.Write(data)
			_ = ws.WriteJSON(map[string]any{"type": "ack", "bytes": len(data)})
		}
		return b.String()
	}
	send := func(code string) {
		t.Helper()
		if e := ws.WriteJSON(map[string]any{"type": "input", "data": code + "\r"}); e != nil {
			t.Fatal(e)
		}
	}
	readUntil(">")
	send("$fluxoValue = 41;")
	readUntil("41")
	send("echo 'PERSIST_' . ($fluxoValue + 1);")
	readUntil("PERSIST_42")
	send("echo 'PHP_' . PHP_MAJOR_VERSION . '.' . PHP_MINOR_VERSION;")
	readUntil("PHP_8.4")
	send("echo 'IDENTITY_' . (posix_geteuid() > 0 && posix_getegid() > 0 ? 'NONROOT' : 'ROOT');")
	readUntil("IDENTITY_NONROOT")
	send("echo 'CLEAN_' . (getenv('FLUXO_TINKER_TEST_SECRET') === false ? 'YES' : 'NO');")
	readUntil("CLEAN_YES")
	send("echo 'NNP_' . trim(shell_exec('grep NoNewPrivs /proc/self/status'));")
	readUntil("NoNewPrivs:\t1")
	send("echo 'CAPS_' . (preg_match('/CapEff:\\s+0000000000000000/', file_get_contents('/proc/self/status')) && preg_match('/CapPrm:\\s+0000000000000000/', file_get_contents('/proc/self/status')) && preg_match('/CapAmb:\\s+0000000000000000/', file_get_contents('/proc/self/status')) ? 'ZERO' : 'NONZERO');")
	readUntil("CAPS_ZERO")
	send("echo 'GROUPS_' . (count(posix_getgroups()) === 0 ? 'EMPTY' : 'PRESENT');")
	readUntil("GROUPS_EMPTY")
	send("while (true) { usleep(50000); }")
	time.Sleep(300 * time.Millisecond)
	if err = ws.WriteJSON(map[string]any{"type": "input", "data": "\u0003"}); err != nil {
		t.Fatal(err)
	}
	send("echo 'AFTER_' . ($fluxoValue + 1);")
	readUntil("AFTER_42")
	if err = ws.WriteJSON(map[string]any{"type": "resize", "rows": 35, "cols": 120}); err != nil {
		t.Fatal(err)
	}
	// While the console owns a lifecycle lease, site mutations must fail closed.
	r := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/sites/%d", id), nil)
	w := httptest.NewRecorder()
	s.tinkerMutationGuard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("mutation admitted") })).ServeHTTP(w, r)
	if w.Code != 409 {
		t.Fatal("mutation not blocked")
	}
	// Spawn an independently sessioned child, then verify whole-cgroup cleanup.
	send("exec('setsid sleep 240 >/dev/null 2>&1 &'); echo 'DETACHED_READY';")
	readUntil("DETACHED_READY")
	if os.Getenv("FLUXO_TINKER_CRASH_TEST") == "1" {
		if err := os.WriteFile("/run/fluxo-tinker-crash-probe.json", []byte(g["session_id"].(string)), 0600); err != nil {
			t.Fatal(err)
		}
		for {
			time.Sleep(time.Second)
		}
	}

	_ = ws.Close()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		s.tinker.mu.Lock()
		n := len(s.tinker.sessions)
		s.tinker.mu.Unlock()
		if n == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.tinker.mu.Lock()
	n := len(s.tinker.sessions)
	s.tinker.mu.Unlock()
	if n != 0 {
		t.Fatal("session cleanup did not finish")
	}
	unit := "fluxo-tinker-" + g["session_id"].(string) + ".service"
	output, e := syscmd.Run(context.Background(), 5*time.Second, "/usr/bin/systemctl", "show", unit, "--property=LoadState", "--value")
	if e != nil || strings.TrimSpace(output) != "not-found" {
		t.Fatalf("execution domain survives: %s %v", output, e)
	}
	fresh := grant()
	freshURL := "wss" + strings.TrimPrefix(origin, "https") + "/api/v1/tinker/ws?session=" + fresh["session_id"].(string) + "&ticket=" + fresh["ticket"].(string)
	ws, _, err = dialer.Dial(freshURL, http.Header{"Origin": []string{origin}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	readUntil(">")
	send("echo 'FRESH_' . (isset($fluxoValue) ? 'NO' : 'YES');")
	readUntil("FRESH_YES")
	if _, err = database.DB.Exec("UPDATE users SET token_version=token_version+1 WHERE username='tinker-admin'"); err != nil {
		t.Fatal(err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, _, err = ws.ReadMessage()
		if err != nil {
			break
		}
	}
	if !websocket.IsCloseError(err, 1000) {
		t.Fatalf("revocation did not close session: %v", err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		s.tinker.mu.Lock()
		n = len(s.tinker.sessions)
		s.tinker.mu.Unlock()
		if n == 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if n != 0 {
		t.Fatal("revoked session cleanup did not finish")
	}
	var history int
	if err = database.DB.QueryRow("SELECT COUNT(*) FROM commands").Scan(&history); err != nil || history != 0 {
		t.Fatal("Tinker transcript entered command history")
	}
	t.Log("Verified real Tinker persistence, PHP selection, non-root identity, clean environment, ticket replay rejection, mutation exclusion and detached-child cleanup")
}
