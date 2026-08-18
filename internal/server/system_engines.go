package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/services/mysql"
	"fluxo/internal/services/postgres"
	"fluxo/internal/syscmd"
)

// handleGetEngines returns a list of installed database engines.
func (s *Server) handleGetEngines() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		engines := []string{}

		if _, err := exec.LookPath("mysql"); err == nil {
			engines = append(engines, "mysql")
		}

		if _, err := exec.LookPath("psql"); err == nil {
			engines = append(engines, "postgres")
		}

		if _, err := exec.LookPath("redis-server"); err == nil {
			engines = append(engines, "redis")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(engines)
	}
}

// syncDatabaseCredentials ensures the fluxo admin user exists with stored passwords.
func syncDatabaseCredentials(engine string) error {
	time.Sleep(5 * time.Second)
	switch engine {
	case "mysql":
		var mysqlPass string
		if err := database.DB.QueryRow("SELECT fluxo_mysql_password FROM users ORDER BY id ASC LIMIT 1").Scan(&mysqlPass); err != nil {
			return err
		}
		mysqlPass = config.Decrypt(mysqlPass)
		if mysqlPass == "" {
			return fmt.Errorf("MySQL administrator password is empty")
		}
		return mysql.SyncAdminUser(mysqlPass)
	case "postgres":
		var postgresPass string
		if err := database.DB.QueryRow("SELECT fluxo_postgres_password FROM users ORDER BY id ASC LIMIT 1").Scan(&postgresPass); err != nil {
			return err
		}
		postgresPass = config.Decrypt(postgresPass)
		if postgresPass == "" {
			return fmt.Errorf("PostgreSQL administrator password is empty")
		}
		if err := postgres.SyncAdminRole(postgresPass); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported database engine %q", engine)
	}
	return nil
}

func markEngineCredentialsPending(engine string) error {
	_, err := database.DB.Exec(`UPDATE users SET
		pending_new_password_engine = CASE
			WHEN pending_new_password_engine = '' THEN ?
			WHEN instr(',' || pending_new_password_engine || ',', ',' || ? || ',') = 0
				THEN pending_new_password_engine || ',' || ?
			ELSE pending_new_password_engine
		END,
		credentials_generation = credentials_generation + 1,
		credentials_download_generation = -1
		WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)`, engine, engine, engine)
	return err
}

// handleInstallMySQL installs MariaDB server asynchronously.
func (s *Server) handleInstallMySQL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := exec.LookPath("mysql"); err == nil {
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusAccepted)

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()

			syscmd.Run(ctx, 10*time.Minute, "apt-get", "update")
			_, err := syscmd.Run(ctx, 10*time.Minute, "apt-get", "install", "-y", "mariadb-server")
			if err == nil {
				if err := syncDatabaseCredentials("mysql"); err != nil {
					log.Printf("MySQL installed but credential sync failed: %v", err)
				} else if err := markEngineCredentialsPending("mysql"); err != nil {
					log.Printf("MySQL installed but one-time credentials could not be queued: %v", err)
				}
			}
		}()
	}
}

// handleInstallPostgres installs PostgreSQL server asynchronously.
func (s *Server) handleInstallPostgres() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := exec.LookPath("psql"); err == nil {
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusAccepted)

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()

			syscmd.Run(ctx, 10*time.Minute, "apt-get", "update")
			_, err := syscmd.Run(ctx, 10*time.Minute, "apt-get", "install", "-y", "postgresql")
			if err == nil {
				if err := syncDatabaseCredentials("postgres"); err != nil {
					log.Printf("PostgreSQL installed but credential sync failed: %v", err)
				} else if err := markEngineCredentialsPending("postgres"); err != nil {
					log.Printf("PostgreSQL installed but one-time credentials could not be queued: %v", err)
				}
			}
		}()
	}
}

// handleInstallRedis installs Redis server asynchronously.
func (s *Server) handleInstallRedis() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := exec.LookPath("redis-server"); err == nil {
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusAccepted)

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()

			syscmd.Run(ctx, 10*time.Minute, "apt-get", "update")
			syscmd.Run(ctx, 10*time.Minute, "apt-get", "install", "-y", "redis-server")
		}()
	}
}
