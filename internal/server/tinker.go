package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/services/packages"
	sitepkg "fluxo/internal/services/site"
	"fluxo/internal/services/siteexecution"
	"fluxo/internal/syscmd"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

const tinkerTicketTTL = 30 * time.Second
const tinkerLifetime = 15 * time.Minute
const tinkerIdle = 5 * time.Minute

type tinkerSession struct {
	id, username, origin, credential string
	siteID, userID, version          int
	digest                           [32]byte
	expires, authExpires             time.Time
	used                             bool
	cancel                           context.CancelFunc
	done                             chan struct{}
	lastInput                        atomic.Int64
}
type tinkerAttempt struct {
	count int
	until time.Time
}
type tinkerManager struct {
	stopping bool
	mu       sync.Mutex
	sessions map[string]*tinkerSession
	attempts map[string]tinkerAttempt
}

func tinkerRandom() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("secure random unavailable")
	}
	return hex.EncodeToString(b[:])
}

// Origin uses the original Host and direct transport. Only the loopback Nginx
// panel proxy may supply X-Forwarded-Proto; forwarded hosts are never trusted.
func tinkerOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	} else {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() && r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
	}
	origin := scheme + "://" + r.Host
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || r.Header.Get("Origin") != origin {
		return ""
	}
	return origin
}
func (m *tinkerManager) attempt(keys ...string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.attempts == nil {
		m.attempts = map[string]tinkerAttempt{}
	}
	now := time.Now()
	for k, a := range m.attempts {
		if !now.Before(a.until) {
			delete(m.attempts, k)
		}
	}
	if len(m.attempts) > 4096 {
		return false
	}
	for _, k := range keys {
		if m.attempts[k].count >= 5 {
			return false
		}
	}
	for _, k := range keys {
		a := m.attempts[k]
		if a.count == 0 {
			a.until = now.Add(15 * time.Minute)
		}
		a.count++
		m.attempts[k] = a
	}
	return true
}
func tinkerIdentity(r *http.Request) (*tinkerSession, error) {
	claims, ok := r.Context().Value(authClaimsContextKey).(jwt.MapClaims)
	if !ok {
		return nil, errors.New("Invalid authentication")
	}
	exp, err := claims.GetExpirationTime()
	v, ok := claims["ver"].(float64)
	if err != nil || exp == nil || !time.Now().Before(exp.Time) || !ok || v != float64(int(v)) {
		return nil, errors.New("Please sign in again")
	}
	t := &tinkerSession{username: usernameFromContext(r.Context()), version: int(v), authExpires: exp.Time}
	if t.username == "" || t.username == "__bootstrap__" {
		return nil, errors.New("Invalid authentication")
	}
	var ver int
	err = database.DB.QueryRow("SELECT id,token_hash,token_version FROM users WHERE username=?", t.username).Scan(&t.userID, &t.credential, &ver)
	if err != nil || ver != t.version {
		return nil, errors.New("Authentication changed")
	}
	return t, nil
}
func (t *tinkerSession) valid() bool {
	if !time.Now().Before(t.authExpires) {
		return false
	}
	var credential string
	var version int
	err := database.DB.QueryRow("SELECT token_hash,token_version FROM users WHERE id=? AND username=?", t.userID, t.username).Scan(&credential, &version)
	return err == nil && credential == t.credential && version == t.version
}
func tinkerSite(id int) (string, string, error) {
	var path, strategy, version, kind, deletion string
	err := database.DB.QueryRow("SELECT path,COALESCE(deployment_strategy,'standard'),php_version,app_type,COALESCE(deletion_status,'') FROM sites WHERE id=?", id).Scan(&path, &strategy, &version, &kind, &deletion)
	if err != nil {
		return "", "", errors.New("Site not found")
	}
	if kind != "laravel" {
		return "", "", errors.New("Tinker is available only for Laravel sites")
	}
	if deletion != "" {
		return "", "", errors.New("Site deletion is in progress")
	}
	var running int
	err = database.DB.QueryRow("SELECT COUNT(*) FROM deployments WHERE site_id=? AND status IN ('pending','running')", id).Scan(&running)
	if err != nil || running != 0 {
		return "", "", errors.New("Wait for site deployments to finish")
	}
	if busy, err := database.PHPMaintenancePending(); err != nil || busy {
		return "", "", errors.New("Runtime maintenance is pending")
	}
	return sitepkg.ActiveSitePath(path, strategy), version, nil
}
func (s *Server) handleCreateTinker() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		origin := tinkerOrigin(r)
		if origin == "" || !strings.HasPrefix(origin, "https://") {
			http.Error(w, "Tinker requires HTTPS and an exact same Origin", 403)
			return
		}
		identity, err := tinkerIdentity(r)
		if err != nil {
			http.Error(w, err.Error(), 401)
			return
		}
		id, err := parseCommandPathID(r, "id", "site")
		if err != nil {
			http.Error(w, "Invalid site", 400)
			return
		}
		if !s.tinker.attempt("user:"+strconv.Itoa(identity.userID), "ip:"+getClientIP(r)) {
			http.Error(w, "Too many console connection attempts; retry in 15 minutes", 429)
			return
		}
		var req struct {
			Password string `json:"password"`
		}
		if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
			http.Error(w, "Content-Type must be application/json", 415)
			return
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil || len(req.Password) == 0 || len(req.Password) > 1024 {
			http.Error(w, "A password of at most 1024 bytes is required", 400)
			return
		}
		if dec.Decode(new(any)) != io.EOF {
			http.Error(w, "A single JSON object is required", 400)
			return
		}
		if !verifyPassword(req.Password, identity.credential) {
			http.Error(w, "Invalid password", http.StatusForbidden)
			return
		}
		req.Password = ""
		if !identity.valid() {
			http.Error(w, "Authentication changed", 401)
			return
		}
		if _, _, err = tinkerSite(id); err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		if _, err = syscmd.TinkerOwner(); err != nil {
			http.Error(w, err.Error(), 503)
			return
		}
		if err = syscmd.TinkerPlatform(r.Context()); err != nil {
			http.Error(w, err.Error(), 503)
			return
		}
		identity.siteID = id
		identity.origin = origin
		identity.id = tinkerRandom()
		identity.expires = time.Now().Add(tinkerTicketTTL)
		identity.done = make(chan struct{})
		ticket := tinkerRandom() + tinkerRandom()
		identity.digest = sha256.Sum256([]byte(ticket))
		m := &s.tinker
		m.mu.Lock()
		if m.sessions == nil {
			m.sessions = map[string]*tinkerSession{}
		}
		for k, t := range m.sessions {
			if !t.used && !time.Now().Before(t.expires) {
				delete(m.sessions, k)
			}
		}
		if m.stopping {
			m.mu.Unlock()
			http.Error(w, "Server is shutting down", 503)
			return
		}
		if len(m.sessions) >= 4 {
			m.mu.Unlock()
			http.Error(w, "Console capacity reached", 409)
			return
		}
		for _, t := range m.sessions {
			if t.userID == identity.userID && t.siteID == id {
				m.mu.Unlock()
				http.Error(w, "Disconnect the existing Tinker session first", 409)
				return
			}
		}
		m.sessions[identity.id] = identity
		m.mu.Unlock()
		phpJSON(w, 201, map[string]any{"session_id": identity.id, "ticket": ticket, "expires_at": identity.expires, "max_seconds": 900, "idle_seconds": 300})
	}
}
func (s *Server) handleDeleteTinker() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if tinkerOrigin(r) == "" {
			http.Error(w, "Invalid Origin", 403)
			return
		}
		id, err := parseCommandPathID(r, "id", "site")
		if err != nil {
			http.Error(w, "Invalid site", 400)
			return
		}
		m := &s.tinker
		m.mu.Lock()
		t := m.sessions[r.PathValue("session_id")]
		if t == nil || t.siteID != id || t.username != usernameFromContext(r.Context()) {
			m.mu.Unlock()
			http.NotFound(w, r)
			return
		}
		if t.cancel != nil {
			t.cancel()
		} else if !t.used {
			delete(m.sessions, t.id)
		}
		m.mu.Unlock()
		w.WriteHeader(204)
	}
}
func (s *Server) handleTinkerSocket() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		origin := tinkerOrigin(r)
		if origin == "" || !strings.HasPrefix(origin, "https://") {
			http.Error(w, "Invalid Origin", 403)
			return
		}
		digest := sha256.Sum256([]byte(r.URL.Query().Get("ticket")))
		m := &s.tinker
		m.mu.Lock()
		t := m.sessions[r.URL.Query().Get("session")]
		if m.stopping || t == nil || t.used || t.origin != origin || !time.Now().Before(t.expires) || subtle.ConstantTimeCompare(digest[:], t.digest[:]) != 1 {
			m.mu.Unlock()
			http.Error(w, "Invalid or expired console ticket", 401)
			return
		}
		t.used = true
		t.digest = [32]byte{}
		ctx, cancel := context.WithDeadline(context.Background(), minTime(t.authExpires, time.Now().Add(tinkerLifetime)))
		t.cancel = cancel
		m.mu.Unlock()
		cleanupSafe := true
		defer func() {
			cancel()
			m.mu.Lock()
			if cleanupSafe {
				delete(m.sessions, t.id)
			}
			m.mu.Unlock()
			close(t.done)
		}()
		if !t.valid() {
			http.Error(w, "Authentication changed", 401)
			return
		}
		releaseSite, ok := siteexecution.Read(t.siteID)
		if !ok {
			http.Error(w, "Site is busy", 409)
			return
		}
		defer func() {
			if cleanupSafe {
				releaseSite()
			}
		}()
		lockctx, lcancel := context.WithTimeout(ctx, 2*time.Second)
		release, err := packages.Lifecycle(lockctx, true)
		lcancel()
		if err != nil {
			http.Error(w, "Runtime maintenance is busy", 409)
			return
		}
		// On uncertain cleanup deliberately retain this lease and capacity until daemon
		// restart; systemd BindsTo and the package cgroup check protect crash recovery.
		defer func() {
			if cleanupSafe {
				release()
			}
		}()
		path, version, err := tinkerSite(t.siteID)
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		up := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return tinkerOrigin(r) == origin }, HandshakeTimeout: 5 * time.Second}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if ctx.Err() != nil {
			return
		}
		p, err := syscmd.StartTinker(t.id, version, path)
		if err != nil {
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1011, "Tinker could not start"), time.Now().Add(time.Second))
			return
		}
		defer func() {
			if err := p.Close(); err != nil {
				cleanupSafe = false
				log.Printf("Tinker cleanup unresolved: session=%s site=%d", t.id, t.siteID)
			}
		}()
		log.Printf("Tinker connected: actor=%d site=%d session=%s", t.userID, t.siteID, t.id)
		defer log.Printf("Tinker ended: actor=%d site=%d session=%s", t.userID, t.siteID, t.id)
		t.lastInput.Store(time.Now().UnixNano())
		// Exactly one writer. A fixed queue and write deadlines bound slow consumers.
		output := make(chan []byte, 32)
		var outstanding atomic.Int64
		go func() {
			defer close(output)
			buf := make([]byte, 4096)
			for {
				n, e := p.PTY.Read(buf)
				if n > 0 {
					b := append([]byte(nil), buf[:n]...)
					select {
					case output <- b:
					case <-ctx.Done():
						return
					default:
						cancel()
						return
					}
				}
				if e != nil {
					return
				}
			}
		}()
		go func() {
			defer cancel()
			conn.SetReadLimit(16 * 1024)
			_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(60 * time.Second)) })
			window := time.Now()
			messages, bytes, inputMessages, inputBytes := 0, 0, 0, 0
			for {
				kind, data, err := conn.ReadMessage()
				if err != nil {
					return
				}
				if time.Since(window) > time.Second {
					window = time.Now()
					messages = 0
					bytes = 0
					inputMessages = 0
					inputBytes = 0
				}
				messages++
				bytes += len(data)
				if messages > 1000 || bytes > 128*1024 {
					return
				}
				if kind != websocket.TextMessage {
					return
				}
				var msg struct {
					Type  string `json:"type"`
					Data  string `json:"data"`
					Rows  uint16 `json:"rows"`
					Bytes int64  `json:"bytes"`
					Cols  uint16 `json:"cols"`
				}
				if json.Unmarshal(data, &msg) != nil {
					return
				}
				switch msg.Type {
				case "input":
					inputMessages++
					inputBytes += len(msg.Data)
					if inputMessages > 200 || inputBytes > 64*1024 {
						return
					}
					if msg.Data != "" {
						t.lastInput.Store(time.Now().UnixNano())
						if err := p.PTY.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
							return
						}
						if _, err := p.PTY.Write([]byte(msg.Data)); err != nil {
							return
						}
					}
				case "ack":
					if msg.Bytes <= 0 || msg.Bytes > 256*1024 {
						return
					}
					for {
						current := outstanding.Load()
						if msg.Bytes > current {
							return
						}
						if outstanding.CompareAndSwap(current, current-msg.Bytes) {
							break
						}
					}
				case "resize":
					if p.Resize(msg.Rows, msg.Cols) != nil {
						return
					}
				default:
					return
				}
			}
		}()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		heartbeat := time.NewTicker(20 * time.Second)
		defer heartbeat.Stop()
		reason := "Session ended"
	loop:
		for {
			select {
			case <-ctx.Done():
				reason = "Disconnected or session limit reached"
				break loop
			case b, ok := <-output:
				if !ok {
					break loop
				}
				// Do not let browser/native WebSocket buffers grow without proof
				// that xterm consumed earlier output.
				if outstanding.Add(int64(len(b))) > 256*1024 {
					reason = "Console output exceeded the render window"
					break loop
				}
				_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if conn.WriteMessage(websocket.BinaryMessage, b) != nil {
					break loop
				}
			case <-tick.C:
				if !t.valid() {
					reason = "Authentication expired or changed"
					break loop
				}
				if time.Since(time.Unix(0, t.lastInput.Load())) >= tinkerIdle {
					reason = "Disconnected after inactivity"
					break loop
				}
			case <-heartbeat.C:
				if conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)) != nil {
					break loop
				}
			}
		}
		cancel()
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1000, reason), time.Now().Add(time.Second))
	}
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// Mutations never overtake a live console. Deploy workers also acquire this
// gate, covering queued/webhook deployments after the HTTP handler returns.
func (s *Server) tinkerMutationGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" || !strings.HasPrefix(r.URL.Path, "/api/v1/sites/") || strings.Contains(r.URL.Path, "/tinker/") {
			next.ServeHTTP(w, r)
			return
		}
		part := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/sites/"), "/")[0]
		id, err := strconv.Atoi(part)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		release, ok := siteexecution.Write(id)
		if !ok {
			http.Error(w, "Disconnect Tinker before changing this site", 409)
			return
		}
		defer release()
		next.ServeHTTP(w, r)
	})
}
func (s *Server) tinkerCleanupLoop(ctx context.Context) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			s.tinker.mu.Lock()
			for _, t := range s.tinker.sessions {
				if t.cancel != nil {
					t.cancel()
				}
			}
			s.tinker.mu.Unlock()
			return
		case <-tick.C:
			s.tinker.mu.Lock()
			for id, t := range s.tinker.sessions {
				if !t.used && !time.Now().Before(t.expires) {
					delete(s.tinker.sessions, id)
				}
			}
			s.tinker.mu.Unlock()
		}
	}
}

// StopTinker closes hijacked WebSockets too; http.Server.Shutdown alone does not.
func (s *Server) StopTinker(ctx context.Context) {
	m := &s.tinker
	m.mu.Lock()
	m.stopping = true
	var done []<-chan struct{}
	for id, t := range m.sessions {
		if t.used {
			if t.cancel != nil {
				t.cancel()
			}
			done = append(done, t.done)
		} else {
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()
	for _, ch := range done {
		select {
		case <-ch:
		case <-ctx.Done():
			return
		}
	}
}
