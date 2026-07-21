package deploy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// LogBroadcaster streams real-time deploy logs to connected WebSocket clients.
type LogBroadcaster interface {
	BroadcastLog(siteID int, message string)
}

// RunScript writes the deploy script to a temp file, runs it as www-data via bash, and streams output in real time.
func RunScript(ctx context.Context, siteID int, scriptContent string, privKeyPath string, envMap map[string]string, broadcaster LogBroadcaster) (string, error) {
	tmpScript, err := os.CreateTemp("", "fluxo_deploy_*.sh")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpScript.Name())

	normalized := strings.ReplaceAll(scriptContent, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	if _, err := tmpScript.Write([]byte(normalized)); err != nil {
		return "", err
	}
	tmpScript.Close()

	if err := os.Chmod(tmpScript.Name(), 0755); err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "bash", tmpScript.Name())

	// Deploy scripts always run as fluxo, never as root.
	u, err := user.Lookup("fluxo")
	if err == nil {
		uid, _ := strconv.ParseUint(u.Uid, 10, 32)
		gid, _ := strconv.ParseUint(u.Gid, 10, 32)
		cmd.SysProcAttr = &syscall.SysProcAttr{}
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
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
	cmd.Env = cleanEnv

	// broadcasterWriter streams each write to WebSocket clients and accumulates the full log.
	if broadcaster == nil {
		broadcaster = &noOpBroadcaster{}
	}
	writer := &broadcasterWriter{siteID: siteID, broadcaster: broadcaster}
	cmd.Stdout = writer
	cmd.Stderr = writer

	broadcaster.BroadcastLog(siteID, "Starting deployment execution...")

	err = cmd.Run()

	finalOutput := writer.fullLog

	if err != nil {
		broadcaster.BroadcastLog(siteID, fmt.Sprintf("Deployment failed: %v", err))
		return finalOutput, fmt.Errorf("deployment failed: %w", err)
	}

	broadcaster.BroadcastLog(siteID, "Deployment completed successfully.")
	return finalOutput, nil
}

// broadcasterWriter implements io.Writer, forwarding output to LogBroadcaster and accumulating the full log.
type broadcasterWriter struct {
	siteID      int
	broadcaster LogBroadcaster
	fullLog     string
}

// Write forwards output to the WebSocket broadcaster and accumulates the full log.
func (w *broadcasterWriter) Write(p []byte) (n int, err error) {
	str := ansiRe.ReplaceAllString(string(p), "")
	if str == "" {
		return len(p), nil
	}
	w.fullLog += str
	w.broadcaster.BroadcastLog(w.siteID, str)
	return len(p), nil
}

type noOpBroadcaster struct{}

// BroadcastLog is a no-op used when no WebSocket broadcaster is configured.
func (n *noOpBroadcaster) BroadcastLog(siteID int, message string) {}
