package database

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxBackups = 7

// BackupLoop runs a daily VACUUM INTO backup of the SQLite database.
// Backups are stored in dataDir/backups/ with the format fluxo-YYYY-MM-DD.db.
// Only the most recent maxBackups files are kept.
func BackupLoop(dbPath, dataDir string) {
	backupDir := filepath.Join(dataDir, "backups")
	os.MkdirAll(backupDir, 0700)

	for {
		time.Sleep(24 * time.Hour)
		runBackup(dbPath, backupDir)
	}
}

func runBackup(dbPath, backupDir string) {
	date := time.Now().Format("2006-01-02")
	backupFile := filepath.Join(backupDir, "fluxo-"+date+".db")

	if _, err := os.Stat(backupFile); err == nil {
		log.Printf("Backup: %s already exists, skipping.", backupFile)
	} else {
		_, err := DB.Exec("VACUUM INTO ?", backupFile)
		if err != nil {
			log.Printf("Backup: VACUUM INTO %s failed: %v", backupFile, err)
			return
		}
		log.Printf("Backup: created %s", backupFile)
	}

	pruneBackups(backupDir)
}

func pruneBackups(backupDir string) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}

	var backups []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "fluxo-") && strings.HasSuffix(e.Name(), ".db") {
			backups = append(backups, e.Name())
		}
	}

	if len(backups) <= maxBackups {
		return
	}

	sort.Strings(backups)
	for _, f := range backups[:len(backups)-maxBackups] {
		path := filepath.Join(backupDir, f)
		if err := os.Remove(path); err != nil {
			log.Printf("Backup: failed to prune %s: %v", path, err)
		} else {
			log.Printf("Backup: pruned old backup %s", f)
		}
	}
}
