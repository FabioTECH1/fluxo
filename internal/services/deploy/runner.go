package deploy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"fluxo/internal/syscmd"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// LogBroadcaster streams real-time deploy logs to connected WebSocket clients.
type LogBroadcaster interface {
	BroadcastLog(siteID int, message string)
}

// RunScript writes protected temporary scripts, runs them as fluxo in an
// isolated process group, and streams output in real time.
func RunScript(ctx context.Context, siteID int, scriptContent, applicationCommands, privKeyPath string, envMap map[string]string, broadcaster LogBroadcaster) (string, error) {
	tmpScript, err := writeTempScript("fluxo_deploy_*.sh", scriptContent)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpScript.Name())

	var applicationScript *os.File
	if strings.TrimSpace(applicationCommands) != "" {
		applicationScript, err = writeTempScript("fluxo_application_*.sh", applicationCommands)
		if err != nil {
			return "", err
		}
		defer os.Remove(applicationScript.Name())
	}

	cmd := exec.Command("bash", tmpScript.Name())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Deploy scripts always run as fluxo, never as root. Credential lookup is
	// deliberately fail-closed so an incomplete server installation cannot
	// turn editable deployment commands into root commands.
	credential, err := syscmd.ResolveCredential("fluxo")
	if err != nil {
		return "", err
	}
	if credential.Uid == 0 {
		return "", fmt.Errorf("refusing to run deployment as root")
	}
	cmd.SysProcAttr.Credential = credential
	if err := os.Chown(tmpScript.Name(), int(credential.Uid), int(credential.Gid)); err != nil {
		return "", err
	}
	if applicationScript != nil {
		if err := os.Chown(applicationScript.Name(), int(credential.Uid), int(credential.Gid)); err != nil {
			return "", err
		}
	}

	// Use the site's deploy key for Git operations.
	sshCmd := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -i %s", privKeyPath)

	// Create a clean environment overriding HOME and USER. Reserved FLUXO_ values
	// are supplied per deployment below; filtering inherited values also ensures
	// legacy scripts cannot receive the retired FLUXO_DB_* credential variables.
	env := os.Environ()
	cleanEnv := []string{}
	for _, e := range env {
		if strings.HasPrefix(e, "HOME=") || strings.HasPrefix(e, "USER=") || strings.HasPrefix(e, "FLUXO_") {
			continue
		}
		cleanEnv = append(cleanEnv, e)
	}
	cleanEnv = append(cleanEnv, "HOME=/home/fluxo")
	cleanEnv = append(cleanEnv, "USER=fluxo")
	cleanEnv = append(cleanEnv, fmt.Sprintf("GIT_SSH_COMMAND=%s", sshCmd))
	for k, v := range envMap {
		cleanEnv = append(cleanEnv, fmt.Sprintf("%s=%s", k, v))
	}
	if applicationScript != nil {
		cleanEnv = append(cleanEnv, "FLUXO_APPLICATION_SCRIPT="+applicationScript.Name())
	}
	cmd.Env = cleanEnv

	// broadcasterWriter streams each write to WebSocket clients and accumulates the full log.
	if broadcaster == nil {
		broadcaster = &noOpBroadcaster{}
	}
	writer := &broadcasterWriter{siteID: siteID, broadcaster: broadcaster}
	cmd.Stdout = writer
	cmd.Stderr = writer

	broadcaster.BroadcastLog(siteID, "Starting deployment execution...")

	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("deployment cancelled before execution: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start deployment: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err = <-done:
		// Deployment scripts must not daemonize unmanaged descendants. Clean up
		// anything that outlived the shell before releasing the site queue.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	case <-ctx.Done():
		broadcaster.BroadcastLog(siteID, "Deployment cancellation requested; stopping all child processes...")
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		select {
		case <-done:
			// The shell may exit before a descendant. Kill anything that remains
			// in the isolated process group before another deployment can start.
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
		err = ctx.Err()
	}
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}

	finalOutput := writer.FullLog()

	if err != nil {
		broadcaster.BroadcastLog(siteID, fmt.Sprintf("Deployment failed: %v", err))
		if ctx.Err() != nil {
			return finalOutput, fmt.Errorf("deployment timed out or was cancelled: %w", ctx.Err())
		}
		return finalOutput, fmt.Errorf("deployment failed: %w", err)
	}

	if envMap["FLUXO_MANAGED_LIFECYCLE"] == "1" {
		broadcaster.BroadcastLog(siteID, "Deployment phase completed successfully.")
	} else {
		broadcaster.BroadcastLog(siteID, "Deployment completed successfully.")
	}
	return finalOutput, nil
}

func writeTempScript(pattern, content string) (*os.File, error) {
	tmpScript, err := os.CreateTemp("", pattern)
	if err != nil {
		return nil, err
	}
	removeOnError := true
	defer func() {
		if removeOnError {
			_ = os.Remove(tmpScript.Name())
		}
	}()

	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	if _, err := tmpScript.Write([]byte(normalized)); err != nil {
		_ = tmpScript.Close()
		return nil, err
	}
	if err := tmpScript.Close(); err != nil {
		return nil, err
	}
	if err := os.Chmod(tmpScript.Name(), 0700); err != nil {
		return nil, err
	}
	removeOnError = false
	return tmpScript, nil
}

// broadcasterWriter implements io.Writer, forwarding output to LogBroadcaster and accumulating the full log.
type broadcasterWriter struct {
	siteID      int
	broadcaster LogBroadcaster
	mu          sync.Mutex
	fullLog     string
}

// Write forwards output to the WebSocket broadcaster and accumulates the full log.
func (w *broadcasterWriter) Write(p []byte) (n int, err error) {
	str := ansiRe.ReplaceAllString(string(p), "")
	if str == "" {
		return len(p), nil
	}
	w.mu.Lock()
	w.fullLog += str
	w.mu.Unlock()
	w.broadcaster.BroadcastLog(w.siteID, str)
	return len(p), nil
}

func (w *broadcasterWriter) FullLog() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.fullLog
}

type noOpBroadcaster struct{}

// BroadcastLog is a no-op used when no WebSocket broadcaster is configured.
func (n *noOpBroadcaster) BroadcastLog(siteID int, message string) {}
