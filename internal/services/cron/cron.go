package cron

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
)

func Create(cronID int, domain, expression, command, cronUser string) error {
	path := filepath.Join("/etc/cron.d", fmt.Sprintf("fluxo-cron-%d", cronID))

	if cronUser == "" {
		cronUser = "fluxo"
	}

	var cdPrefix string
	if domain != "" {
		cdPrefix = fmt.Sprintf("cd /home/fluxo/%s/current && ", domain)
	}

	content := fmt.Sprintf("# Fluxo Cron ID: %d\n%s %s %s%s >> /var/log/fluxo/cron-%d.log 2>&1\n", cronID, expression, cronUser, cdPrefix, command, cronID)

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

func Delete(cronID int) error {
	path := filepath.Join("/etc/cron.d", fmt.Sprintf("fluxo-cron-%d", cronID))
	return os.Remove(path)
}
