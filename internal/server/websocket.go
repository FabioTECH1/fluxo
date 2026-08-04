// WebSocket hub for real-time deploy and command log streaming.
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
	conn         *websocket.Conn
	siteID       int
	deploymentID int64
	commandID    int64
	replay       bool
	writeMu      sync.Mutex
}

// Hub manages connected WebSocket clients and dispatches log messages by site ID.
type Hub struct {
	clients         map[*WSClient]bool
	logs            map[int64][]string
	logBytes        map[int64]int
	commandLogs     map[int64][]string
	commandLogBytes map[int64]int
	mu              sync.RWMutex
}

// GlobalHub is the singleton WebSocket hub used across the application.
var GlobalHub = &Hub{
	clients:         make(map[*WSClient]bool),
	logs:            make(map[int64][]string),
	logBytes:        make(map[int64]int),
	commandLogs:     make(map[int64][]string),
	commandLogBytes: make(map[int64]int),
}

const maxBufferedDeployLogBytes = 256 * 1024

// AddClient registers a client with the hub.
func (h *Hub) AddClient(client *WSClient) bool {
	h.mu.Lock()
	replay := []string(nil)
	if client.replay && client.commandID > 0 {
		replay = append(replay, h.commandLogs[client.commandID]...)
	} else if client.replay && client.deploymentID > 0 {
		replay = append(replay, h.logs[client.deploymentID]...)
	}
	h.clients[client] = true
	client.writeMu.Lock()
	h.mu.Unlock()

	if len(replay) > 0 {
		for _, message := range replay {
			client.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := client.conn.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
				client.writeMu.Unlock()
				h.RemoveClient(client)
				return false
			}
		}
	}
	client.writeMu.Unlock()
	return true
}

// RemoveClient unregisters a client and closes its connection.
func (h *Hub) RemoveClient(client *WSClient) {
	h.mu.Lock()
	delete(h.clients, client)
	h.mu.Unlock()
	client.close()
}

// BroadcastLog sends a log line to clients subscribed to the given site or deployment.
func (h *Hub) BroadcastLog(siteID int, deploymentID int64, message string) {
	h.mu.Lock()
	if deploymentID > 0 {
		h.appendLogLocked(deploymentID, message)
	}
	clients := make([]*WSClient, 0)
	for client := range h.clients {
		if client.commandID == 0 && client.siteID == siteID && (client.deploymentID == 0 || client.deploymentID == deploymentID) {
			clients = append(clients, client)
		}
	}
	h.mu.Unlock()

	for _, client := range clients {
		if err := client.writeMessage(websocket.TextMessage, []byte(message)); err != nil {
			h.RemoveClient(client)
		}
	}
}

// BroadcastCommandLog sends command output to clients subscribed to the given command.
func (h *Hub) BroadcastCommandLog(siteID int, commandID int64, message string) {
	if commandID <= 0 {
		return
	}
	h.mu.Lock()
	h.appendCommandLogLocked(commandID, message)
	clients := make([]*WSClient, 0)
	for client := range h.clients {
		if client.siteID == siteID && client.commandID == commandID {
			clients = append(clients, client)
		}
	}
	h.mu.Unlock()

	for _, client := range clients {
		if err := client.writeMessage(websocket.TextMessage, []byte(message)); err != nil {
			h.RemoveClient(client)
		}
	}
}

// ClearLog drops buffered replay logs for a deployment before it starts.
func (h *Hub) ClearLog(siteID int, deploymentID int64) {
	if deploymentID <= 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.logs, deploymentID)
	delete(h.logBytes, deploymentID)
}

// ClearCommandLog drops buffered replay logs for a command before it starts.
func (h *Hub) ClearCommandLog(siteID int, commandID int64) {
	if commandID <= 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.commandLogs, commandID)
	delete(h.commandLogBytes, commandID)
}

func (h *Hub) appendLogLocked(deploymentID int64, message string) {
	h.logs[deploymentID] = append(h.logs[deploymentID], message)
	h.logBytes[deploymentID] += len(message)
	for h.logBytes[deploymentID] > maxBufferedDeployLogBytes && len(h.logs[deploymentID]) > 0 {
		h.logBytes[deploymentID] -= len(h.logs[deploymentID][0])
		h.logs[deploymentID] = h.logs[deploymentID][1:]
	}
}

func (h *Hub) appendCommandLogLocked(commandID int64, message string) {
	h.commandLogs[commandID] = append(h.commandLogs[commandID], message)
	h.commandLogBytes[commandID] += len(message)
	for h.commandLogBytes[commandID] > maxBufferedDeployLogBytes && len(h.commandLogs[commandID]) > 0 {
		h.commandLogBytes[commandID] -= len(h.commandLogs[commandID][0])
		h.commandLogs[commandID] = h.commandLogs[commandID][1:]
	}
}

func (c *WSClient) writeMessage(messageType int, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.conn.WriteMessage(messageType, data)
}

func (c *WSClient) close() {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.conn.Close()
}

// handleWebSocket upgrades HTTP to WebSocket and subscribes to deploy or command logs for a site.
func (s *Server) handleWebSocket() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		siteID, err := strconv.Atoi(r.URL.Query().Get("site_id"))
		if err != nil || siteID <= 0 {
			http.Error(w, "Invalid site ID", http.StatusBadRequest)
			return
		}
		var deploymentID int64
		if idStr := r.URL.Query().Get("deployment_id"); idStr != "" {
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil || id <= 0 {
				http.Error(w, "Invalid deployment ID", http.StatusBadRequest)
				return
			}
			deploymentID = id
		}
		var commandID int64
		if idStr := r.URL.Query().Get("command_id"); idStr != "" {
			id, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil || id <= 0 {
				http.Error(w, "Invalid command ID", http.StatusBadRequest)
				return
			}
			commandID = id
		}
		if deploymentID > 0 && commandID > 0 {
			http.Error(w, "Choose deployment or command stream", http.StatusBadRequest)
			return
		}
		replay := r.URL.Query().Get("replay") == "1"

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
		if !ok || username == "" || username == "__bootstrap__" {
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

		var siteExists int
		if err := database.DB.QueryRow("SELECT 1 FROM sites WHERE id = ?", siteID).Scan(&siteExists); err != nil {
			http.Error(w, "Site not found", http.StatusNotFound)
			return
		}
		if deploymentID > 0 {
			var exists int
			if err := database.DB.QueryRow("SELECT 1 FROM deployments WHERE id = ? AND site_id = ?", deploymentID, siteID).Scan(&exists); err != nil {
				http.Error(w, "Deployment not found", http.StatusNotFound)
				return
			}
		}
		if commandID > 0 {
			var exists int
			if err := database.DB.QueryRow("SELECT 1 FROM commands WHERE id = ? AND site_id = ?", commandID, siteID).Scan(&exists); err != nil {
				http.Error(w, "Command not found", http.StatusNotFound)
				return
			}
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("WS Upgrade error:", err)
			return
		}

		client := &WSClient{conn: conn, siteID: siteID, deploymentID: deploymentID, commandID: commandID, replay: replay}
		if !GlobalHub.AddClient(client) {
			return
		}

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
			}()
			for {
				select {
				case <-ticker.C:
					if err := client.writeMessage(websocket.PingMessage, nil); err != nil {
						GlobalHub.RemoveClient(client)
						return
					}
				}
			}
		}()
	}
}
