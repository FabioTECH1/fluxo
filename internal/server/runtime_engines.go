package server

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"fluxo/internal/syscmd"
)

func (s *Server) handleGetMySQLInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := map[string]interface{}{}

		if out, err := exec.LookPath("mysql"); err == nil {
			info["binary"] = out
		} else {
			info["binary"] = ""
		}

		if out, err := syscmd.Run(r.Context(), 5*time.Second, "mysql", "--version"); err == nil {
			raw := strings.TrimSpace(out)
			if idx := strings.Index(raw, "Distrib "); idx != -1 {
				rest := raw[idx+len("Distrib "):]
				if comma := strings.Index(rest, ","); comma != -1 {
					rest = rest[:comma]
				}
				info["version"] = rest
			} else {
				info["version"] = raw
			}
		} else {
			info["version"] = ""
		}

		info["socket"] = "/var/run/mysqld/mysqld.sock"

		if _, err := os.Stat("/var/run/mysqld/mysqld.sock"); err == nil {
			info["status"] = "running"
		} else {
			info["status"] = "stopped"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	}
}

func (s *Server) handleGetRedisInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := map[string]interface{}{}

		if out, err := exec.LookPath("redis-server"); err == nil {
			info["binary"] = out
		} else {
			info["binary"] = ""
		}

		if out, err := syscmd.Run(r.Context(), 5*time.Second, "redis-server", "--version"); err == nil {
			raw := strings.TrimSpace(out)
			if idx := strings.Index(raw, "v="); idx != -1 {
				rest := raw[idx+len("v="):]
				if space := strings.Index(rest, " "); space != -1 {
					rest = rest[:space]
				}
				info["version"] = rest
			} else {
				info["version"] = raw
			}
		} else {
			info["version"] = ""
		}

		running := false
		if conn, err := net.DialTimeout("tcp", "127.0.0.1:6379", 1*time.Second); err == nil {
			conn.Close()
			running = true
		} else if _, err := os.Stat("/run/redis/redis-server.sock"); err == nil {
			running = true
		}

		if running {
			info["status"] = "running"
		} else {
			info["status"] = "stopped"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	}
}

func (s *Server) handleGetPostgresInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := map[string]interface{}{}

		if out, err := exec.LookPath("psql"); err == nil {
			info["binary"] = out
		} else {
			info["binary"] = ""
		}

		if out, err := syscmd.Run(r.Context(), 5*time.Second, "psql", "--version"); err == nil {
			raw := strings.TrimSpace(out)
			if idx := strings.Index(raw, "psql (PostgreSQL) "); idx != -1 {
				info["version"] = raw[idx+len("psql (PostgreSQL) "):]
			} else {
				info["version"] = raw
			}
		} else {
			info["version"] = ""
		}

		running := false
		if conn, err := net.DialTimeout("tcp", "127.0.0.1:5432", 1*time.Second); err == nil {
			conn.Close()
			running = true
		} else if _, err := os.Stat("/var/run/postgresql/.s.PGSQL.5432"); err == nil {
			running = true
		}

		if running {
			info["status"] = "running"
		} else {
			info["status"] = "stopped"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	}
}
