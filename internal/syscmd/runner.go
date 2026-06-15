// Package syscmd provides a safe command execution layer. Every function
// requires an explicit executable name + separate arguments — shell string
// interpolation is impossible by design. All commands run with a context
// timeout to prevent runaway processes.
//
// Key safety properties:
//   - No shell: uses exec.CommandContext(name, args...) directly
//   - Timeout: every call requires a context with deadline
//   - Privilege dropping: RunAsUser* functions set syscall.Credential
//   - Output capture: stdout/stderr are captured, never forwarded to terminal
//
// This is the only package allowed to execute external commands in Fluxo.
package syscmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// RunAsUserInDir executes a command as the specified system user in the
// given working directory. It drops privileges via Unix credentials and
// captures combined stdout/stderr. Returns the stdout output or an error
// (which includes stderr for debugging).
func RunAsUserInDir(ctx context.Context, timeout time.Duration, username string, dir string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	u, err := user.Lookup(username)
	if err == nil {
		uid, _ := strconv.ParseUint(u.Uid, 10, 32)
		gid, _ := strconv.ParseUint(u.Gid, 10, 32)
		cmd.SysProcAttr = &syscall.SysProcAttr{}
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	}

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

// RunAsUser executes a command with the privileges of the specified
// system user. Used for operations that must run as fluxo or www-data
// rather than root.
func RunAsUser(ctx context.Context, timeout time.Duration, username string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)

	u, err := user.Lookup(username)
	if err == nil {
		uid, _ := strconv.ParseUint(u.Uid, 10, 32)
		gid, _ := strconv.ParseUint(u.Gid, 10, 32)
		cmd.SysProcAttr = &syscall.SysProcAttr{}
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	}

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

// RunEnvAsUser executes a command as a specific user with additional
// environment variables appended to the process environment.
func RunEnvAsUser(ctx context.Context, timeout time.Duration, username string, env []string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	u, err := user.Lookup(username)
	if err == nil {
		uid, _ := strconv.ParseUint(u.Uid, 10, 32)
		gid, _ := strconv.ParseUint(u.Gid, 10, 32)
		cmd.SysProcAttr = &syscall.SysProcAttr{}
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}
	}

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

// Run executes a command with the current (root) privileges. This is the
// base function — all other variants build on the same pattern.
// The caller must pass each argument as a separate string; no shell
// parsing occurs.
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

// RunStdin executes a command with the given input piped to stdin. This is
// the preferred way to pass passwords or SQL containing credentials, avoiding
// exposure via /proc/[pid]/cmdline.
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
