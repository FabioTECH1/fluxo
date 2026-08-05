package cron

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fluxo/internal/safeinput"
	"fluxo/internal/services/processlog"
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
	logPath := filepath.Join("/var/log/fluxo", fmt.Sprintf("cron-%d.log", cronID))
	if err := processlog.Prepare(logPath, cronUser); err != nil {
		return err
	}
	content := fmt.Sprintf("# Fluxo Cron ID: %d\n%s %s %s%s >> %s 2>&1\n", cronID, expression, cronUser, cdPrefix, escapedCommand, logPath)

	if err := writeCronConfig(path, content); err != nil {
		return fmt.Errorf("failed to write cron file: %w", err)
	}
	return nil
}

func writeCronConfig(path, content string) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".fluxo-cron-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0644); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.WriteString(content); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

// Delete removes the cron file for the given cron ID.
func Delete(cronID int) error {
	path := filepath.Join("/etc/cron.d", fmt.Sprintf("fluxo-cron-%d", cronID))
	return os.Remove(path)
}
