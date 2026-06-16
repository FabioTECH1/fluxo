package bootstrap

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/services/cron"

	"golang.org/x/crypto/bcrypt"
)

// generateToken creates a 32-char random hex string for bootstrap auth.
func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// generatePassword creates a random hex string of the given length.
func generatePassword(length int) string {
	b := make([]byte, (length+1)/2)
	rand.Read(b)
	return hex.EncodeToString(b)[:length]
}

// InitAdminToken bootstraps day-zero auth: creates a sentinel user with a random token on first run.
func InitAdminToken() {
	var count int
	err := database.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		log.Fatalf("Failed to query users: %v", err)
	}

	if count == 0 {
		token := generateToken()
		hashBytes, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
		if err != nil {
			log.Fatalf("Failed to hash token: %v", err)
		}
		hashStr := string(hashBytes)

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

// InitFluxoUser creates and configures the fluxo system user (idempotent).
func InitFluxoUser() {
	if _, err := user.Lookup("fluxo"); err != nil {
		log.Println("Creating fluxo system user...")
		if out, err := exec.Command("useradd", "fluxo", "-m", "-s", "/bin/bash", "-G", "www-data").CombinedOutput(); err != nil {
			log.Printf("Warning: failed to create fluxo user: %v\n%s", err, string(out))
		} else {
			log.Println("fluxo system user created.")
		}
	}

	os.MkdirAll("/home/fluxo", 0755)
	if u, err := user.Lookup("fluxo"); err == nil {
		if uid, err := strconv.Atoi(u.Uid); err == nil {
			if gid, err := strconv.Atoi(u.Gid); err == nil {
				os.Chown("/home/fluxo", uid, gid)
			}
		}
	}
	os.MkdirAll("/home/fluxo/.ssh", 0700)

	if u, err := user.Lookup("fluxo"); err == nil {
		if uid, err := strconv.Atoi(u.Uid); err == nil {
			if gid, err := strconv.Atoi(u.Gid); err == nil {
				os.Chown("/home/fluxo/.ssh", uid, gid)
			}
		}
	}

	// Set or load the fluxo sudo password via chpasswd (no shell interpolation).
	var existingSudoPass string
	database.DB.QueryRow("SELECT fluxo_sudo_password FROM users WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)").Scan(&existingSudoPass)
	existingSudoPass = config.Decrypt(existingSudoPass)

	sudoPass := existingSudoPass
	if sudoPass == "" {
		sudoPass = generatePassword(16)
		database.DB.Exec("UPDATE users SET fluxo_sudo_password = ? WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)", config.Encrypt(sudoPass))
	}

	if _, err := user.Lookup("fluxo"); err == nil {
		cmd := exec.Command("chpasswd")
		cmd.Stdin = strings.NewReader(fmt.Sprintf("fluxo:%s\n", sudoPass))
		cmd.Run()
		exec.Command("usermod", "-aG", "sudo", "fluxo").Run()
		log.Println("Fluxo sudo password configured.")
	}

	// Set or load the fluxo MySQL password
	var existingMysqlPass string
	database.DB.QueryRow("SELECT fluxo_mysql_password FROM users WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)").Scan(&existingMysqlPass)
	existingMysqlPass = config.Decrypt(existingMysqlPass)

	mysqlPass := existingMysqlPass
	if mysqlPass == "" {
		mysqlPass = generatePassword(16)
		database.DB.Exec("UPDATE users SET fluxo_mysql_password = ? WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)", config.Encrypt(mysqlPass))
	}

	// Set or load the fluxo PostgreSQL password
	var existingPostgresPass string
	database.DB.QueryRow("SELECT fluxo_postgres_password FROM users WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)").Scan(&existingPostgresPass)
	existingPostgresPass = config.Decrypt(existingPostgresPass)

	postgresPass := existingPostgresPass
	if postgresPass == "" {
		postgresPass = generatePassword(16)
		database.DB.Exec("UPDATE users SET fluxo_postgres_password = ? WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)", config.Encrypt(postgresPass))
	}

	// Keep fluxo_db_password in sync with mysql_password for backward compat
	database.DB.Exec("UPDATE users SET fluxo_db_password = ? WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)", config.Encrypt(mysqlPass))

	// Apply/sync password to MySQL/MariaDB if installed
	if _, err := exec.LookPath("mysql"); err == nil {
		sqlCmd := fmt.Sprintf(
			"CREATE USER IF NOT EXISTS 'fluxo'@'localhost' IDENTIFIED BY '%[1]s';\n"+
				"ALTER USER 'fluxo'@'localhost' IDENTIFIED BY '%[1]s';\n"+
				"GRANT ALL PRIVILEGES ON *.* TO 'fluxo'@'localhost' WITH GRANT OPTION;\n"+
				"FLUSH PRIVILEGES;\n", mysqlPass)
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
		createCmd := exec.Command("sudo", "-u", "postgres", "psql")
		createCmd.Stdin = strings.NewReader("CREATE ROLE fluxo WITH LOGIN CREATEDB CREATEROLE;\n")
		createCmd.Run()

		alterCmd := exec.Command("sudo", "-u", "postgres", "psql")
		alterCmd.Stdin = strings.NewReader(fmt.Sprintf("ALTER ROLE fluxo WITH PASSWORD '%s';\n", postgresPass))
		if out, err := alterCmd.CombinedOutput(); err != nil {
			log.Printf("Warning: failed to sync PostgreSQL fluxo role password: %v\n%s", err, string(out))
		} else {
			log.Println("PostgreSQL fluxo role password synced successfully.")
		}
	}

	// Seed default firewall rules (actual UFW rules applied by install.sh).
	var count int
	database.DB.QueryRow("SELECT COUNT(*) FROM firewall_rules").Scan(&count)
	if count == 0 {
		defaults := []struct {
			name, port, fromIP, ruleType string
		}{
			{"SSH", "22", "Any", "allow"},
			{"HTTP", "80", "Any", "allow"},
			{"HTTPS", "443", "Any", "allow"},
			{"Fluxo Daemon", "9595", "Any", "allow"},
		}
		for _, d := range defaults {
			database.DB.Exec("INSERT INTO firewall_rules (name, port, from_ip, rule_type) VALUES (?, ?, ?, ?)", d.name, d.port, d.fromIP, d.ruleType)
		}
		log.Println("Default firewall rules seeded.")
	}

	initDefaultCrons()
}

// initDefaultCrons seeds system maintenance cron jobs (idempotent).
func initDefaultCrons() {
	type defaultCron struct {
		name, expression, command, user string
		binaryCheck                     string // skip if this binary isn't installed
	}

	defaults := []defaultCron{
		{"System Cleanup", "0 0 * * 0", "apt-get autoremove -y && apt-get autoclean", "root", ""},
		{"Renew SSL Certificates", "0 */12 * * *", "certbot renew --quiet", "root", "certbot"},
		{"Update Composer", "0 0 * * 0", "/usr/local/bin/composer self-update", "root", "composer"},
	}

	for _, d := range defaults {
		if d.binaryCheck != "" {
			if _, err := exec.LookPath(d.binaryCheck); err != nil {
				continue
			}
		}

		var count int
		database.DB.QueryRow("SELECT COUNT(*) FROM crons WHERE name = ? AND site_id = 0", d.name).Scan(&count)
		if count > 0 {
			continue
		}

		result, err := database.DB.Exec("INSERT INTO crons (site_id, name, expression, command, user) VALUES (0, ?, ?, ?, ?)", d.name, d.expression, d.command, d.user)
		if err != nil {
			log.Printf("Warning: failed to seed default cron %s: %v", d.name, err)
			continue
		}

		id, _ := result.LastInsertId()
		if err := cron.Create(int(id), "", d.expression, d.command, d.user); err != nil {
			log.Printf("Warning: failed to write cron file for %s: %v", d.name, err)
			database.DB.Exec("DELETE FROM crons WHERE id = ?", id)
		} else {
			log.Printf("Default cron seeded: %s (%s)", d.name, d.expression)
		}
	}
}

// ResetAdminToken resets the admin token and prints the new one to stdout.
func ResetAdminToken() {
	token := generateToken()
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("Failed to hash token: %v", err)
	}
	hashStr := string(hashBytes)

	var id int
	var username string
	err = database.DB.QueryRow("SELECT id, username FROM users ORDER BY id ASC LIMIT 1").Scan(&id, &username)
	if err != nil {
		// No users exist, create bootstrap user
		_, err = database.DB.Exec("INSERT INTO users (username, token_hash) VALUES (?, ?)", "__bootstrap__", hashStr)
		if err != nil {
			log.Fatalf("Failed to create bootstrap user: %v", err)
		}
		username = "__bootstrap__"
	} else {
		_, err = database.DB.Exec("UPDATE users SET token_hash = ? WHERE id = ?", hashStr, id)
		if err != nil {
			log.Fatalf("Failed to reset token: %v", err)
		}
	}

	fmt.Println("=========================================================")
	fmt.Println("ADMIN TOKEN RESET SUCCESSFUL")
	fmt.Printf("Username: %s\n", username)
	fmt.Printf("New Token: %s\n", token)
	fmt.Println("Use this token to log in. Please save it securely.")
	fmt.Println("=========================================================")
}
