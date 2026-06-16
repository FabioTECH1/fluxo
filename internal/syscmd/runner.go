// Package syscmd provides safe command execution via exec.CommandContext with no shell strings.
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

// RunAsUserInDir executes a command as the specified user in the given working directory.
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

// RunAsUser executes a command as the specified user with root's working directory.
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

// RunEnvAsUser executes a command as the specified user with additional environment variables.
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
