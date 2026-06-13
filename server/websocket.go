// WebSocket hub for real-time deploy log streaming. Deploy scripts run in
// the background and stream output via the hub to WebSocket clients filtered
// by site_id. Clients connect with GET /api/v1/ws?site_id=N and receive
// only logs for that site.
package server

import (
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"fluxo/database"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Accept connections from any origin for MVP simplicity.
	},
}

// WSClient represents a connected WebSocket client subscribed to a
// specific site's deploy logs.
type WSClient struct {
	conn   *websocket.Conn
	siteID int
}

// Hub manages all connected WebSocket clients and dispatches log
// messages to clients subscribed to the matching site_id.
type Hub struct {
	clients map[*WSClient]bool
	mu      sync.RWMutex
}

// GlobalHub is the singleton WebSocket hub used across the application.
// Deploy scripts call BroadcastLog to push output lines to connected
// UI clients.
var GlobalHub = &Hub{
	clients: make(map[*WSClient]bool),
}

func (h *Hub) AddClient(client *WSClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[client] = true
}

func (h *Hub) RemoveClient(client *WSClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, client)
	client.conn.Close()
}

// BroadcastLog implements deploy.LogBroadcaster. It sends a log line to
// every client subscribed to the given siteID. Clients that fail to receive
// are removed from the hub.
func (h *Hub) BroadcastLog(siteID int, message string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		if client.siteID == siteID {
			err := client.conn.WriteMessage(websocket.TextMessage, []byte(message))
			if err != nil {
				client.conn.Close()
				delete(h.clients, client)
			}
		}
	}
}

// handleWebSocket upgrades an HTTP connection to a WebSocket and subscribes
// the client to deploy logs for a specific site. The site_id is read from
// the query string: GET /api/v1/ws?site_id=N
//
// Connection lifecycle:
//   - Read loop with 60s deadline detects disconnects and cleans up the client.
//   - Write loop sends pings every 54s to keep idle connections alive.
func (s *Server) handleWebSocket() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID := 0
		if idStr := r.URL.Query().Get("site_id"); idStr != "" {
			if id, err := strconv.Atoi(idStr); err == nil {
				siteID = id
			}
		}

		tokenString := r.URL.Query().Get("token")
		if tokenString == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		parser := jwt.NewParser()
		unverified, _, err := parser.ParseUnverified(tokenString, jwt.MapClaims{})
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		unverifiedClaims, ok := unverified.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		username, ok := unverifiedClaims["sub"].(string)
		if !ok || username == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		var tokenHash string
		err = database.DB.QueryRow("SELECT token_hash FROM users WHERE username = ?", username).Scan(&tokenHash)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, http.ErrAbortHandler
			}
			return []byte(tokenHash), nil
		})

		if err != nil || !token.Valid {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("WS Upgrade error:", err)
			return
		}

		client := &WSClient{conn: conn, siteID: siteID}
		GlobalHub.AddClient(client)

		// Read loop: detect disconnects and clean up.
		go func() {
			defer GlobalHub.RemoveClient(client)
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			conn.SetPongHandler(func(string) error {
				conn.SetReadDeadline(time.Now().Add(60 * time.Second))
				return nil
			})
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					break
				}
			}
		}()

		// Ping loop: keep idle connections alive.
		go func() {
			ticker := time.NewTicker(54 * time.Second)
			defer func() {
				ticker.Stop()
				conn.Close()
			}()
			for {
				select {
				case <-ticker.C:
					conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
					if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
						return
					}
				}
			}
		}()
	}
}
