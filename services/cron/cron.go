package cron

import (
	"fmt"
	"os"
	"path/filepath"
)

func Create(cronID int, domain, expression, command string) error {
	path := filepath.Join("/etc/cron.d", fmt.Sprintf("fluxo-cron-%d", cronID))

	content := fmt.Sprintf("# Fluxo Cron ID: %d\n%s www-data cd /var/www/%s/current && %s >> /var/log/fluxo/cron-%d.log 2>&1\n", cronID, expression, domain, command, cronID)

	os.MkdirAll("/var/log/fluxo", 0755)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write cron file: %w", err)
	}
	return nil
}

func Delete(cronID int) error {
	path := filepath.Join("/etc/cron.d", fmt.Sprintf("fluxo-cron-%d", cronID))
	return os.Remove(path)
}
