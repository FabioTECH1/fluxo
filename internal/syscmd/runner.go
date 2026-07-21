// Package syscmd provides safe command execution via exec.CommandContext with no shell strings.
package syscmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
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

// RunAsUserInDir executes a command as the specified user in the given working directory.
func RunAsUserInDir(ctx context.Context, timeout time.Duration, username string, dir string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	credential, err := ResolveCredential(username)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
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

// RunAsUser executes a command as the specified user with root's working directory.
func RunAsUser(ctx context.Context, timeout time.Duration, username string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	credential, err := ResolveCredential(username)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, name, args...)
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

// RunEnvAsUser executes a command as the specified user with additional environment variables.
func RunEnvAsUser(ctx context.Context, timeout time.Duration, username string, env []string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	credential, err := ResolveCredential(username)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
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
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

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
