package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fluxo/syscmd"
)

// GenerateServiceFile creates a systemd service file
func GenerateServiceFile(daemonID int, command, directory, user string, startSeconds, stopSeconds int, stopSignal string) error {
	serviceName := fmt.Sprintf("fluxo-daemon-%d.service", daemonID)
	servicePath := filepath.Join("/etc/systemd/system", serviceName)

	if user == "" {
		user = "www-data"
	}
	if startSeconds <= 0 {
		startSeconds = 1
	}
	if stopSeconds <= 0 {
		stopSeconds = 15
	}
	if stopSignal == "" {
		stopSignal = "SIGTERM"
	}

	content := fmt.Sprintf(`[Unit]
Description=Fluxo Daemon %d
After=network.target

[Service]
Type=simple
User=%s
WorkingDirectory=%s
ExecStart=%s
Restart=always
RestartSec=%d
TimeoutStopSec=%d
KillSignal=%s
StandardOutput=append:/var/log/fluxo/fluxo-daemon-%d.log
StandardError=append:/var/log/fluxo/fluxo-daemon-%d.log

[Install]
WantedBy=multi-user.target
`, daemonID, user, directory, command, startSeconds, stopSeconds, stopSignal, daemonID, daemonID)

	os.MkdirAll("/var/log/fluxo", 0755)

	if err := os.WriteFile(servicePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}

	return nil
}

func EnableAndStart(ctx context.Context, daemonID int) error {
	serviceName := fmt.Sprintf("fluxo-daemon-%d.service", daemonID)

	_, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "daemon-reload")
	if err != nil {
		return err
	}

	_, err = syscmd.Run(ctx, 10*time.Second, "systemctl", "enable", serviceName)
	if err != nil {
		return err
	}

	_, err = syscmd.Run(ctx, 10*time.Second, "systemctl", "start", serviceName)
	return err
}

func Stop(ctx context.Context, daemonID int) error {
	serviceName := fmt.Sprintf("fluxo-daemon-%d.service", daemonID)
	_, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "stop", serviceName)
	return err
}

func Restart(ctx context.Context, daemonID int) error {
	serviceName := fmt.Sprintf("fluxo-daemon-%d.service", daemonID)
	_, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "restart", serviceName)
	return err
}

func Delete(ctx context.Context, daemonID int) error {
	serviceName := fmt.Sprintf("fluxo-daemon-%d.service", daemonID)
	servicePath := filepath.Join("/etc/systemd/system", serviceName)

	syscmd.Run(ctx, 10*time.Second, "systemctl", "stop", serviceName)
	syscmd.Run(ctx, 10*time.Second, "systemctl", "disable", serviceName)

	os.Remove(servicePath)
	syscmd.Run(ctx, 10*time.Second, "systemctl", "daemon-reload")

	return nil
}

func Status(ctx context.Context, daemonID int) string {
	serviceName := fmt.Sprintf("fluxo-daemon-%d.service", daemonID)
	out, err := syscmd.Run(ctx, 5*time.Second, "systemctl", "is-active", serviceName)
	if err != nil {
		return "stopped"
	}
	return strings.TrimSpace(out)
}
