// Package syscmd provides safe command execution via exec.CommandContext with no shell strings.
package syscmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ResolveCredential returns the operating-system credentials for a requested
// user. User-scoped commands must fail closed: silently falling back to the
// Fluxo daemon's credentials would turn an account lookup problem into a
// privilege change. Callers that specifically require an unprivileged account
// must additionally reject UID 0.
func ResolveCredential(username string) (*syscall.Credential, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return nil, fmt.Errorf("look up command user %q: %w", username, err)
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("parse UID for command user %q: %w", username, err)
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("parse GID for command user %q: %w", username, err)
	}
	return &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}, nil
}

// userEnvironment returns the daemon environment with identity and cache paths
// belonging to the daemon user removed, then adds the target user's identity.
// Changing UID/GID alone does not update HOME; without this, unprivileged
// commands such as npm can try to read and write root's profile and cache.
func userEnvironment(username string, additional []string) ([]string, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return nil, fmt.Errorf("look up command user %q: %w", username, err)
	}
	if u.HomeDir == "" {
		return nil, fmt.Errorf("command user %q has no home directory", username)
	}

	userScopedKeys := map[string]bool{
		"HOME": true, "USER": true, "LOGNAME": true,
		"XDG_CACHE_HOME": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true,
		"NPM_CONFIG_CACHE": true, "npm_config_cache": true,
		"YARN_CACHE_FOLDER": true, "PNPM_HOME": true, "COREPACK_HOME": true,
	}
	for _, entry := range additional {
		if key, _, ok := strings.Cut(entry, "="); ok {
			userScopedKeys[key] = true
		}
	}

	env := make([]string, 0, len(os.Environ())+len(additional)+3)
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || userScopedKeys[key] {
			continue
		}
		env = append(env, entry)
	}
	env = append(env,
		"HOME="+u.HomeDir,
		"USER="+username,
		"LOGNAME="+username,
	)
	env = append(env, additional...)
	return env, nil
}

// RunAsUserInDir executes a command as the specified user in the given working directory.
func RunAsUserInDir(ctx context.Context, timeout time.Duration, username string, dir string, name string, args ...string) (string, error) {
	return RunEnvAsUserInDir(ctx, timeout, username, dir, nil, name, args...)
}

// RunEnvAsUserInDir executes a command as the specified user in the given
// working directory with additional environment variables.
func RunEnvAsUserInDir(ctx context.Context, timeout time.Duration, username string, dir string, env []string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	credential, err := ResolveCredential(username)
	if err != nil {
		return "", err
	}
	commandEnv, err := userEnvironment(username, env)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = commandEnv
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: credential}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command timed out: %w", err)
		}
		return "", fmt.Errorf("command failed: %w\nStderr: %s\nStdout: %s", err, stderr.String(), stdout.String())
	}

	return stdout.String(), nil
}

// RunAsUserInDirStreaming executes a command as the specified user and streams
// combined stdout/stderr to writer while retaining the full captured output.
func RunAsUserInDirStreaming(ctx context.Context, timeout time.Duration, username string, dir string, writer io.Writer, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	credential, err := ResolveCredential(username)
	if err != nil {
		return "", err
	}
	env, err := userEnvironment(username, nil)
	if err != nil {
		return "", err
	}
	if writer == nil {
		writer = io.Discard
	}

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: credential, Setpgid: true}

	capture := &streamCapture{writer: writer}
	cmd.Stdout = capture
	cmd.Stderr = capture

	err = cmd.Run()
	output := capture.String()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			return output, fmt.Errorf("command timed out: %w", err)
		}
		return output, fmt.Errorf("command failed: %w", err)
	}

	return output, nil
}

// RunAsUser executes a command as the specified user with root's working directory.
func RunAsUser(ctx context.Context, timeout time.Duration, username string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	credential, err := ResolveCredential(username)
	if err != nil {
		return "", err
	}
	env, err := userEnvironment(username, nil)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: credential}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command timed out: %w", err)
		}
		return "", fmt.Errorf("command failed: %w\nStderr: %s\nStdout: %s", err, stderr.String(), stdout.String())
	}

	return stdout.String(), nil
}

type streamCapture struct {
	writer io.Writer
	buffer bytes.Buffer
	mu     sync.Mutex
}

func (c *streamCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buffer.Write(p)
	if _, err := c.writer.Write(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *streamCapture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buffer.String()
}

// RunEnvAsUser executes a command as the specified user with additional environment variables.
func RunEnvAsUser(ctx context.Context, timeout time.Duration, username string, env []string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	credential, err := ResolveCredential(username)
	if err != nil {
		return "", err
	}
	commandEnv, err := userEnvironment(username, env)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = commandEnv
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: credential}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command timed out: %w", err)
		}
		return "", fmt.Errorf("command failed: %w\nStderr: %s\nStdout: %s", err, stderr.String(), stdout.String())
	}

	return stdout.String(), nil
}

// Run executes a command with current (root) privileges. All arguments must be separate strings.
func Run(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command timed out: %w", err)
		}
		return "", fmt.Errorf("command failed: %w\nStderr: %s\nStdout: %s", err, stderr.String(), stdout.String())
	}

	return stdout.String(), nil
}

// RunEnv executes a command with additional environment variables.
func RunEnv(ctx context.Context, timeout time.Duration, env []string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
		return nil
	}
	cmd.WaitDelay = 2 * time.Second
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command timed out: %w\nStderr: %s\nStdout: %s", err, stderr.String(), stdout.String())
		}
		return "", fmt.Errorf("command failed: %w\nStderr: %s\nStdout: %s", err, stderr.String(), stdout.String())
	}

	return stdout.String(), nil
}

// RunStdin executes a command with input piped to stdin, hiding credentials from /proc/cmdline.
func RunStdin(ctx context.Context, timeout time.Duration, stdin string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command timed out: %w", err)
		}
		return "", fmt.Errorf("command failed: %w\nStderr: %s\nStdout: %s", err, stderr.String(), stdout.String())
	}

	return stdout.String(), nil
}

// RunToWriter executes a command with current privileges and streams stdout to writer.
// It is intended for large outputs such as database dumps that must not be buffered in memory.
func RunToWriter(ctx context.Context, timeout time.Duration, writer io.Writer, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stdout = writer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("command timed out: %w", err)
		}
		return fmt.Errorf("command failed: %w\nStderr: %s", err, stderr.String())
	}
	return nil
}
