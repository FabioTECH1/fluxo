package deploy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

// LogBroadcaster defines the interface for streaming logs
type LogBroadcaster interface {
	BroadcastLog(siteID int, message string)
}

// RunScript executes a multi-line bash script.
// It pipes stdout and stderr to the broadcaster, and saves the output to the DB later.
func RunScript(ctx context.Context, siteID int, scriptContent string, privKeyPath string, broadcaster LogBroadcaster) (string, error) {
	// Write script to temp file
	tmpScript, err := os.CreateTemp("", "fluxo_deploy_*.sh")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpScript.Name())

	if _, err := tmpScript.Write([]byte(scriptContent)); err != nil {
		return "", err
	}
	tmpScript.Close()

	if err := os.Chmod(tmpScript.Name(), 0755); err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "bash", tmpScript.Name())

	u, err := user.Lookup("www-data")
	if err == nil {
		uid, _ := strconv.ParseUint(u.Uid, 10, 32)
		gid, _ := strconv.ParseUint(u.Gid, 10, 32)
		cmd.SysProcAttr = &syscall.SysProcAttr{}
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	}

	// Inject SSH StrictHostKeyChecking bypass
	sshCmd := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -i %s", privKeyPath)
	cmd.Env = append(os.Environ(), fmt.Sprintf("GIT_SSH_COMMAND=%s", sshCmd))

	// We use io.Pipe to capture output in real-time
	// However, for simplicity and robustness without goroutines leaking,
	// let's capture it and broadast periodically or write a custom writer.

	// Better approach: custom writer that broadasts
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

type broadcasterWriter struct {
	siteID      int
	broadcaster LogBroadcaster
	fullLog     string
}

func (w *broadcasterWriter) Write(p []byte) (n int, err error) {
	str := string(p)
	w.fullLog += str
	w.broadcaster.BroadcastLog(w.siteID, str)
	return len(p), nil
}
