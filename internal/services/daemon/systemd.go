package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	"fluxo/internal/services/processlog"
	"fluxo/internal/syscmd"
)

// GenerateServiceFile writes a systemd unit file for the given daemon.
func GenerateServiceFile(daemonID int, command, directory, userStr string, startSeconds, stopSeconds int, stopSignal string) error {
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

	logPath := filepath.Join("/var/log/fluxo", fmt.Sprintf("fluxo-daemon-%d.log", daemonID))
	if err := processlog.Prepare(logPath, userStr); err != nil {
		return err
	}

	instances := daemonInstances(daemonID)
	desired := make(map[string]bool, instances)
	for instance := 1; instance <= instances; instance++ {
		serviceName := daemonServiceName(daemonID, instance)
		servicePath := filepath.Join("/etc/systemd/system", serviceName)
		desired[serviceName] = true
		content := fmt.Sprintf(`[Unit]
Description=Fluxo Daemon %d (%d/%d)
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
`, daemonID, instance, instances, userStr, directory, command, startSeconds, stopSeconds, stopSignal, daemonID, daemonID)

		if err := writeServiceFile(servicePath, []byte(content)); err != nil {
			return fmt.Errorf("failed to write service file: %w", err)
		}
	}

	for _, serviceName := range existingDaemonServiceNames(daemonID) {
		if desired[serviceName] {
			continue
		}
		if err := os.Remove(filepath.Join("/etc/systemd/system", serviceName)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove stale service file: %w", err)
		}
	}
	return nil
}

func daemonInstances(daemonID int) int {
	instances := 1
	if database.DB != nil {
		_ = database.DB.QueryRow("SELECT COALESCE(instances, 1) FROM daemons WHERE id = ?", daemonID).Scan(&instances)
	}
	if instances < 1 {
		return 1
	}
	if instances > 64 {
		return 64
	}
	return instances
}

func daemonServiceName(daemonID, instance int) string {
	if instance <= 1 {
		return fmt.Sprintf("fluxo-daemon-%d.service", daemonID)
	}
	return fmt.Sprintf("fluxo-daemon-%d-%d.service", daemonID, instance)
}

func daemonServiceNames(daemonID int) []string {
	instances := daemonInstances(daemonID)
	names := make([]string, 0, instances)
	for instance := 1; instance <= instances; instance++ {
		names = append(names, daemonServiceName(daemonID, instance))
	}
	return names
}

func installedDaemonServiceNames(daemonID int) ([]string, error) {
	entries, err := os.ReadDir("/etc/systemd/system")
	if err != nil {
		return nil, err
	}
	primary := daemonServiceName(daemonID, 1)
	prefix := fmt.Sprintf("fluxo-daemon-%d-", daemonID)
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == primary {
			names = append(names, name)
			continue
		}
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".service") {
			continue
		}
		instanceText := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".service")
		instance, err := strconv.Atoi(instanceText)
		if err == nil && instance >= 2 {
			names = append(names, name)
		}
	}
	return names, nil
}

func existingDaemonServiceNames(daemonID int) []string {
	names := daemonServiceNames(daemonID)
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		seen[name] = true
	}
	installed, err := installedDaemonServiceNames(daemonID)
	if err != nil {
		return names
	}
	for _, name := range installed {
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	return names
}

func sameServiceNameSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	leftSet := make(map[string]bool, len(left))
	for _, name := range left {
		leftSet[name] = true
	}
	for _, name := range right {
		if !leftSet[name] {
			return false
		}
	}
	return true
}

// ReconcileServiceFiles expands legacy single-unit daemons into their configured
// process groups without restarting an already-running primary process.
func ReconcileServiceFiles(ctx context.Context) (int, error) {
	rows, err := database.DB.Query(`SELECT id, command, directory, user,
		start_seconds, stop_seconds, stop_signal FROM daemons ORDER BY id`)
	if err != nil {
		return 0, err
	}
	type record struct {
		id, startSeconds, stopSeconds        int
		command, directory, user, stopSignal string
	}
	var records []record
	for rows.Next() {
		var item record
		if err := rows.Scan(&item.id, &item.command, &item.directory, &item.user, &item.startSeconds, &item.stopSeconds, &item.stopSignal); err != nil {
			rows.Close()
			return 0, err
		}
		records = append(records, item)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	reconciled := 0
	var reconcileErrors []string
	for _, item := range records {
		installed, err := installedDaemonServiceNames(item.id)
		if err != nil {
			reconcileErrors = append(reconcileErrors, fmt.Sprintf("daemon %d: inspect service files: %v", item.id, err))
			continue
		}
		primary := daemonServiceName(item.id, 1)
		primaryInstalled := false
		for _, name := range installed {
			if name == primary {
				primaryInstalled = true
				break
			}
		}
		if !primaryInstalled || sameServiceNameSet(installed, daemonServiceNames(item.id)) {
			continue
		}

		_, activeErr := syscmd.Run(ctx, 5*time.Second, "systemctl", "is-active", "--quiet", primary)
		_, enabledErr := syscmd.Run(ctx, 5*time.Second, "systemctl", "is-enabled", "--quiet", primary)
		desired := make(map[string]bool)
		for _, name := range daemonServiceNames(item.id) {
			desired[name] = true
		}
		cleanupFailed := false
		for _, name := range installed {
			if desired[name] {
				continue
			}
			if _, err := syscmd.Run(ctx, 30*time.Second, "systemctl", "stop", name); err != nil {
				reconcileErrors = append(reconcileErrors, fmt.Sprintf("daemon %d: stop stale %s: %v", item.id, name, err))
				cleanupFailed = true
				break
			}
			if _, err := syscmd.Run(ctx, 30*time.Second, "systemctl", "disable", name); err != nil {
				reconcileErrors = append(reconcileErrors, fmt.Sprintf("daemon %d: disable stale %s: %v", item.id, name, err))
				cleanupFailed = true
				break
			}
		}
		if cleanupFailed {
			continue
		}
		if err := GenerateServiceFile(item.id, item.command, item.directory, item.user, item.startSeconds, item.stopSeconds, item.stopSignal); err != nil {
			reconcileErrors = append(reconcileErrors, fmt.Sprintf("daemon %d: generate service files: %v", item.id, err))
			continue
		}
		if err := Reload(ctx); err != nil {
			reconcileErrors = append(reconcileErrors, fmt.Sprintf("daemon %d: reload systemd: %v", item.id, err))
			continue
		}
		if enabledErr == nil {
			if err := Enable(ctx, item.id); err != nil {
				reconcileErrors = append(reconcileErrors, fmt.Sprintf("daemon %d: enable process group: %v", item.id, err))
				continue
			}
		}
		if activeErr == nil {
			if err := Start(ctx, item.id); err != nil {
				reconcileErrors = append(reconcileErrors, fmt.Sprintf("daemon %d: start process group: %v", item.id, err))
				continue
			}
		}
		reconciled++
	}
	if len(reconcileErrors) > 0 {
		return reconciled, errors.New(strings.Join(reconcileErrors, "; "))
	}
	return reconciled, nil
}

func runSystemctlForDaemon(ctx context.Context, timeout time.Duration, action string, daemonID int) error {
	args := append([]string{action}, daemonServiceNames(daemonID)...)
	_, err := syscmd.Run(ctx, timeout, "systemctl", args...)
	return err
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
	return runSystemctlForDaemon(ctx, 30*time.Second, "enable", daemonID)
}

func Disable(ctx context.Context, daemonID int) error {
	return runSystemctlForDaemon(ctx, 30*time.Second, "disable", daemonID)
}

func IsActive(ctx context.Context, daemonID int) bool {
	for _, serviceName := range daemonServiceNames(daemonID) {
		if _, err := syscmd.Run(ctx, 5*time.Second, "systemctl", "is-active", "--quiet", serviceName); err != nil {
			return false
		}
	}
	return true
}

func IsEnabled(ctx context.Context, daemonID int) bool {
	for _, serviceName := range daemonServiceNames(daemonID) {
		if _, err := syscmd.Run(ctx, 5*time.Second, "systemctl", "is-enabled", "--quiet", serviceName); err != nil {
			return false
		}
	}
	return true
}

// Stop stops the daemon service.
func Stop(ctx context.Context, daemonID int) error {
	return runSystemctlForDaemon(ctx, 30*time.Second, "stop", daemonID)
}

// Restart restarts the daemon service.
func Restart(ctx context.Context, daemonID int) error {
	return runSystemctlForDaemon(ctx, 30*time.Second, "restart", daemonID)
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
	serviceNames := daemonServiceNames(daemonID)
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	stablePIDs := make(map[string]string, len(serviceNames))
	stableSince := make(map[string]time.Time, len(serviceNames))
	lastStates := make(map[string]string, len(serviceNames))
	for {
		allStable := true
		for _, serviceName := range serviceNames {
			out, err := syscmd.Run(ctx, 2*time.Second, "systemctl", "show", serviceName, "--property=ActiveState,SubState,MainPID")
			if err != nil {
				allStable = false
				continue
			}
			properties := map[string]string{}
			for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
				key, value, found := strings.Cut(line, "=")
				if found {
					properties[key] = value
				}
			}
			pid := properties["MainPID"]
			lastStates[serviceName] = properties["ActiveState"] + "/" + properties["SubState"]
			if properties["ActiveState"] == "active" && properties["SubState"] == "running" && pid != "" && pid != "0" {
				if stablePIDs[serviceName] != pid {
					stablePIDs[serviceName] = pid
					stableSince[serviceName] = time.Now()
					allStable = false
				} else if time.Since(stableSince[serviceName]) < 2*time.Second {
					allStable = false
				}
			} else {
				delete(stablePIDs, serviceName)
				delete(stableSince, serviceName)
				allStable = false
			}
		}
		if allStable {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("daemon services did not become stable (last states %v)", lastStates)
		case <-ticker.C:
		}
	}
}

// Start starts the daemon service.
func Start(ctx context.Context, daemonID int) error {
	return runSystemctlForDaemon(ctx, 30*time.Second, "start", daemonID)
}

// Delete stops, disables, and removes the daemon service file.
func Delete(ctx context.Context, daemonID int) error {
	var cleanupErrors []string
	for _, serviceName := range existingDaemonServiceNames(daemonID) {
		servicePath := filepath.Join("/etc/systemd/system", serviceName)
		loadState, stateErr := syscmd.Run(ctx, 5*time.Second, "systemctl", "show", serviceName, "--property=LoadState", "--value")
		shouldControl := stateErr != nil || strings.TrimSpace(loadState) != "not-found"
		if stateErr != nil {
			cleanupErrors = append(cleanupErrors, "inspect "+serviceName+": "+stateErr.Error())
		}
		if shouldControl {
			if _, err := syscmd.Run(ctx, 30*time.Second, "systemctl", "stop", serviceName); err != nil {
				cleanupErrors = append(cleanupErrors, "stop "+serviceName+": "+err.Error())
			}
			if _, err := syscmd.Run(ctx, 30*time.Second, "systemctl", "disable", serviceName); err != nil {
				cleanupErrors = append(cleanupErrors, "disable "+serviceName+": "+err.Error())
			}
		}
		if err := os.Remove(servicePath); err != nil && !os.IsNotExist(err) {
			cleanupErrors = append(cleanupErrors, "remove "+serviceName+": "+err.Error())
		}
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
	serviceNames := daemonServiceNames(daemonID)
	active := 0
	for _, serviceName := range serviceNames {
		out, err := syscmd.Run(ctx, 5*time.Second, "systemctl", "is-active", serviceName)
		if err == nil && strings.TrimSpace(out) == "active" {
			active++
		}
	}
	if active == len(serviceNames) {
		return "active"
	}
	if active > 0 {
		return "degraded"
	}
	return "stopped"
}
