package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fluxo/internal/safeinput"
	"fluxo/internal/services/processlog"
	"fluxo/internal/syscmd"
)

// GenerateServiceFile writes a systemd unit file for the given daemon.
func GenerateServiceFile(daemonID int, command, directory, userStr string, startSeconds, stopSeconds int, stopSignal string) error {
	serviceName := fmt.Sprintf("fluxo-daemon-%d.service", daemonID)
	servicePath := filepath.Join("/etc/systemd/system", serviceName)

	if userStr == "" {
		userStr = "fluxo"
	}
	if stopSignal == "" {
		stopSignal = "SIGTERM"
	}
	if !safeinput.ValidateCronUser(userStr, false) {
		return fmt.Errorf("invalid daemon user: %s (must be fluxo or www-data)", userStr)
	}
	if safeinput.HasControlChars(command) || safeinput.HasControlChars(directory) || safeinput.HasControlChars(stopSignal) {
		return fmt.Errorf("invalid daemon fields")
	}
	if !safeinput.ValidateSystemSignal(stopSignal) {
		return fmt.Errorf("invalid stop signal: %s", stopSignal)
	}
	command = strings.ReplaceAll(command, "\n", " ")
	command = strings.ReplaceAll(command, "\r", " ")
	directory = strings.ReplaceAll(directory, "\n", " ")
	directory = strings.ReplaceAll(directory, "\r", " ")
	if startSeconds <= 0 {
		startSeconds = 1
	}
	if stopSeconds <= 0 {
		stopSeconds = 15
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
`, daemonID, userStr, directory, command, startSeconds, stopSeconds, stopSignal, daemonID, daemonID)

	logPath := filepath.Join("/var/log/fluxo", fmt.Sprintf("fluxo-daemon-%d.log", daemonID))
	if err := processlog.Prepare(logPath, userStr); err != nil {
		return err
	}

	if err := writeServiceFile(servicePath, []byte(content)); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}

	return nil
}

func writeServiceFile(path string, content []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".fluxo-daemon-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0644); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
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

// EnableAndStart reloads systemd, enables, and starts the daemon service.
func EnableAndStart(ctx context.Context, daemonID int) error {
	if err := Reload(ctx); err != nil {
		return err
	}
	if err := Enable(ctx, daemonID); err != nil {
		return err
	}
	return Start(ctx, daemonID)
}

func Reload(ctx context.Context) error {
	_, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "daemon-reload")
	return err
}

func Enable(ctx context.Context, daemonID int) error {
	serviceName := fmt.Sprintf("fluxo-daemon-%d.service", daemonID)
	_, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "enable", serviceName)
	return err
}

func Disable(ctx context.Context, daemonID int) error {
	serviceName := fmt.Sprintf("fluxo-daemon-%d.service", daemonID)
	_, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "disable", serviceName)
	return err
}

func IsActive(ctx context.Context, daemonID int) bool {
	serviceName := fmt.Sprintf("fluxo-daemon-%d.service", daemonID)
	_, err := syscmd.Run(ctx, 5*time.Second, "systemctl", "is-active", "--quiet", serviceName)
	return err == nil
}

func IsEnabled(ctx context.Context, daemonID int) bool {
	serviceName := fmt.Sprintf("fluxo-daemon-%d.service", daemonID)
	_, err := syscmd.Run(ctx, 5*time.Second, "systemctl", "is-enabled", "--quiet", serviceName)
	return err == nil
}

// Stop stops the daemon service.
func Stop(ctx context.Context, daemonID int) error {
	serviceName := fmt.Sprintf("fluxo-daemon-%d.service", daemonID)
	_, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "stop", serviceName)
	return err
}

// Restart restarts the daemon service.
func Restart(ctx context.Context, daemonID int) error {
	serviceName := fmt.Sprintf("fluxo-daemon-%d.service", daemonID)
	_, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "restart", serviceName)
	return err
}

// RestartAndWait restarts a daemon and verifies that the same main process
// remains active long enough to rule out an immediate crash/restart loop.
func RestartAndWait(ctx context.Context, daemonID int) error {
	if err := Restart(ctx, daemonID); err != nil {
		return err
	}
	return WaitHealthy(ctx, daemonID)
}

// WaitHealthy requires a systemd daemon to remain active with a stable PID.
func WaitHealthy(ctx context.Context, daemonID int) error {
	serviceName := fmt.Sprintf("fluxo-daemon-%d.service", daemonID)
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	var stablePID string
	var stableSince time.Time
	var lastState string
	for {
		out, err := syscmd.Run(ctx, 2*time.Second, "systemctl", "show", serviceName, "--property=ActiveState,SubState,MainPID")
		if err == nil {
			properties := map[string]string{}
			for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
				key, value, found := strings.Cut(line, "=")
				if found {
					properties[key] = value
				}
			}
			pid := properties["MainPID"]
			lastState = properties["ActiveState"] + "/" + properties["SubState"]
			if properties["ActiveState"] == "active" && properties["SubState"] == "running" && pid != "" && pid != "0" {
				if stablePID != pid {
					stablePID = pid
					stableSince = time.Now()
				} else if time.Since(stableSince) >= 2*time.Second {
					return nil
				}
			} else {
				stablePID = ""
				stableSince = time.Time{}
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("%s did not become stable (last state %s)", serviceName, lastState)
		case <-ticker.C:
		}
	}
}

// Start starts the daemon service.
func Start(ctx context.Context, daemonID int) error {
	serviceName := fmt.Sprintf("fluxo-daemon-%d.service", daemonID)
	_, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "start", serviceName)
	return err
}

// Delete stops, disables, and removes the daemon service file.
func Delete(ctx context.Context, daemonID int) error {
	serviceName := fmt.Sprintf("fluxo-daemon-%d.service", daemonID)
	servicePath := filepath.Join("/etc/systemd/system", serviceName)

	var cleanupErrors []string
	loadState, stateErr := syscmd.Run(ctx, 5*time.Second, "systemctl", "show", serviceName, "--property=LoadState", "--value")
	shouldControl := stateErr != nil || strings.TrimSpace(loadState) != "not-found"
	if stateErr != nil {
		cleanupErrors = append(cleanupErrors, "inspect daemon service state: "+stateErr.Error())
	}
	if shouldControl {
		if _, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "stop", serviceName); err != nil {
			cleanupErrors = append(cleanupErrors, "stop daemon service: "+err.Error())
		}
		if _, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "disable", serviceName); err != nil {
			cleanupErrors = append(cleanupErrors, "disable daemon service: "+err.Error())
		}
	}
	if err := os.Remove(servicePath); err != nil && !os.IsNotExist(err) {
		cleanupErrors = append(cleanupErrors, "remove daemon service file: "+err.Error())
	}
	if err := Reload(ctx); err != nil {
		cleanupErrors = append(cleanupErrors, "reload systemd: "+err.Error())
	}
	if len(cleanupErrors) > 0 {
		return errors.New(strings.Join(cleanupErrors, "; "))
	}
	return nil
}

// Status returns the active state of the daemon service.
func Status(ctx context.Context, daemonID int) string {
	serviceName := fmt.Sprintf("fluxo-daemon-%d.service", daemonID)
	out, err := syscmd.Run(ctx, 5*time.Second, "systemctl", "is-active", serviceName)
	if err != nil {
		return "stopped"
	}
	return strings.TrimSpace(out)
}
