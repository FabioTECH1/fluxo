package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"os/user"
	"strings"

	"fluxo/config"
	"fluxo/database"
	"fluxo/server"
)

// generateToken creates a cryptographically random hex string suitable
// for the admin bootstrap token. 16 bytes → 32 hex characters.
func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// generatePassword creates a cryptographically random hex string of
// the given length. Used for system passwords (sudo, database).
func generatePassword(length int) string {
	b := make([]byte, (length+1)/2)
	rand.Read(b)
	return hex.EncodeToString(b)[:length]
}

// initAdminToken is the day-zero authentication bootstrap.
// On first run (users table empty) it creates a sentinel row with
// username "__bootstrap__" and a random token. The token is printed
// to stdout exactly once. The user claims the account by logging in
// with any desired username + this token as the password.
func initAdminToken() {
	var count int
	err := database.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		log.Fatalf("Failed to query users: %v", err)
	}

	if count == 0 {
		token := generateToken()
		hash := sha256.Sum256([]byte(token))
		hashStr := hex.EncodeToString(hash[:])

		_, err = database.DB.Exec("INSERT INTO users (username, token_hash) VALUES (?, ?)", "__bootstrap__", hashStr)
		if err != nil {
			log.Fatalf("Failed to create bootstrap user: %v", err)
		}

		log.Println("=========================================================")
		log.Println("DAY ZERO AUTHENTICATION")
		log.Println("Use this token with any username at first login.")
		log.Printf("Token:    %s\n", token)
		log.Println("Please save this token. It will only be shown once.")
		log.Println("=========================================================")
	}
}

// initFluxoUser bootstraps the fluxo system user and its credentials.
// It runs on every startup but is idempotent — each step skips if
// the user or passwords already exist. The fluxo user is the single
// system account used for SSH access, daemon execution, cron jobs,
// and privileged operations via sudo.
func initFluxoUser() {
	if _, err := user.Lookup("fluxo"); err != nil {
		log.Println("Creating fluxo system user...")
		if out, err := exec.Command("useradd", "fluxo", "-m", "-s", "/bin/bash", "-G", "www-data").CombinedOutput(); err != nil {
			log.Printf("Warning: failed to create fluxo user: %v\n%s", err, string(out))
		} else {
			log.Println("fluxo system user created.")
		}
	}

	os.MkdirAll("/home/fluxo/.ssh", 0700)

	// Set or load the fluxo sudo password. Uses crypto/rand for generation.
	// The password is stored in the SQLite users table and applied to the
	// system via chpasswd. The password is piped through stdin to avoid
	// leaking it in /proc/[pid]/cmdline (no shell interpolation).
	var existingSudoPass string
	database.DB.QueryRow("SELECT fluxo_sudo_password FROM users WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)").Scan(&existingSudoPass)

	sudoPass := existingSudoPass
	if sudoPass == "" {
		sudoPass = generatePassword(16)
		database.DB.Exec("UPDATE users SET fluxo_sudo_password = ? WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)", sudoPass)
	}

	if _, err := user.Lookup("fluxo"); err == nil {
		cmd := exec.Command("chpasswd")
		cmd.Stdin = strings.NewReader(fmt.Sprintf("fluxo:%s\n", sudoPass))
		cmd.Run()
		exec.Command("usermod", "-aG", "sudo", "fluxo").Run()
		log.Printf("Fluxo sudo password: %s", sudoPass)
	}

	// Set or load the fluxo database password. Uses crypto/rand for generation.
	var existingDbPass string
	database.DB.QueryRow("SELECT fluxo_db_password FROM users WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)").Scan(&existingDbPass)

	dbPass := existingDbPass
	if dbPass == "" {
		dbPass = generatePassword(16)
		database.DB.Exec("UPDATE users SET fluxo_db_password = ? WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)", dbPass)
	}

	// Apply/sync password to MySQL/MariaDB if installed
	if _, err := exec.LookPath("mysql"); err == nil {
		sqlCmd := fmt.Sprintf(
			"CREATE USER IF NOT EXISTS 'fluxo'@'localhost' IDENTIFIED BY '%[1]s';\n"+
				"ALTER USER 'fluxo'@'localhost' IDENTIFIED BY '%[1]s';\n"+
				"GRANT ALL PRIVILEGES ON *.* TO 'fluxo'@'localhost' WITH GRANT OPTION;\n"+
				"FLUSH PRIVILEGES;\n", dbPass)
		cmd := exec.Command("mysql")
		cmd.Stdin = strings.NewReader(sqlCmd)
		out, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("Warning: failed to sync MySQL fluxo user: %v\n%s", err, string(out))
		} else {
			log.Println("MySQL fluxo user and password synced successfully.")
		}
	}

	// Apply/sync password to PostgreSQL if installed
	if _, err := exec.LookPath("psql"); err == nil {
		// Try to create the role first. If it exists, it will error but we'll alter it anyway.
		createCmd := exec.Command("sudo", "-u", "postgres", "psql")
		createCmd.Stdin = strings.NewReader("CREATE ROLE fluxo WITH LOGIN CREATEDB CREATEROLE;\n")
		createCmd.Run()

		alterCmd := exec.Command("sudo", "-u", "postgres", "psql")
		alterCmd.Stdin = strings.NewReader(fmt.Sprintf("ALTER ROLE fluxo WITH PASSWORD '%s';\n", dbPass))
		if out, err := alterCmd.CombinedOutput(); err != nil {
			log.Printf("Warning: failed to sync PostgreSQL fluxo role password: %v\n%s", err, string(out))
		} else {
			log.Println("PostgreSQL fluxo role password synced successfully.")
		}
	}

	// Seed default firewall rules in the SQLite tracking table.
	// The actual UFW rules are applied by install.sh at provisioning time.
	var count int
	database.DB.QueryRow("SELECT COUNT(*) FROM firewall_rules").Scan(&count)
	if count == 0 {
		defaults := []struct {
			name, port, fromIP, ruleType string
		}{
			{"SSH", "22", "Any", "allow"},
			{"HTTP", "80", "Any", "allow"},
			{"HTTPS", "443", "Any", "allow"},
			{"Fluxo Daemon", "8080", "Any", "allow"},
		}
		for _, d := range defaults {
			database.DB.Exec("INSERT INTO firewall_rules (name, port, from_ip, rule_type) VALUES (?, ?, ?, ?)", d.name, d.port, d.fromIP, d.ruleType)
		}
		log.Println("Default firewall rules seeded.")
	}
}

// main is the entrypoint for the Fluxo daemon. Startup sequence:
// 1. Load config from environment variables
// 2. Initialize SQLite database (schema + migrations)
// 3. Bootstrap admin credentials (day-zero auth)
// 4. Bootstrap the fluxo system user
// 5. Start pprof debug server on localhost:6060 (background)
// 6. Start the HTTP server (foreground, blocks)
func main() {
	log.Println("Starting Fluxo daemon...")

	cfg := config.LoadConfig()

	err := database.InitDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("Database initialization failed: %v", err)
	}
	log.Println("Database initialized successfully.")

	initAdminToken()
	initFluxoUser()

	// pprof debugging server bound to loopback only — not exposed externally.
	go func() {
		log.Println("Starting pprof server on 127.0.0.1:6060")
		if err := http.ListenAndServe("127.0.0.1:6060", nil); err != nil {
			log.Printf("pprof failed: %v\n", err)
		}
	}()

	srv := server.NewServer()

	port := ":" + cfg.Port
	log.Printf("Listening on %s\n", port)
	if err := http.ListenAndServe(port, srv); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
