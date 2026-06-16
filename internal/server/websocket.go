// WebSocket hub for real-time deploy log streaming.
package server

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"fluxo/internal/database"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		host := r.Host
		if colonIdx := strings.LastIndex(host, ":"); colonIdx != -1 {
			host = host[:colonIdx]
		}
		return strings.Contains(origin, host)
	},
}

// WSClient represents a connected WebSocket client subscribed to a site's logs.
type WSClient struct {
	conn   *websocket.Conn
	siteID int
}

// Hub manages connected WebSocket clients and dispatches log messages by site ID.
type Hub struct {
	clients map[*WSClient]bool
	mu      sync.RWMutex
}

// GlobalHub is the singleton WebSocket hub used across the application.
var GlobalHub = &Hub{
	clients: make(map[*WSClient]bool),
}

// AddClient registers a client with the hub.
func (h *Hub) AddClient(client *WSClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[client] = true
}

// RemoveClient unregisters a client and closes its connection.
func (h *Hub) RemoveClient(client *WSClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, client)
	client.conn.Close()
}

// BroadcastLog sends a log line to all clients subscribed to the given site ID.
func (h *Hub) BroadcastLog(siteID int, message string) {
	h.mu.Lock()
	defer h.mu.Unlock()

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

// handleWebSocket upgrades HTTP to WebSocket and subscribes to deploy logs for a site.
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
		var tokenVersion int
		err = database.DB.QueryRow("SELECT token_hash, token_version FROM users WHERE username = ?", username).Scan(&tokenHash, &tokenVersion)
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

		if claims, ok := token.Claims.(jwt.MapClaims); ok {
			if ver, ok := claims["ver"].(float64); ok {
				if int(ver) != tokenVersion {
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}
			}
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("WS Upgrade error:", err)
			return
		}

		client := &WSClient{conn: conn, siteID: siteID}
		GlobalHub.AddClient(client)

		// Read loop: detect disconnects and clean up
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

		// Ping loop: keep idle connections alive
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
