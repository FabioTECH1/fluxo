package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/packages"
	"fluxo/internal/services/siteexecution"
	"fluxo/internal/syscmd"
	"github.com/gorilla/websocket"
)

type vmConsole struct {
	s                       *Server
	id                      int
	origin, password, token string
	client                  *http.Client
	dialer                  websocket.Dialer
}

func newVMConsole(t *testing.T) *vmConsole {
	t.Helper()
	requireTinkerVM(t)
	s, id, password, token := tinkerFixture(t)
	if _, err := database.DB.Exec("UPDATE sites SET app_type='laravel',php_version='8.4',path=? WHERE id=?", tinkerVMApp(t), id); err != nil {
		t.Fatal(err)
	}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("POST /api/v1/sites/{id}/tinker/sessions", s.handleCreateTinker())
	s.mux.HandleFunc("DELETE /api/v1/sites/{id}/tinker/sessions/{session_id}", s.handleDeleteTinker())
	s.mux.HandleFunc("GET /api/v1/tinker/ws", s.handleTinkerSocket())
	server := httptest.NewTLSServer(s)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		s.StopTinker(ctx)
		server.Close()
	})
	return &vmConsole{s: s, id: id, password: password, token: token, origin: server.URL, client: server.Client(), dialer: websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
}
func (v *vmConsole) grant(t *testing.T) map[string]string {
	t.Helper()
	r, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/sites/%d/tinker/sessions", v.origin, v.id), strings.NewReader(`{"password":"`+v.password+`"}`))
	r.Header.Set("Authorization", "Bearer "+v.token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", v.origin)
	resp, err := v.client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("grant %d: %s", resp.StatusCode, b)
	}
	var g struct {
		Session string `json:"session_id"`
		Ticket  string `json:"ticket"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&g); err != nil {
		t.Fatal(err)
	}
	return map[string]string{"session": g.Session, "ticket": g.Ticket}
}
func (v *vmConsole) connect(t *testing.T, g map[string]string) *websocket.Conn {
	t.Helper()
	ws, _, err := v.dialer.Dial("wss"+strings.TrimPrefix(v.origin, "https")+"/api/v1/tinker/ws?session="+g["session"]+"&ticket="+g["ticket"], http.Header{"Origin": []string{v.origin}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	vmReadUntil(t, ws, ">")
	return ws
}
func vmReadUntil(t *testing.T, ws *websocket.Conn, marker string) {
	t.Helper()
	var b strings.Builder
	ws.SetReadDeadline(time.Now().Add(15 * time.Second))
	for !strings.Contains(b.String(), marker) {
		_, data, err := ws.ReadMessage()
		if err != nil {
			t.Fatalf("waiting for %q: %v (%s)", marker, err, b.String())
		}
		b.Write(data)
		_ = ws.WriteJSON(map[string]any{"type": "ack", "bytes": len(data)})
		if b.Len() > 1024*1024 {
			t.Fatal("unexpected output flood")
		}
	}
}
func (v *vmConsole) waitClean(t *testing.T, id string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		v.s.tinker.mu.Lock()
		_, present := v.s.tinker.sessions[id]
		v.s.tinker.mu.Unlock()
		if !present {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("console session did not clean up")
}
func vmWaitClosed(t *testing.T, ws *websocket.Conn, reason string) {
	t.Helper()
	ws.SetReadDeadline(time.Now().Add(8 * time.Second))
	for {
		_, _, err := ws.ReadMessage()
		if err != nil {
			if _, ok := err.(*websocket.CloseError); !ok {
				t.Fatalf("connection did not close: %v", err)
			}
			if reason != "" && !strings.Contains(err.Error(), reason) {
				t.Fatalf("unexpected close: %v", err)
			}
			return
		}
	}
}
func TestVMTinkerAdversarialSessions(t *testing.T) {
	requireTinkerVM(t)
	t.Run("input idle survives neither resize nor heartbeat", func(t *testing.T) {
		v := newVMConsole(t)
		g := v.grant(t)
		ws := v.connect(t, g)
		v.s.tinker.mu.Lock()
		session := v.s.tinker.sessions[g["session"]]
		v.s.tinker.mu.Unlock()
		session.lastInput.Store(time.Now().Add(-6 * time.Minute).UnixNano())
		if err := ws.WriteJSON(map[string]any{"type": "resize", "rows": 30, "cols": 100}); err != nil {
			t.Fatal(err)
		}
		vmWaitClosed(t, ws, "inactivity")
		v.waitClean(t, g["session"])
	})
	t.Run("authentication deadline", func(t *testing.T) {
		v := newVMConsole(t)
		g := v.grant(t)
		v.s.tinker.mu.Lock()
		v.s.tinker.sessions[g["session"]].authExpires = time.Now().Add(7 * time.Second)
		v.s.tinker.mu.Unlock()
		ws := v.connect(t, g)
		vmWaitClosed(t, ws, "")
		v.waitClean(t, g["session"])
	})
	t.Run("oversized input", func(t *testing.T) {
		v := newVMConsole(t)
		g := v.grant(t)
		ws := v.connect(t, g)
		_ = ws.WriteJSON(map[string]any{"type": "input", "data": strings.Repeat("x", 20*1024)})
		vmWaitClosed(t, ws, "")
		v.waitClean(t, g["session"])
	})
	t.Run("output flood and slow network consumer", func(t *testing.T) {
		v := newVMConsole(t)
		g := v.grant(t)
		ws := v.connect(t, g)
		if err := ws.WriteJSON(map[string]any{"type": "input", "data": "while (true) { echo str_repeat('Z', 4096); }\r"}); err != nil {
			t.Fatal(err)
		}
		// Deliberately do not read output; application/server resources must remain bounded.
		v.waitClean(t, g["session"])
	})
	t.Run("rapid disconnect and reconnect", func(t *testing.T) {
		v := newVMConsole(t)
		for range 3 {
			g := v.grant(t)
			ws := v.connect(t, g)
			ws.Close()
			v.waitClean(t, g["session"])
			release, ok := siteexecution.Write(v.id)
			if !ok {
				t.Fatal("site gate leaked after disconnect")
			}
			release()
		}
	})
	t.Run("invalid output acknowledgement", func(t *testing.T) {
		v := newVMConsole(t)
		g := v.grant(t)
		ws := v.connect(t, g)
		if err := ws.WriteJSON(map[string]any{"type": "ack", "bytes": 256 * 1024}); err != nil {
			t.Fatal(err)
		}
		vmWaitClosed(t, ws, "")
		v.waitClean(t, g["session"])
	})
	t.Run("expired ticket", func(t *testing.T) {
		v := newVMConsole(t)
		g := v.grant(t)
		v.s.tinker.mu.Lock()
		v.s.tinker.sessions[g["session"]].expires = time.Now().Add(-time.Second)
		v.s.tinker.mu.Unlock()
		ws, resp, err := v.dialer.Dial("wss"+strings.TrimPrefix(v.origin, "https")+"/api/v1/tinker/ws?session="+g["session"]+"&ticket="+g["ticket"], http.Header{"Origin": []string{v.origin}})
		if err == nil {
			ws.Close()
			t.Fatal("expired ticket accepted")
		}
		if resp == nil || resp.StatusCode != 401 {
			t.Fatal("unexpected expired-ticket response")
		}
	})
	t.Run("server shutdown", func(t *testing.T) {
		v := newVMConsole(t)
		g := v.grant(t)
		ws := v.connect(t, g)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		v.s.StopTinker(ctx)
		vmWaitClosed(t, ws, "")
		v.waitClean(t, g["session"])
	})
}
func TestVMTinkerDelayedStartAndRestartAdmission(t *testing.T) {
	requireTinkerVM(t)
	// Hold exclusive package admission: the root helper must not bootstrap PHP.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	release, err := packages.Lifecycle(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	p, err := syscmd.StartTinker(tinkerRandom(), "8.4", tinkerVMApp(t))
	if err != nil {
		release()
		t.Fatal(err)
	}
	if err = p.Close(); err != nil {
		release()
		t.Fatal(err)
	}
	release()
	// Model a previous daemon's surviving cgroup: this process has no registry
	// entry for it. Both site mutation and new console admission must fail closed.
	id := tinkerRandom()
	unit := "fluxo-tinker-" + id + ".service"
	_, err = syscmd.Run(context.Background(), 5*time.Second, "/usr/bin/systemd-run", "--quiet", "--collect", "--unit="+unit, "/usr/bin/sleep", "30")
	if err != nil {
		t.Fatal(err)
	}
	defer syscmd.Run(context.Background(), 5*time.Second, "/usr/bin/systemctl", "stop", unit)
	if release, ok := siteexecution.Write(987654); ok {
		release()
		t.Fatal("mutation admitted across a previous daemon's cgroup")
	}
	if release, ok := siteexecution.Read(987654); ok {
		release()
		t.Fatal("new console admitted across a previous daemon's cgroup")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if release, err := packages.Lifecycle(ctx2, false); err == nil {
		release()
		t.Fatal("maintenance admitted across live interactive descendants")
	}
}
func TestVMTinkerBlockedPTYWrite(t *testing.T) {
	requireTinkerVM(t)
	p, err := syscmd.StartTinker(tinkerRandom(), "8.4", tinkerVMApp(t))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	read := make(chan struct{})
	go func() {
		defer close(read)
		var output strings.Builder
		buf := make([]byte, 4096)
		for !strings.Contains(output.String(), ">") {
			n, e := p.PTY.Read(buf)
			if e != nil {
				return
			}
			output.Write(buf[:n])
		}
	}()
	select {
	case <-read:
	case <-time.After(15 * time.Second):
		t.Fatal("Tinker prompt unavailable")
	}
	if err = p.Resize(35, 120); err != nil {
		t.Fatal(err)
	}
	if _, err = p.PTY.Write([]byte("system('stty raw -echo'); echo 'RAW_' . 'READY'; sleep(30);\r")); err != nil {
		t.Fatal(err)
	}
	var rawOutput strings.Builder
	buf := make([]byte, 4096)
	_ = p.PTY.SetReadDeadline(time.Now().Add(10 * time.Second))
	for !strings.Contains(rawOutput.String(), "RAW_READY") {
		n, e := p.PTY.Read(buf)
		if e != nil {
			t.Fatal(e)
		}
		rawOutput.Write(buf[:n])
	}
	if err = p.PTY.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal("PTY does not support bounded writes:", err)
	}
	done := make(chan error, 1)
	go func() { _, e := p.PTY.Write([]byte(strings.Repeat("x", 1024*1024))); done <- e }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("blocked input unexpectedly accepted all bytes")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("blocked PTY input ignored its deadline")
	}
}
