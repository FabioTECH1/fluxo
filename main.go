package main

import (
	"context"
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
	"time"

	"fluxo/config"
	"fluxo/database"
	"fluxo/server"
	"fluxo/syscmd"
)

func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

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

		_, err = database.DB.Exec("INSERT INTO users (username, token_hash) VALUES (?, ?)", "admin", hashStr)
		if err != nil {
			log.Fatalf("Failed to create admin user: %v", err)
		}

		log.Println("=========================================================")
		log.Println("DAY ZERO AUTHENTICATION")
		log.Println("An initial admin user has been created.")
		log.Printf("Username: admin\n")
		log.Printf("Token:    %s\n", token)
		log.Println("Please save this token. It will only be shown once.")
		log.Println("=========================================================")
	}
}

func initFluxoUser() {
	// Check if fluxo system user exists
	if _, err := user.Lookup("fluxo"); err != nil {
		log.Println("Creating fluxo system user...")
		if out, err := exec.Command("useradd", "fluxo", "-m", "-s", "/bin/bash", "-G", "www-data").CombinedOutput(); err != nil {
			log.Printf("Warning: failed to create fluxo user: %v\n%s", err, string(out))
		} else {
			log.Println("fluxo system user created.")
		}
	}

	// Ensure .ssh directory exists with correct permissions
	os.MkdirAll("/home/fluxo/.ssh", 0700)

	// Create MySQL fluxo user if not exists
	var existingPass string
	database.DB.QueryRow("SELECT fluxo_db_password FROM users WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)").Scan(&existingPass)

	pass := existingPass
	if pass == "" {
		pass = fmt.Sprintf("%x", time.Now().UnixNano())[:16]
		database.DB.Exec("UPDATE users SET fluxo_db_password = ? WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)", pass)
	}

	ctx := context.Background()
	if _, err := syscmd.Run(ctx, 10*time.Second, "mysql", "-e", fmt.Sprintf("CREATE USER IF NOT EXISTS 'fluxo'@'localhost' IDENTIFIED BY '%s'", pass)); err == nil {
		syscmd.Run(ctx, 5*time.Second, "mysql", "-e", fmt.Sprintf("GRANT ALL PRIVILEGES ON *.* TO 'fluxo'@'localhost' WITH GRANT OPTION"))
		syscmd.Run(ctx, 5*time.Second, "mysql", "-e", "FLUSH PRIVILEGES")
		log.Printf("MySQL fluxo user password: %s", pass)
	}

	// Set sudo password for fluxo user
	var existingSudoPass string
	database.DB.QueryRow("SELECT fluxo_sudo_password FROM users WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)").Scan(&existingSudoPass)

	sudoPass := existingSudoPass
	if sudoPass == "" {
		sudoPass = fmt.Sprintf("%x", time.Now().UnixNano())[:16]
		database.DB.Exec("UPDATE users SET fluxo_sudo_password = ? WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)", sudoPass)
	}

	// Ensure fluxo user has the password set on the system and is in sudo group
	if _, err := user.Lookup("fluxo"); err == nil {
		exec.Command("bash", "-c", fmt.Sprintf("echo 'fluxo:%s' | chpasswd", sudoPass)).Run()
		exec.Command("usermod", "-aG", "sudo", "fluxo").Run()
		log.Printf("Fluxo sudo password: %s", sudoPass)
	}

	// Seed default firewall rules if table is empty
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
