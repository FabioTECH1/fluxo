package bootstrap

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
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
	"fluxo/internal/services/daemon"
	"fluxo/internal/services/firewall"
	"fluxo/internal/services/mysql"
	"fluxo/internal/services/postgres"
	"fluxo/internal/services/processlog"
	sitepkg "fluxo/internal/services/site"
	sshservice "fluxo/internal/services/ssh"
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

func pendingAdminResetPath(dataDir string) string {
	return filepath.Join(dataDir, ".fluxo_token_reset_pending")
}

type pendingAdminReset struct {
	UserID int    `json:"user_id"`
	Token  string `json:"token"`
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

func writePendingAdminReset(dataDir string, pending pendingAdminReset) error {
	if pending.UserID < 0 || !validCredentialValue(pending.Token) {
		return fmt.Errorf("invalid pending admin reset")
	}
	data, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return rewriteCredentialsFile(pendingAdminResetPath(dataDir), data)
}

func readPendingAdminReset(dataDir string) (pendingAdminReset, bool, error) {
	var pending pendingAdminReset
	path := pendingAdminResetPath(dataDir)
	data, err := readCredentialsFile(path)
	if os.IsNotExist(err) {
		return pending, false, nil
	}
	if err != nil {
		return pending, false, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&pending); err != nil {
		return pending, false, fmt.Errorf("decode pending admin reset: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return pending, false, fmt.Errorf("pending admin reset contains trailing data")
	}
	if pending.UserID < 0 || !validCredentialValue(pending.Token) {
		return pending, false, fmt.Errorf("pending admin reset is invalid")
	}
	return pending, true, nil
}

func completePendingAdminReset(dataDir string, migrateLegacy bool) (string, bool, error) {
	pending, exists, err := readPendingAdminReset(dataDir)
	if err != nil || !exists {
		return "", false, err
	}

	userID := pending.UserID
	username := ""
	if userID > 0 {
		if err := database.DB.QueryRow("SELECT username FROM users WHERE id = ?", userID).Scan(&username); err != nil {
			return "", true, fmt.Errorf("find account for pending reset: %w", err)
		}
	} else {
		err := database.DB.QueryRow("SELECT id, username FROM users ORDER BY id ASC LIMIT 1").Scan(&userID, &username)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", true, fmt.Errorf("inspect account for pending reset: %w", err)
		}
		if errors.Is(err, sql.ErrNoRows) {
			username = "__bootstrap__"
		}
	}

	if err := writeAccountRecoveryCredentials(dataDir, migrateLegacy, username, pending.Token); err != nil {
		return "", true, fmt.Errorf("persist pending account recovery credentials: %w", err)
	}
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(pending.Token), bcrypt.DefaultCost)
	if err != nil {
		return "", true, fmt.Errorf("hash pending reset token: %w", err)
	}
	if userID == 0 {
		result, err := database.DB.Exec("INSERT INTO users (username, token_hash) VALUES (?, ?)", "__bootstrap__", string(hashBytes))
		if err != nil {
			return "", true, fmt.Errorf("create bootstrap account for pending reset: %w", err)
		}
		insertedID, _ := result.LastInsertId()
		userID = int(insertedID)
	} else {
		result, err := database.DB.Exec("UPDATE users SET token_hash = ?, token_version = token_version + 1 WHERE id = ?", string(hashBytes), userID)
		if err != nil {
			return "", true, fmt.Errorf("apply pending reset token: %w", err)
		}
		if affected, err := result.RowsAffected(); err != nil || affected != 1 {
			return "", true, fmt.Errorf("pending reset account no longer exists")
		}
	}
	if err := removeCredentialFile(pendingAdminResetPath(dataDir)); err != nil {
		return "", true, fmt.Errorf("remove completed pending reset: %w", err)
	}
	return username, true, nil
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

func repairManagedMySQLAccounts() {
	pendingUsers, err := database.ListManagedDatabaseUsers("mysql", database.ManagedDatabaseUserPending)
	if err != nil {
		log.Printf("Warning: failed to load interrupted MySQL user reservations: %v", err)
	} else {
		for _, item := range pendingUsers {
			exists, inspectErr := mysql.LocalAccountExists(item.Username, item.Host)
			if inspectErr != nil {
				log.Printf("Warning: failed to inspect interrupted MySQL user %s: %v", item.Username, inspectErr)
				continue
			}
			if exists {
				if activateErr := database.ActivateManagedDatabaseUser(item.Engine, item.Username, item.Host); activateErr != nil {
					log.Printf("Warning: failed to recover interrupted MySQL user %s: %v", item.Username, activateErr)
				}
			} else if cleanupErr := database.DeleteManagedDatabaseUser(item.Engine, item.Username, item.Host); cleanupErr != nil {
				log.Printf("Warning: failed to clear stale MySQL user reservation %s: %v", item.Username, cleanupErr)
			}
		}
	}

	rows, err := database.DB.Query(`
		SELECT username, GROUP_CONCAT(name, char(31))
		FROM databases
		WHERE engine = 'mysql' AND username NOT IN ('', 'fluxo', 'root')
		GROUP BY username`)
	if err != nil {
		log.Printf("Warning: failed to load managed MySQL users for TCP-account repair: %v", err)
		return
	}
	type managedUser struct {
		username  string
		databases []string
	}
	users := make([]managedUser, 0)
	for rows.Next() {
		var username, names string
		if err := rows.Scan(&username, &names); err == nil {
			users = append(users, managedUser{username: username, databases: strings.Split(names, string(rune(31)))})
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("Warning: failed while reading managed MySQL users for TCP-account repair: %v", err)
	}
	rows.Close()

	for _, item := range users {
		reserved, err := database.BeginManagedDatabaseUser("mysql", item.username, mysql.LocalTCPHost)
		if err != nil {
			log.Printf("Warning: failed to reserve local TCP access for MySQL user %s: %v", item.username, err)
			continue
		}
		state, stateErr := database.ManagedDatabaseUserState("mysql", item.username, mysql.LocalTCPHost)
		targetExists, targetErr := mysql.LocalAccountExists(item.username, mysql.LocalTCPHost)
		if stateErr == nil && targetErr == nil && state == database.ManagedDatabaseUserActive && targetExists {
			grantFailed := false
			for _, name := range item.databases {
				if grantErr := mysql.GrantDatabaseAccess(name, item.username); grantErr != nil {
					log.Printf("Warning: failed to repair MySQL access for %s on %s: %v", item.username, name, grantErr)
					grantFailed = true
				}
			}
			if !grantFailed {
				continue
			}
		}
		if err := mysql.EnsureTCPAccountFromLocalhost(item.username, item.databases, !reserved); err != nil {
			if reserved {
				if cleanupErr := database.DeleteManagedDatabaseUser("mysql", item.username, mysql.LocalTCPHost); cleanupErr != nil {
					log.Printf("Warning: failed to release TCP-account reservation for MySQL user %s: %v", item.username, cleanupErr)
				}
			}
			log.Printf("Warning: failed to repair local TCP access for MySQL user %s: %v", item.username, err)
			continue
		}
		if err := database.ActivateManagedDatabaseUser("mysql", item.username, mysql.LocalTCPHost); err != nil {
			log.Printf("Warning: failed to activate TCP-account ownership for MySQL user %s: %v", item.username, err)
		}
	}
}

// InitAdminToken bootstraps day-zero auth: creates a sentinel user with a random token on first run.
func InitAdminToken(dataDir string, migrateLegacy bool) {
	if _, err := prepareCredentialsFile(dataDir, migrateLegacy); err != nil {
		log.Fatalf("Failed to prepare credentials file: %v", err)
	}
	if username, completed, err := completePendingAdminReset(dataDir, migrateLegacy); err != nil {
		log.Fatalf("Failed to complete a pending admin-token reset: %v", err)
	} else if completed {
		log.Printf("Completed interrupted admin-token reset for %s", adminUsernameMessage(username))
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
		log.Printf("Bootstrap token saved to the root-only credentials file at %s", credentialsPath)
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
	if u, err := user.Lookup("fluxo"); err == nil {
		if uid, err := strconv.Atoi(u.Uid); err == nil {
			if gid, err := strconv.Atoi(u.Gid); err == nil {
				directory, openErr := sshservice.OpenManagedSSHDirectory("/home/fluxo", true, uid, gid)
				if openErr != nil {
					if os.Getenv("FLUXO_ENV") == "prod" {
						log.Fatalf("Failed to initialize the Fluxo SSH directory safely: %v", openErr)
					}
					log.Printf("Warning: failed to initialize the Fluxo SSH directory safely: %v", openErr)
				} else {
					directory.Close()
				}
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
		if err := mysql.SyncAdminUser(mysqlPass); err != nil {
			log.Printf("Warning: failed to sync MySQL fluxo user: %v", err)
		} else {
			log.Println("MySQL fluxo user and password synced successfully.")
			repairManagedMySQLAccounts()
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

	retireLegacyAssumedFirewallRules()
	importInstallerFirewallRules(dataDir)
	recoverVerifiedLegacyFirewallRules()

	reconcileCtx, cancelReconcile := context.WithTimeout(context.Background(), 2*time.Minute)
	if count, err := daemon.ReconcileServiceFiles(reconcileCtx); err != nil {
		log.Printf("Warning: could not fully reconcile daemon process groups: %v", err)
	} else if count > 0 {
		log.Printf("Reconciled %d daemon process group(s).", count)
	}
	cancelReconcile()

	repairManagedProcessLogs()
	initDefaultCrons()
}

func repairManagedProcessLogs() {
	repairManagedCronLogs()
	repairManagedDaemonLogs()
}

func repairManagedCronLogs() {
	rows, err := database.DB.Query(`SELECT c.id, c.site_id, c.expression, c.command, c.user,
		COALESCE(s.path, ''), COALESCE(s.deployment_strategy, 'standard')
		FROM crons c LEFT JOIN sites s ON c.site_id = s.id`)
	if err != nil {
		log.Printf("Warning: could not inspect managed cron logs: %v", err)
		return
	}
	type record struct {
		id, siteID                   int
		expression, command, user    string
		sitePath, deploymentStrategy string
	}
	var records []record
	for rows.Next() {
		var item record
		if err := rows.Scan(&item.id, &item.siteID, &item.expression, &item.command, &item.user, &item.sitePath, &item.deploymentStrategy); err != nil {
			log.Printf("Warning: could not read managed cron log metadata: %v", err)
			continue
		}
		records = append(records, item)
	}
	if err := rows.Err(); err != nil {
		log.Printf("Warning: could not finish reading managed cron logs: %v", err)
	}
	rows.Close()

	for _, item := range records {
		logPath := filepath.Join("/var/log/fluxo", fmt.Sprintf("cron-%d.log", item.id))
		safe, err := processlog.IsSafe(logPath)
		if err != nil {
			log.Printf("Warning: could not inspect cron %d log: %v", item.id, err)
			continue
		}
		if safe {
			if err := processlog.Prepare(logPath, item.user); err != nil {
				log.Printf("Warning: could not secure cron %d log: %v", item.id, err)
			}
			continue
		}

		if err := cron.Delete(item.id); err != nil && !os.IsNotExist(err) {
			log.Printf("Warning: disabled cron %d but could not remove its config: %v", item.id, err)
			continue
		}
		if err := processlog.Repair(logPath, item.user); err != nil {
			log.Printf("Warning: cron %d remains disabled because its unsafe log could not be repaired: %v", item.id, err)
			continue
		}
		workingDirectory := ""
		if item.siteID > 0 {
			workingDirectory = sitepkg.ActiveSitePath(item.sitePath, item.deploymentStrategy)
		}
		if err := cron.Create(item.id, workingDirectory, item.expression, item.command, item.user); err != nil {
			log.Printf("Warning: cron %d remains disabled after log repair: %v", item.id, err)
			continue
		}
		log.Printf("Repaired unsafe legacy process log for cron %d", item.id)
	}
}

func repairManagedDaemonLogs() {
	rows, err := database.DB.Query(`SELECT d.id, d.command, d.directory, d.user, d.start_seconds,
		d.stop_seconds, d.stop_signal, COALESCE(s.app_type, ''), COALESCE(s.path, '')
		FROM daemons d LEFT JOIN sites s ON s.id = d.site_id`)
	if err != nil {
		log.Printf("Warning: could not inspect managed daemon logs: %v", err)
		return
	}
	type record struct {
		id, startSeconds, stopSeconds        int
		command, directory, user, stopSignal string
		appType, sitePath                    string
	}
	var records []record
	for rows.Next() {
		var item record
		if err := rows.Scan(&item.id, &item.command, &item.directory, &item.user, &item.startSeconds, &item.stopSeconds, &item.stopSignal, &item.appType, &item.sitePath); err != nil {
			log.Printf("Warning: could not read managed daemon log metadata: %v", err)
			continue
		}
		records = append(records, item)
	}
	if err := rows.Err(); err != nil {
		log.Printf("Warning: could not finish reading managed daemon logs: %v", err)
	}
	rows.Close()

	ctx := context.Background()
	for _, item := range records {
		logPath := filepath.Join("/var/log/fluxo", fmt.Sprintf("fluxo-daemon-%d.log", item.id))
		safe, err := processlog.IsSafe(logPath)
		if err != nil {
			log.Printf("Warning: could not inspect daemon %d log: %v", item.id, err)
			continue
		}
		if safe {
			if err := processlog.Prepare(logPath, item.user); err != nil {
				log.Printf("Warning: could not secure daemon %d log: %v", item.id, err)
			}
			continue
		}

		wasActive := daemon.IsActive(ctx, item.id)
		wasEnabled := daemon.IsEnabled(ctx, item.id)
		if err := daemon.Delete(ctx, item.id); err != nil {
			log.Printf("Warning: daemon %d was quarantined but its cleanup was incomplete; its unsafe log was not reused: %v", item.id, err)
			continue
		}
		if err := processlog.Repair(logPath, item.user); err != nil {
			log.Printf("Warning: daemon %d remains stopped because its unsafe log could not be repaired: %v", item.id, err)
			continue
		}
		environmentFile := ""
		if item.appType == "python" && item.sitePath != "" {
			environmentFile = filepath.Join(item.sitePath, ".env")
		}
		if err := daemon.GenerateServiceFileWithEnvironmentFile(item.id, item.command, item.directory, item.user, item.startSeconds, item.stopSeconds, item.stopSignal, environmentFile); err != nil {
			log.Printf("Warning: daemon %d remains stopped after log repair: %v", item.id, err)
			continue
		}
		if err := daemon.Reload(ctx); err != nil {
			log.Printf("Warning: daemon %d remains stopped because systemd could not reload: %v", item.id, err)
			continue
		}
		if wasEnabled {
			if err := daemon.Enable(ctx, item.id); err != nil {
				log.Printf("Warning: daemon %d could not be re-enabled after log repair: %v", item.id, err)
				continue
			}
		}
		if wasActive {
			if err := daemon.Start(ctx, item.id); err != nil {
				log.Printf("Warning: daemon %d could not be restarted after log repair: %v", item.id, err)
				continue
			}
		}
		log.Printf("Repaired unsafe legacy process log for daemon %d", item.id)
	}
}

const installerFirewallManifestName = "installer-firewall-rules.json"

type installerFirewallManifest struct {
	Version int                     `json:"version"`
	Rules   []installerFirewallRule `json:"rules"`
}

type installerFirewallRule struct {
	Name     string `json:"name"`
	RuleType string `json:"rule_type"`
	Port     string `json:"port"`
	FromIP   string `json:"from_ip"`
}

func readInstallerFirewallManifest(path string) (installerFirewallManifest, error) {
	var manifest installerFirewallManifest
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return manifest, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return manifest, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm()&0022 != 0 {
		return manifest, fmt.Errorf("manifest must be daemon-owned, regular, and not group/world writable")
	}
	if info.Size() > 64*1024 {
		return manifest, fmt.Errorf("manifest exceeds the 64 KiB safety limit")
	}
	decoder := json.NewDecoder(io.LimitReader(f, 64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("decode manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return manifest, fmt.Errorf("manifest contains trailing data")
	}
	if manifest.Version != 1 || len(manifest.Rules) == 0 || len(manifest.Rules) > 32 {
		return manifest, fmt.Errorf("unsupported manifest version or rule count")
	}
	for i := range manifest.Rules {
		rule := &manifest.Rules[i]
		rule.Name = strings.TrimSpace(rule.Name)
		rule.RuleType = strings.ToLower(strings.TrimSpace(rule.RuleType))
		rule.Port = strings.TrimSpace(rule.Port)
		rule.FromIP = strings.TrimSpace(rule.FromIP)
		if rule.Name == "" || len(rule.Name) > 80 || safeinput.HasControlChars(rule.Name) ||
			!safeinput.ValidateFirewallAction(rule.RuleType) ||
			!safeinput.ValidateFirewallPortSpec(rule.Port) ||
			!safeinput.ValidateFirewallSource(rule.FromIP) {
			return manifest, fmt.Errorf("rule %d is invalid", i+1)
		}
		if rule.FromIP == "" {
			rule.FromIP = "Any"
		}
	}
	return manifest, nil
}

func importInstallerFirewallRules(dataDir string) {
	path := filepath.Join(dataDir, installerFirewallManifestName)
	manifest, err := readInstallerFirewallManifest(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		log.Printf("Warning: installer firewall rule manifest was not imported: %v", err)
		return
	}
	tx, err := database.DB.Begin()
	if err != nil {
		log.Printf("Warning: could not begin installer firewall rule import: %v", err)
		return
	}
	for _, rule := range manifest.Rules {
		result, err := tx.Exec(`UPDATE firewall_rules SET name = ?, managed_by = 'installer'
			WHERE rule_type = ? AND port = ? AND from_ip = ?`, rule.Name, rule.RuleType, rule.Port, rule.FromIP)
		if err != nil {
			tx.Rollback()
			log.Printf("Warning: could not import installer firewall rules: %v", err)
			return
		}
		updated, err := result.RowsAffected()
		if err != nil {
			tx.Rollback()
			log.Printf("Warning: could not inspect imported installer firewall rules: %v", err)
			return
		}
		if updated == 0 {
			if _, err := tx.Exec(`INSERT INTO firewall_rules (name, rule_type, port, from_ip, managed_by)
				VALUES (?, ?, ?, ?, 'installer')`, rule.Name, rule.RuleType, rule.Port, rule.FromIP); err != nil {
				tx.Rollback()
				log.Printf("Warning: could not import installer firewall rules: %v", err)
				return
			}
		}
	}
	if err := tx.Commit(); err != nil {
		log.Printf("Warning: could not commit installer firewall rules: %v", err)
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("Warning: imported installer firewall rules but could not consume the manifest: %v", err)
		return
	}
	log.Printf("Imported %d installer-managed firewall rules.", len(manifest.Rules))
}

func retireLegacyAssumedFirewallRules() {
	const migrationKey = "firewall_legacy_assumptions_retired_v1"
	var completed string
	if err := database.DB.QueryRow("SELECT value FROM system_metadata WHERE key = ?", migrationKey).Scan(&completed); err == nil {
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		log.Printf("Warning: could not inspect the legacy firewall migration: %v", err)
		return
	}
	tx, err := database.DB.Begin()
	if err != nil {
		log.Printf("Warning: could not begin the legacy firewall migration: %v", err)
		return
	}
	result, err := tx.Exec(`DELETE FROM firewall_rules WHERE
		(name = 'SSH' AND rule_type = 'allow' AND port = '22' AND from_ip = 'Any') OR
		(name = 'HTTP' AND rule_type = 'allow' AND port = '80' AND from_ip = 'Any') OR
		(name = 'HTTPS' AND rule_type = 'allow' AND port = '443' AND from_ip = 'Any') OR
		(name = 'Fluxo Daemon' AND rule_type = 'allow' AND port = '9595' AND from_ip = 'Any')`)
	if err != nil {
		tx.Rollback()
		log.Printf("Warning: could not retire legacy assumed firewall rules: %v", err)
		return
	}
	if retired, _ := result.RowsAffected(); retired > 0 {
		log.Printf("Retired %d legacy assumed firewall record(s); existing host UFW rules were left unchanged.", retired)
	}
	if _, err := tx.Exec("INSERT INTO system_metadata (key, value) VALUES (?, '1')", migrationKey); err != nil {
		tx.Rollback()
		log.Printf("Warning: could not record the legacy firewall migration: %v", err)
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("Warning: could not commit the legacy firewall migration: %v", err)
	}
}

func recoverVerifiedLegacyFirewallRules() {
	var completed string
	if err := database.DB.QueryRow("SELECT value FROM system_metadata WHERE key = ?", verifiedLegacyFirewallMigrationKey).Scan(&completed); err == nil {
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		log.Printf("Warning: could not inspect verified firewall reconciliation: %v", err)
		return
	}
	addedRules, err := firewall.AddedRules()
	if err != nil {
		log.Printf("Warning: could not reconcile legacy firewall rules with UFW: %v", err)
		return
	}
	recovered, err := reconcileLegacyFirewallRules(addedRules)
	if err != nil {
		log.Printf("Warning: could not reconcile legacy firewall rules: %v", err)
		return
	}
	if recovered > 0 {
		log.Printf("Recovered %d verified legacy firewall rule record(s) from UFW.", recovered)
	}
}

const verifiedLegacyFirewallMigrationKey = "firewall_verified_legacy_rules_recovered_v2"

func reconcileLegacyFirewallRules(addedRules string) (int64, error) {
	var completed string
	if err := database.DB.QueryRow("SELECT value FROM system_metadata WHERE key = ?", verifiedLegacyFirewallMigrationKey).Scan(&completed); err == nil {
		return 0, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("inspect verified firewall reconciliation: %w", err)
	}

	type storedRule struct {
		ruleType string
		port     string
		fromIP   string
	}
	rows, err := database.DB.Query("SELECT rule_type, port, from_ip FROM firewall_rules")
	if err != nil {
		return 0, fmt.Errorf("load managed firewall rules for reconciliation: %w", err)
	}
	stored := make([]storedRule, 0)
	for rows.Next() {
		var rule storedRule
		if err := rows.Scan(&rule.ruleType, &rule.port, &rule.fromIP); err != nil {
			rows.Close()
			return 0, fmt.Errorf("read managed firewall rule for reconciliation: %w", err)
		}
		stored = append(stored, rule)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("read managed firewall rules for reconciliation: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close managed firewall rules after reconciliation: %w", err)
	}

	candidates := []installerFirewallRule{
		{Name: "SSH", RuleType: "allow", Port: "22/tcp", FromIP: "Any"},
		{Name: "HTTP", RuleType: "allow", Port: "80/tcp", FromIP: "Any"},
		{Name: "HTTPS", RuleType: "allow", Port: "443/tcp", FromIP: "Any"},
		{Name: "Fluxo Dashboard", RuleType: "allow", Port: "9595/tcp", FromIP: "Any"},
	}
	tx, err := database.DB.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin verified firewall reconciliation: %w", err)
	}
	var recovered int64
	for _, candidate := range candidates {
		if !firewall.RuleExists(addedRules, candidate.Port, candidate.FromIP, candidate.RuleType) {
			continue
		}
		alreadyStored := false
		for _, rule := range stored {
			if strings.EqualFold(rule.ruleType, candidate.RuleType) &&
				firewall.NormalizePort(rule.port) == firewall.NormalizePort(candidate.Port) &&
				firewall.NormalizeSource(rule.fromIP) == firewall.NormalizeSource(candidate.FromIP) {
				alreadyStored = true
				break
			}
		}
		if alreadyStored {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO firewall_rules (name, rule_type, port, from_ip, managed_by)
			VALUES (?, ?, ?, ?, 'installer')`, candidate.Name, candidate.RuleType, candidate.Port, candidate.FromIP); err != nil {
			tx.Rollback()
			return 0, fmt.Errorf("recover verified %s firewall rule: %w", candidate.Name, err)
		}
		recovered++
	}
	if _, err := tx.Exec("INSERT INTO system_metadata (key, value) VALUES (?, '1')", verifiedLegacyFirewallMigrationKey); err != nil {
		tx.Rollback()
		return 0, fmt.Errorf("record verified firewall reconciliation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit verified firewall reconciliation: %w", err)
	}
	return recovered, nil
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
	}
	ensureComposerUpdateCron()

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

const (
	composerUpdateCronName        = "Update Composer"
	composerUpdateCronExpression  = "0 0 * * 0"
	composerUpdateCronCommand     = "/usr/bin/flock -n /run/lock/fluxo-composer.lock /usr/bin/env COMPOSER_ALLOW_SUPERUSER=1 /usr/local/bin/composer self-update --2 --stable --no-interaction --no-ansi"
	legacyComposerUpdateCommand   = "/usr/local/bin/composer self-update"
	unlockedComposerUpdateCommand = "/usr/local/bin/composer self-update --2 --stable --no-interaction"
)

type composerCronRecord struct {
	id         int
	expression string
	command    string
	user       string
}

func ensureComposerUpdateCron() {
	if _, err := exec.LookPath("composer"); err != nil {
		return
	}
	reconcileComposerUpdateCron(cron.Create, cron.Delete)
}

// reconcileComposerUpdateCron keeps one Fluxo-managed Composer maintenance job.
// The installer supplies a verified baseline; this job follows stable Composer 2
// releases between Fluxo upgrades, matching the maintenance model used by Forge.
func reconcileComposerUpdateCron(
	createCronFile func(int, string, string, string, string) error,
	deleteCronFile func(int) error,
) {
	rows, err := database.DB.Query(`SELECT id, expression, command, user FROM crons
		WHERE site_id = 0 AND name = ? ORDER BY id ASC`, composerUpdateCronName)
	if err != nil {
		log.Printf("Warning: could not inspect the Composer update cron: %v", err)
		return
	}
	var managed []composerCronRecord
	for rows.Next() {
		var record composerCronRecord
		if err := rows.Scan(&record.id, &record.expression, &record.command, &record.user); err != nil {
			log.Printf("Warning: could not read a Composer update cron: %v", err)
			continue
		}
		if record.command == legacyComposerUpdateCommand || record.command == unlockedComposerUpdateCommand || record.command == composerUpdateCronCommand {
			managed = append(managed, record)
		}
	}
	if err := rows.Close(); err != nil {
		log.Printf("Warning: could not finish reading Composer update crons: %v", err)
		return
	}
	if err := rows.Err(); err != nil {
		log.Printf("Warning: could not iterate Composer update crons: %v", err)
		return
	}

	if len(managed) == 0 {
		result, err := database.DB.Exec(`INSERT INTO crons (site_id, name, expression, command, user)
			VALUES (0, ?, ?, ?, 'root')`, composerUpdateCronName, composerUpdateCronExpression, composerUpdateCronCommand)
		if err != nil {
			log.Printf("Warning: failed to seed the Composer update cron: %v", err)
			return
		}
		id, _ := result.LastInsertId()
		if err := createCronFile(int(id), "", composerUpdateCronExpression, composerUpdateCronCommand, "root"); err != nil {
			log.Printf("Warning: failed to write the Composer update cron file: %v", err)
			database.DB.Exec("DELETE FROM crons WHERE id = ?", id)
			return
		}
		log.Printf("Default cron seeded: %s (%s)", composerUpdateCronName, composerUpdateCronExpression)
		return
	}

	keeper := managed[0]
	if err := createCronFile(keeper.id, "", composerUpdateCronExpression, composerUpdateCronCommand, "root"); err != nil {
		log.Printf("Warning: could not normalize the Composer update cron file %d: %v", keeper.id, err)
		return
	}
	if _, err := database.DB.Exec(`UPDATE crons SET expression = ?, command = ?, user = 'root' WHERE id = ?`,
		composerUpdateCronExpression, composerUpdateCronCommand, keeper.id); err != nil {
		log.Printf("Warning: could not normalize the Composer update cron record %d: %v", keeper.id, err)
		return
	}

	for _, record := range managed {
		if record.id == keeper.id {
			continue
		}
		if err := deleteCronFile(record.id); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("Warning: could not remove duplicate Composer update cron file %d: %v", record.id, err)
			continue
		}
		if _, err := database.DB.Exec("DELETE FROM crons WHERE id = ?", record.id); err != nil {
			log.Printf("Warning: could not remove duplicate Composer update cron record %d: %v", record.id, err)
			continue
		}
	}
}

// ResetAdminToken resets the admin token and writes recovery details to out.
func ResetAdminToken(dataDir string, migrateLegacy bool, out io.Writer) {
	token := generateToken()

	var id int
	var username string
	err := database.DB.QueryRow("SELECT id, username FROM users ORDER BY id ASC LIMIT 1").Scan(&id, &username)
	createBootstrap := errors.Is(err, sql.ErrNoRows)
	if createBootstrap {
		username = "__bootstrap__"
	} else if err != nil {
		log.Fatalf("Failed to retrieve admin user: %v", err)
	}

	if createBootstrap {
		id = 0
	}
	if err := writePendingAdminReset(dataDir, pendingAdminReset{UserID: id, Token: token}); err != nil {
		log.Fatalf("Failed to persist the pending admin-token reset: %v", err)
	}
	completedUsername, _, err := completePendingAdminReset(dataDir, migrateLegacy)
	if err != nil {
		log.Fatalf("Failed to apply the pending admin-token reset: %v", err)
	}
	username = completedUsername

	credentialsPath := CredentialsPath(dataDir)
	fmt.Fprintln(out, "=========================================================")
	fmt.Fprintln(out, "ADMIN TOKEN RESET SUCCESSFUL")
	fmt.Fprintln(out, adminUsernameMessage(username))
	fmt.Fprintf(out, "New token: %s\n", token)
	fmt.Fprintln(out, "Use this token to log in and store it securely.")
	fmt.Fprintf(out, "A recovery copy is stored in %s with root-only permissions.\n", credentialsPath)
	fmt.Fprintln(out, "=========================================================")
}
