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

// LogBroadcaster is the interface for streaming real-time deploy logs
// to connected WebSocket clients. The server.Hub implements this.
type LogBroadcaster interface {
	BroadcastLog(siteID int, message string)
}

// RunScript writes the deploy script to a temp file, makes it executable,
// and runs it via bash as www-data. Output is streamed in real time to the
// broadcaster (WebSocket) and the full output is returned.
//
// The GIT_SSH_COMMAND environment variable is set to use the site-specific
// SSH private key with StrictHostKeyChecking disabled (acceptable for
// connecting to known Git hosts like GitHub/GitLab).
func RunScript(ctx context.Context, siteID int, scriptContent string, privKeyPath string, broadcaster LogBroadcaster) (string, error) {
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

	// Create a clean environment overriding HOME and USER
	env := os.Environ()
	cleanEnv := []string{}
	for _, e := range env {
		// Filter out existing HOME and USER
		if len(e) > 5 && e[:5] == "HOME=" {
			continue
		}
		if len(e) > 5 && e[:5] == "USER=" {
			continue
		}
		cleanEnv = append(cleanEnv, e)
	}
	cleanEnv = append(cleanEnv, "HOME=/home/fluxo")
	cleanEnv = append(cleanEnv, "USER=fluxo")
	cleanEnv = append(cleanEnv, fmt.Sprintf("GIT_SSH_COMMAND=%s", sshCmd))
	cmd.Env = cleanEnv

	// broadcasterWriter implements io.Writer to stream output line-by-line
	// to WebSocket clients via the LogBroadcaster interface.
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

// broadcasterWriter implements io.Writer and forwards each write call
// to the LogBroadcaster so WebSocket clients receive output in real time.
// It also accumulates the full log for storage in the database.
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
