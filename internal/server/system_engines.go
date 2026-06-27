package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/database"
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
func syncDatabaseCredentials() {
	var mysqlPass, postgresPass string
	database.DB.QueryRow("SELECT fluxo_mysql_password, fluxo_postgres_password FROM users ORDER BY id ASC LIMIT 1").Scan(&mysqlPass, &postgresPass)

	time.Sleep(5 * time.Second)

	// Sync MySQL
	if mysqlPass != "" {
		mysqlPass = config.Decrypt(mysqlPass)
		if _, err := exec.LookPath("mysql"); err == nil {
			sqlCmd := fmt.Sprintf(
				"CREATE USER IF NOT EXISTS 'fluxo'@'localhost' IDENTIFIED BY '%[1]s';\n"+
					"ALTER USER 'fluxo'@'localhost' IDENTIFIED BY '%[1]s';\n"+
					"GRANT ALL PRIVILEGES ON *.* TO 'fluxo'@'localhost' WITH GRANT OPTION;\n"+
					"FLUSH PRIVILEGES;\n", mysqlPass)
			cmd := exec.Command("mysql")
			cmd.Stdin = strings.NewReader(sqlCmd)
			cmd.Run()
		}
	}

	// Sync PostgreSQL
	if postgresPass != "" {
		postgresPass = config.Decrypt(postgresPass)
		if _, err := exec.LookPath("psql"); err == nil {
			createCmd := exec.Command("sudo", "-u", "postgres", "psql")
			createCmd.Stdin = strings.NewReader("CREATE ROLE fluxo WITH LOGIN CREATEDB CREATEROLE;\n")
			createCmd.Run()

			alterCmd := exec.Command("sudo", "-u", "postgres", "psql")
			alterCmd.Stdin = strings.NewReader(fmt.Sprintf("ALTER ROLE fluxo WITH SUPERUSER PASSWORD '%s';\n", postgresPass))
			alterCmd.Run()
		}
	}
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
				syncDatabaseCredentials()
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
				syncDatabaseCredentials()
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
