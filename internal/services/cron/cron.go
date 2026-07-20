package cron

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"fluxo/internal/safeinput"
)

// Create writes a cron file to /etc/cron.d for the given cron job.
func Create(cronID int, workingDirectory, expression, command, cronUser string) error {
	path := filepath.Join("/etc/cron.d", fmt.Sprintf("fluxo-cron-%d", cronID))

	if cronUser == "" {
		cronUser = "fluxo"
	}
	if !safeinput.ValidateCronUser(cronUser, true) {
		return fmt.Errorf("invalid cron user: %s", cronUser)
	}
	if safeinput.HasControlChars(expression) || safeinput.HasControlChars(command) || safeinput.HasControlChars(cronUser) {
		return fmt.Errorf("invalid cron fields")
	}
	if !safeinput.ValidateCronExpression(expression) {
		return fmt.Errorf("invalid cron expression")
	}

	var cdPrefix string
	if workingDirectory != "" {
		workingDirectory = filepath.Clean(workingDirectory)
		if !filepath.IsAbs(workingDirectory) || !strings.HasPrefix(workingDirectory, "/home/fluxo/") || safeinput.HasControlChars(workingDirectory) {
			return fmt.Errorf("invalid cron working directory")
		}
		cdPrefix = fmt.Sprintf("cd %s && ", workingDirectory)
	}

	escapedCommand := strings.ReplaceAll(command, "\r", " ")
	escapedCommand = strings.ReplaceAll(escapedCommand, "\n", " ")
	content := fmt.Sprintf("# Fluxo Cron ID: %d\n%s %s %s%s >> /var/log/fluxo/cron-%d.log 2>&1\n", cronID, expression, cronUser, cdPrefix, escapedCommand, cronID)

	os.MkdirAll("/var/log/fluxo", 0755)
	if u, err := user.Lookup("fluxo"); err == nil {
		if uid, err := strconv.Atoi(u.Uid); err == nil {
			if gid, err := strconv.Atoi(u.Gid); err == nil {
				os.Chown("/var/log/fluxo", uid, gid)
			}
		}
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write cron file: %w", err)
	}
	return nil
}

// Delete removes the cron file for the given cron ID.
func Delete(cronID int) error {
	path := filepath.Join("/etc/cron.d", fmt.Sprintf("fluxo-cron-%d", cronID))
	return os.Remove(path)
}
