package server

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for MVP
	},
}

// WSClient represents a connected WebSocket client
type WSClient struct {
	conn   *websocket.Conn
	siteID int
}

// Hub manages WebSocket clients and broadcasts logs
type Hub struct {
	clients map[*WSClient]bool
	mu      sync.RWMutex
}

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

// BroadcastLog implements deploy.LogBroadcaster
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

func (s *Server) handleWebSocket() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("WS Upgrade error:", err)
			return
		}

		// Client should send siteID as first message or query param. For simplicity, we just broadcast all for now if siteID is 0, but let's parse from query:
		// e.g. /api/v1/ws?site_id=1
		siteID := 0
		// You would parse site_id from r.URL.Query() here in a real app

		client := &WSClient{conn: conn, siteID: siteID}
		GlobalHub.AddClient(client)

		// Read loop to keep connection alive and handle disconnects
		go func() {
			defer GlobalHub.RemoveClient(client)
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			conn.SetPongHandler(func(string) error { conn.SetReadDeadline(time.Now().Add(60 * time.Second)); return nil })
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					break
				}
			}
		}()

		// Write loop for Pings
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
