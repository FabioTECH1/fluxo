package bootstrap

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"fluxo/internal/config"
	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	"fluxo/internal/services/cron"
	"fluxo/internal/services/postgres"
	"fluxo/internal/syscmd"

	"golang.org/x/crypto/bcrypt"
)

// generateToken creates a 32-char random hex string for bootstrap auth.
func generateToken() string {
	token, err := safeinput.GenerateSecretHex(16)
	if err != nil {
		panic(err)
	}
	return token
}

// generatePassword creates a random hex string of the given length.
func generatePassword(length int) string {
	b, err := safeinput.GenerateSecretHex((length + 1) / 2)
	if err != nil {
		panic(err)
	}
	return b[:length]
}

func CredentialsPath(dataDir string) string {
	return filepath.Join(dataDir, ".fluxo_credentials")
}

func validCredentialValue(value string) bool {
	if len(value) != 16 && len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func readCredentialsFile(path string) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("credentials path is not a regular file")
	}
	if info.Size() > 64*1024 {
		return nil, fmt.Errorf("credentials file exceeds the 64 KiB safety limit")
	}
	return io.ReadAll(io.LimitReader(f, 64*1024))
}

func readCredentialsTail(path string) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("credentials path is not a regular file")
	}
	offset := info.Size() - 64*1024
	if offset < 0 {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(f, 64*1024))
}

func ReadCredential(dataDir, label string) string {
	contents, err := readCredentialsFile(CredentialsPath(dataDir))
	if err != nil {
		return ""
	}
	prefix := label + ":"
	lines := strings.Split(string(contents), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if strings.HasPrefix(line, prefix) {
			value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			if validCredentialValue(value) {
				return value
			}
		}
	}
	return ""
}

func ReadBootstrapToken(dataDir string) string {
	contents, err := readCredentialsFile(CredentialsPath(dataDir))
	if err != nil {
		return ""
	}
	lines := strings.Split(string(contents), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if !strings.HasPrefix(lines[i], "Fluxo bootstrap token") {
			continue
		}
		_, value, ok := strings.Cut(lines[i], ":")
		value = strings.TrimSpace(value)
		if ok && len(value) == 32 && validCredentialValue(value) {
			return value
		}
	}
	return ""
}

func rewriteCredentialsFile(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".fluxo-credentials-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(contents); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func prepareCredentialsFile(dataDir string, migrateLegacy bool) (string, error) {
	path := CredentialsPath(dataDir)
	var current []byte
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return path, fmt.Errorf("credentials path is not a regular file")
		}
		if info.Size() > 64*1024 {
			return path, fmt.Errorf("credentials file exceeds the 64 KiB safety limit")
		}
		current, err = readCredentialsFile(path)
		if err != nil {
			return path, err
		}
	} else if !os.IsNotExist(err) {
		return path, err
	}

	if !migrateLegacy {
		return path, nil
	}

	const legacyPath = "/home/fluxo/.fluxo_credentials"
	if filepath.Clean(path) == legacyPath {
		return path, nil
	}
	legacyInfo, err := os.Lstat(legacyPath)
	if os.IsNotExist(err) {
		return path, nil
	}
	if err != nil {
		return path, err
	}
	if legacyInfo.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(legacyPath); err != nil {
			return path, fmt.Errorf("remove rejected legacy credentials symlink: %w", err)
		}
		log.Printf("Removed rejected legacy credentials symlink %s", legacyPath)
		return path, nil
	}
	if !legacyInfo.Mode().IsRegular() {
		return path, fmt.Errorf("legacy credentials path is not a regular file")
	}
	if legacyInfo.Size() > 64*1024 {
		return path, fmt.Errorf("legacy credentials file exceeds the 64 KiB safety limit")
	}
	legacy, err := readCredentialsFile(legacyPath)
	if err != nil {
		return path, err
	}
	merged := legacy
	if len(current) > 0 {
		if len(legacy)+1+len(current) > 64*1024 {
			return path, fmt.Errorf("combined credentials exceed the 64 KiB safety limit")
		}
		merged = append(append(append([]byte{}, legacy...), '\n'), current...)
	}
	if err := rewriteCredentialsFile(path, merged); err != nil {
		return path, fmt.Errorf("migrate legacy credentials: %w", err)
	}
	if err := removeCredentialFile(legacyPath); err != nil {
		return path, fmt.Errorf("remove migrated legacy credentials: %w", err)
	}
	log.Printf("Migrated legacy credentials from %s to %s", legacyPath, path)
	return path, nil
}

func appendCredential(dataDir string, migrateLegacy bool, label, value string) error {
	if !validCredentialValue(value) {
		return fmt.Errorf("invalid credential value")
	}
	return appendCredentialLine(dataDir, migrateLegacy, label, value)
}

func appendCredentialLine(dataDir string, migrateLegacy bool, label, value string) error {
	path, err := prepareCredentialsFile(dataDir, migrateLegacy)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("credentials path is not a regular file")
	}
	fd, err := syscall.Open(path, syscall.O_APPEND|syscall.O_CREAT|syscall.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "\n%s: %s\n", label, value); err != nil {
		return err
	}
	return f.Sync()
}

func writeAccountRecoveryCredentials(dataDir string, migrateLegacy bool, username, token string) error {
	if !validCredentialValue(token) {
		return fmt.Errorf("invalid reset token")
	}
	claimedUsername := username != "" && username != "__bootstrap__"
	persistUsername := claimedUsername && safeinput.ValidateAdminUsername(username)

	path, err := prepareCredentialsFile(dataDir, migrateLegacy)
	if err != nil {
		return err
	}
	current, err := readCredentialsFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	lines := strings.Split(string(current), "\n")
	kept := make([]string, 0, len(lines)+2)
	for _, line := range lines {
		if strings.HasPrefix(line, "Fluxo bootstrap token") || strings.HasPrefix(line, "Fluxo admin username:") {
			continue
		}
		kept = append(kept, line)
	}
	contents := strings.TrimRight(strings.Join(kept, "\n"), "\n")
	if contents != "" {
		contents += "\n"
	}
	contents += "Fluxo bootstrap token (reset): " + token + "\n"
	if persistUsername {
		contents += "Fluxo admin username: " + username + "\n"
	}
	if len(contents) > 64*1024 {
		return fmt.Errorf("credentials file exceeds the 64 KiB safety limit")
	}
	return rewriteCredentialsFile(path, []byte(contents))
}

func adminUsernameMessage(username string) string {
	if username == "" || username == "__bootstrap__" {
		return "No admin username is configured yet. Choose a username for first login."
	}
	if !safeinput.ValidateAdminUsername(username) {
		return "Admin username: " + strconv.QuoteToGraphic(username)
	}
	return "Admin username: " + username
}

// ShowAdminUsername prints the configured administrator identity without changing it.
func ShowAdminUsername(dbPath string, out io.Writer) error {
	username, err := database.ReadAdminUsername(dbPath)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, adminUsernameMessage(username))
	return nil
}

func matchingResetToken(path, tokenHash string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("credentials path is not a regular file")
	}
	if info.Size() > 64*1024 {
		return "", fmt.Errorf("credentials file exceeds the 64 KiB safety limit")
	}
	contents, err := readCredentialsFile(path)
	if err != nil {
		return "", err
	}
	return matchingResetTokenIn(contents, tokenHash), nil
}

func matchingResetTokenIn(contents []byte, tokenHash string) string {
	lines := strings.Split(string(contents), "\n")
	checked := 0
	for i := len(lines) - 1; i >= 0 && checked < 20; i-- {
		const prefix = "Fluxo bootstrap token (reset):"
		if !strings.HasPrefix(lines[i], prefix) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(lines[i], prefix))
		if !validCredentialValue(value) {
			continue
		}
		checked++
		if bcrypt.CompareHashAndPassword([]byte(tokenHash), []byte(value)) == nil {
			return value
		}
	}
	return ""
}

func matchingResetTokenFromTail(path, tokenHash string) (string, error) {
	contents, err := readCredentialsTail(path)
	if err != nil {
		return "", err
	}
	return matchingResetTokenIn(contents, tokenHash), nil
}

func sanitizeOversizedCurrentCredentials(path, tokenHash string) error {
	resetToken, err := matchingResetTokenFromTail(path, tokenHash)
	if err != nil {
		return err
	}
	contents := "Fluxo Installation Credentials\n==============================\n"
	if resetToken != "" {
		contents += "\nFluxo bootstrap token (reset): " + resetToken + "\n"
	}
	return rewriteCredentialsFile(path, []byte(contents))
}

func removeCredentialFile(path string) error {
	if err := os.Remove(path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func sanitizeAcknowledgedCredentialsPath(path, tokenHash string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("credentials path is not a regular file")
	}
	if info.Size() > 64*1024 {
		return fmt.Errorf("credentials file exceeds the 64 KiB safety limit")
	}
	contents, err := readCredentialsFile(path)
	if err != nil {
		return err
	}
	resetToken, err := matchingResetToken(path, tokenHash)
	if err != nil {
		return err
	}

	lines := strings.Split(string(contents), "\n")
	kept := make([]string, 0, len(lines))
	removed := false
	preservedReset := false
	for _, line := range lines {
		secret := strings.HasPrefix(line, "Fluxo bootstrap token") ||
			strings.HasPrefix(line, "Fluxo sudo password:") ||
			strings.HasPrefix(line, "MySQL fluxo user password:") ||
			strings.HasPrefix(line, "PostgreSQL fluxo user password:")
		value := strings.TrimSpace(strings.TrimPrefix(line, "Fluxo bootstrap token (reset):"))
		preserve := !preservedReset && resetToken != "" && strings.HasPrefix(line, "Fluxo bootstrap token (reset):") && value == resetToken
		if secret && !preserve {
			removed = true
			continue
		}
		if preserve {
			preservedReset = true
		}
		kept = append(kept, line)
	}
	if !removed {
		return nil
	}
	return rewriteCredentialsFile(path, []byte(strings.Join(kept, "\n")))
}

func sanitizeAcknowledgedCredentials(dataDir, tokenHash string, migrateLegacy bool) error {
	path := CredentialsPath(dataDir)
	const legacyPath = "/home/fluxo/.fluxo_credentials"
	if migrateLegacy && filepath.Clean(path) != legacyPath {
		if info, err := os.Lstat(legacyPath); err == nil {
			switch {
			case info.Mode()&os.ModeSymlink != 0:
				if err := os.Remove(legacyPath); err != nil {
					return fmt.Errorf("remove legacy credentials symlink: %w", err)
				}
			case info.Mode().IsRegular() && info.Size() <= 64*1024:
				legacyReset, err := matchingResetToken(legacyPath, tokenHash)
				if err != nil {
					return fmt.Errorf("inspect legacy credentials: %w", err)
				}
				currentReset, err := matchingResetToken(path, tokenHash)
				if err != nil {
					return fmt.Errorf("inspect current credentials: %w", err)
				}
				if legacyReset != "" && currentReset == "" {
					if err := appendCredential(dataDir, false, "Fluxo bootstrap token (reset)", legacyReset); err != nil {
						return fmt.Errorf("preserve legacy reset token: %w", err)
					}
				}
				if err := sanitizeAcknowledgedCredentialsPath(legacyPath, tokenHash); err != nil {
					return fmt.Errorf("sanitize legacy credentials: %w", err)
				}
				if err := os.Remove(legacyPath); err != nil {
					return fmt.Errorf("remove sanitized legacy credentials: %w", err)
				}
			case info.Mode().IsRegular():
				if err := os.MkdirAll(dataDir, 0700); err != nil {
					return err
				}
				legacyReset, err := matchingResetTokenFromTail(legacyPath, tokenHash)
				if err != nil {
					return fmt.Errorf("inspect oversized legacy credentials: %w", err)
				}
				currentReset, err := matchingResetToken(path, tokenHash)
				if err != nil {
					return fmt.Errorf("inspect current credentials: %w", err)
				}
				if legacyReset != "" && currentReset == "" {
					if err := appendCredential(dataDir, false, "Fluxo bootstrap token (reset)", legacyReset); err != nil {
						return fmt.Errorf("preserve oversized legacy reset token: %w", err)
					}
				}
				if err := removeCredentialFile(legacyPath); err != nil {
					return fmt.Errorf("remove oversized legacy credentials: %w", err)
				}
			default:
				return fmt.Errorf("legacy credentials path is not a regular file")
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() && info.Size() > 64*1024 {
		return sanitizeOversizedCurrentCredentials(path, tokenHash)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return sanitizeAcknowledgedCredentialsPath(path, tokenHash)
}

func repairManagedPostgresGrants() {
	rows, err := database.DB.Query("SELECT name, username FROM databases WHERE engine = 'postgres'")
	if err != nil {
		log.Printf("Warning: failed to load managed PostgreSQL databases for grant repair: %v", err)
		return
	}
	type managedDatabase struct {
		name, username string
	}
	databases := make([]managedDatabase, 0)
	for rows.Next() {
		var item managedDatabase
		if err := rows.Scan(&item.name, &item.username); err == nil {
			databases = append(databases, item)
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("Warning: failed while reading managed PostgreSQL databases for grant repair: %v", err)
	}
	rows.Close()

	for _, item := range databases {
		grantees, err := postgres.ListDatabaseGrantees(item.name)
		if err != nil {
			log.Printf("Warning: failed to inspect PostgreSQL grants for %s: %v", item.name, err)
			continue
		}
		if item.username != "" && item.username != "fluxo" && item.username != "postgres" {
			grantees = append(grantees, item.username)
		}
		seen := make(map[string]struct{})
		for _, grantee := range grantees {
			if _, duplicate := seen[grantee]; duplicate {
				continue
			}
			seen[grantee] = struct{}{}
			if err := postgres.GrantDatabaseAccess(item.name, grantee); err != nil {
				log.Printf("Warning: failed to repair PostgreSQL access for %s on %s: %v", grantee, item.name, err)
			}
		}
	}
}

// InitAdminToken bootstraps day-zero auth: creates a sentinel user with a random token on first run.
func InitAdminToken(dataDir string, migrateLegacy bool) {
	if _, err := prepareCredentialsFile(dataDir, migrateLegacy); err != nil {
		log.Fatalf("Failed to prepare credentials file: %v", err)
	}
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

		tx, err := database.DB.Begin()
		if err != nil {
			log.Fatalf("Failed to begin bootstrap transaction: %v", err)
		}
		if _, err = tx.Exec("INSERT INTO users (username, token_hash) VALUES (?, ?)", "__bootstrap__", hashStr); err != nil {
			tx.Rollback()
			log.Fatalf("Failed to create bootstrap user: %v", err)
		}

		// Persist the only copy before committing the matching hash. If this fails,
		// the transaction is rolled back so the next start can safely try again.
		credentialsPath := CredentialsPath(dataDir)
		if err := appendCredential(dataDir, migrateLegacy, "Fluxo bootstrap token", token); err != nil {
			tx.Rollback()
			log.Fatalf("Failed to save bootstrap token securely: %v", err)
		}
		if err := tx.Commit(); err != nil {
			log.Fatalf("Failed to commit bootstrap user: %v", err)
		}
		log.Println("=========================================================")
		log.Println("DAY ZERO AUTHENTICATION")
		log.Printf("Bootstrap token saved to %s", credentialsPath)
		log.Printf("Read it with: sudo cat %s", credentialsPath)
		log.Println("=========================================================")
		return
	}

	var copied int
	var tokenHash string
	if err := database.DB.QueryRow("SELECT credentials_copied, token_hash FROM users ORDER BY id ASC LIMIT 1").Scan(&copied, &tokenHash); err != nil {
		log.Fatalf("Failed to inspect credential acknowledgement state: %v", err)
	}
	if copied != 0 {
		if err := sanitizeAcknowledgedCredentials(dataDir, tokenHash, migrateLegacy); err != nil {
			log.Fatalf("Failed to sanitize acknowledged credentials file: %v", err)
		}
	}
}

// InitFluxoUser creates and configures the fluxo system user (idempotent).
func InitFluxoUser(dataDir string) {
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
	repairManagedSiteOwnership()

	// Set or load the fluxo sudo password via chpasswd (no shell interpolation).
	var existingSudoPass string
	database.DB.QueryRow("SELECT fluxo_sudo_password FROM users WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)").Scan(&existingSudoPass)
	existingSudoPass = config.Decrypt(existingSudoPass)

	sudoPass := existingSudoPass
	if sudoPass == "" {
		sudoPass = ReadCredential(dataDir, "Fluxo sudo password")
		if sudoPass == "" {
			sudoPass = generatePassword(16)
		}
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
		mysqlPass = ReadCredential(dataDir, "MySQL fluxo user password")
		if mysqlPass == "" {
			mysqlPass = generatePassword(16)
		}
		database.DB.Exec("UPDATE users SET fluxo_mysql_password = ? WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)", config.Encrypt(mysqlPass))
	}

	// Set or load the fluxo PostgreSQL password
	var existingPostgresPass string
	database.DB.QueryRow("SELECT fluxo_postgres_password FROM users WHERE id = (SELECT id FROM users ORDER BY id ASC LIMIT 1)").Scan(&existingPostgresPass)
	existingPostgresPass = config.Decrypt(existingPostgresPass)

	postgresPass := existingPostgresPass
	if postgresPass == "" {
		postgresPass = ReadCredential(dataDir, "PostgreSQL fluxo user password")
		if postgresPass == "" {
			postgresPass = generatePassword(16)
		}
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
		if err := postgres.SyncAdminRole(postgresPass); err != nil {
			log.Printf("Warning: failed to sync PostgreSQL fluxo role: %v", err)
		} else {
			log.Println("PostgreSQL fluxo role synced successfully.")
		}
		repairManagedPostgresGrants()
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

func managedSiteOwnershipTarget(domain, storedPath string) (string, bool) {
	if !safeinput.ValidateDomain(domain) {
		return "", false
	}
	managedPath, err := safeinput.NormalizeManagedSitePath(storedPath)
	if err != nil {
		return "", false
	}
	return managedPath, true
}

func ownershipMatches(path string, uid, gid uint32) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("unsupported ownership metadata for %s", path)
	}
	return stat.Uid == uid && stat.Gid == gid, nil
}

// repairManagedSiteOwnership fixes the root-owned site trees created by older
// releases before unprivileged Git and application commands touch them. It is
// deliberately limited to validated site paths stored by Fluxo and only runs
// chown when the site root or releases directory has the wrong owner.
func repairManagedSiteOwnership() {
	fluxoUser, err := user.Lookup("fluxo")
	if err != nil {
		log.Printf("Warning: cannot repair site ownership without the fluxo user: %v", err)
		return
	}
	wwwDataGroup, err := user.LookupGroup("www-data")
	if err != nil {
		log.Printf("Warning: cannot repair site ownership without the www-data group: %v", err)
		return
	}
	uidValue, uidErr := strconv.ParseUint(fluxoUser.Uid, 10, 32)
	gidValue, gidErr := strconv.ParseUint(wwwDataGroup.Gid, 10, 32)
	if uidErr != nil || gidErr != nil {
		log.Printf("Warning: cannot repair site ownership with invalid user/group IDs")
		return
	}
	uid, gid := uint32(uidValue), uint32(gidValue)

	rows, err := database.DB.Query("SELECT domain, path FROM sites WHERE COALESCE(deletion_status, '') = ''")
	if err != nil {
		log.Printf("Warning: failed to list sites for ownership repair: %v", err)
		return
	}
	defer rows.Close()

	type managedSite struct {
		domain string
		path   string
	}
	var sites []managedSite
	for rows.Next() {
		var site managedSite
		if err := rows.Scan(&site.domain, &site.path); err != nil {
			log.Printf("Warning: failed to inspect a site for ownership repair: %v", err)
			continue
		}
		sites = append(sites, site)
	}
	if err := rows.Err(); err != nil {
		log.Printf("Warning: failed while listing sites for ownership repair: %v", err)
		return
	}
	rows.Close()

	for _, site := range sites {
		target, ok := managedSiteOwnershipTarget(site.domain, site.path)
		if !ok {
			log.Printf("Warning: skipped unsafe site ownership target for %q", site.domain)
			continue
		}
		info, err := os.Lstat(target)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			log.Printf("Warning: failed to inspect site ownership for %s: %v", site.domain, err)
			continue
		}
		if !info.IsDir() {
			log.Printf("Warning: skipped non-directory site ownership target for %s", site.domain)
			continue
		}

		rootMatches, err := ownershipMatches(target, uid, gid)
		if err != nil {
			log.Printf("Warning: failed to inspect site root ownership for %s: %v", site.domain, err)
			continue
		}
		needsRepair := !rootMatches
		releasesPath := filepath.Join(target, "releases")
		if releasesMatch, err := ownershipMatches(releasesPath, uid, gid); err == nil {
			needsRepair = needsRepair || !releasesMatch
		} else if !os.IsNotExist(err) {
			log.Printf("Warning: failed to inspect releases ownership for %s: %v", site.domain, err)
			continue
		}
		if !needsRepair {
			continue
		}

		out, repairErr := syscmd.Run(context.Background(), 2*time.Minute, "chown", "-R", "fluxo:www-data", target)
		if repairErr != nil {
			log.Printf("Warning: failed to repair ownership for %s: %v\n%s", site.domain, repairErr, string(out))
			continue
		}
		log.Printf("Repaired site ownership for %s", site.domain)
	}
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

// ResetAdminToken resets the admin token and writes recovery details to out.
func ResetAdminToken(dataDir string, migrateLegacy bool, out io.Writer) {
	token := generateToken()
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("Failed to hash token: %v", err)
	}
	hashStr := string(hashBytes)

	var id int
	var username string
	err = database.DB.QueryRow("SELECT id, username FROM users ORDER BY id ASC LIMIT 1").Scan(&id, &username)
	if errors.Is(err, sql.ErrNoRows) {
		// No users exist, create bootstrap user
		_, err = database.DB.Exec("INSERT INTO users (username, token_hash) VALUES (?, ?)", "__bootstrap__", hashStr)
		if err != nil {
			log.Fatalf("Failed to create bootstrap user: %v", err)
		}
		username = "__bootstrap__"
	} else if err != nil {
		log.Fatalf("Failed to retrieve admin user: %v", err)
	} else {
		_, err = database.DB.Exec("UPDATE users SET token_hash = ? WHERE id = ?", hashStr, id)
		if err != nil {
			log.Fatalf("Failed to reset token: %v", err)
		}
	}

	// Persist recovery details when possible and fall back to the provided output.
	credentialsPath := CredentialsPath(dataDir)
	if err := writeAccountRecoveryCredentials(dataDir, migrateLegacy, username, token); err == nil {
		fmt.Fprintln(out, adminUsernameMessage(username))
		fmt.Fprintf(out, "New token saved to %s\n", credentialsPath)
		fmt.Fprintf(out, "Read it with: sudo cat %s\n", credentialsPath)
	} else {
		log.Printf("Warning: failed to save account recovery credentials to %s: %v", credentialsPath, err)
		fmt.Fprintln(out, "=========================================================")
		fmt.Fprintln(out, "ADMIN TOKEN RESET SUCCESSFUL")
		fmt.Fprintln(out, adminUsernameMessage(username))
		fmt.Fprintf(out, "New Token: %s\n", token)
		fmt.Fprintln(out, "Use this token to log in. Please save it securely.")
		fmt.Fprintln(out, "=========================================================")
	}
}
