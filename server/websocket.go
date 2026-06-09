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
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("WS Upgrade error:", err)
			return
		}

		siteID := 0
		if idStr := r.URL.Query().Get("site_id"); idStr != "" {
			if id, err := strconv.Atoi(idStr); err == nil {
				siteID = id
			}
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
