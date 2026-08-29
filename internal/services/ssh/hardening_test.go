package ssh

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const hardenedEffectiveConfiguration = `passwordauthentication no
kbdinteractiveauthentication no
pubkeyauthentication yes
permitrootlogin prohibit-password
`

func successfulSSHRunner(_ context.Context, _ time.Duration, name string, args ...string) (string, error) {
	if name == "sshd" {
		for _, argument := range args {
			if argument == "-T" {
				return hardenedEffectiveConfiguration, nil
			}
		}
		return "", nil
	}
	if name == "systemctl" {
		return "", nil
	}
	return "", errors.New("unexpected command")
}

func testHardeningEnvironment(t *testing.T, runner commandRunner) hardeningEnvironment {
	t.Helper()
	directory := t.TempDir()
	mainConfig := filepath.Join(directory, "sshd_config")
	if err := os.WriteFile(mainConfig, []byte("# test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return hardeningEnvironment{
		targetPath: filepath.Join(directory, "sshd_config.d", "00-fluxo-hardening.conf"),
		mainConfig: mainConfig,
		run:        runner,
		keyCount:   func() (int, error) { return 1, nil },
	}
}

func TestEnableAndDisableManagedHardening(t *testing.T) {
	environment := testHardeningEnvironment(t, successfulSSHRunner)
	status, err := enableHardening(context.Background(), environment)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Hardened || !status.Managed || status.PasswordLoginEnabled {
		t.Fatalf("unexpected hardened status: %+v", status)
	}
	content, err := os.ReadFile(environment.targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != managedHardeningConfig {
		t.Fatalf("managed policy = %q", content)
	}

	status, err = disableManagedHardening(context.Background(), environment)
	if err != nil {
		t.Fatal(err)
	}
	if status.Managed {
		t.Fatalf("managed policy still reported after removal: %+v", status)
	}
	if _, err := os.Stat(environment.targetPath); !os.IsNotExist(err) {
		t.Fatalf("managed policy still exists: %v", err)
	}
}

func TestEnableHardeningRollsBackWhenReloadFails(t *testing.T) {
	systemctlCalls := 0
	runner := func(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
		if name == "systemctl" {
			systemctlCalls++
			if systemctlCalls <= 2 {
				return "", errors.New("reload failed")
			}
			return "", nil
		}
		return successfulSSHRunner(ctx, timeout, name, args...)
	}
	environment := testHardeningEnvironment(t, runner)
	if _, err := enableHardening(context.Background(), environment); err == nil || !strings.Contains(err.Error(), "previous policy was restored and OpenSSH reloaded") {
		t.Fatalf("enable error = %v", err)
	}
	if systemctlCalls != 3 {
		t.Fatalf("expected failed primary reload and successful reconciliation, got %d systemctl calls", systemctlCalls)
	}
	if _, err := os.Stat(environment.targetPath); !os.IsNotExist(err) {
		t.Fatalf("failed activation left policy installed: %v", err)
	}
}

func TestEnableHardeningReportsUnreconciledRollback(t *testing.T) {
	runner := func(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
		if name == "systemctl" {
			return "", errors.New("reload failed")
		}
		return successfulSSHRunner(ctx, timeout, name, args...)
	}
	environment := testHardeningEnvironment(t, runner)
	if _, err := enableHardening(context.Background(), environment); err == nil || !strings.Contains(err.Error(), "could not be reconciled") {
		t.Fatalf("enable error = %v", err)
	}
	if _, err := os.Stat(environment.targetPath); !os.IsNotExist(err) {
		t.Fatalf("failed activation did not restore disk state: %v", err)
	}
}

func TestSSHStatusRequiresHardenedRemoteContexts(t *testing.T) {
	runner := func(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
		if name != "sshd" {
			return successfulSSHRunner(ctx, timeout, name, args...)
		}
		for _, argument := range args {
			if strings.Contains(argument, "addr=2001:db8::10") {
				return strings.Replace(hardenedEffectiveConfiguration, "passwordauthentication no", "passwordauthentication yes", 1), nil
			}
		}
		return hardenedEffectiveConfiguration, nil
	}
	status := inspectSecurityStatus(context.Background(), testHardeningEnvironment(t, runner))
	if !status.Available || status.Hardened || !status.PasswordLoginEnabled {
		t.Fatalf("unexpected status for divergent remote policy: %+v", status)
	}
}

func TestSSHStatusDoesNotTreatRootOrLocalPasswordAsFluxoFallback(t *testing.T) {
	runner := func(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
		if name != "sshd" {
			return successfulSSHRunner(ctx, timeout, name, args...)
		}
		for _, argument := range args {
			if strings.Contains(argument, "user=root") || strings.Contains(argument, "host=localhost") {
				return strings.Replace(hardenedEffectiveConfiguration, "passwordauthentication no", "passwordauthentication yes", 1), nil
			}
		}
		return hardenedEffectiveConfiguration, nil
	}
	status := inspectSecurityStatus(context.Background(), testHardeningEnvironment(t, runner))
	if status.PasswordLoginEnabled {
		t.Fatalf("root or local password policy was treated as a remote fluxo fallback: %+v", status)
	}
	if status.Hardened {
		t.Fatalf("divergent root or local policy was incorrectly reported as fully hardened: %+v", status)
	}
}

func TestStagedHardeningRejectsDivergentRemoteContext(t *testing.T) {
	runner := func(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
		if name != "sshd" {
			return successfulSSHRunner(ctx, timeout, name, args...)
		}
		for _, argument := range args {
			if strings.Contains(argument, "addr=203.0.113.10") {
				return strings.Replace(hardenedEffectiveConfiguration, "passwordauthentication no", "passwordauthentication yes", 1), nil
			}
		}
		return successfulSSHRunner(ctx, timeout, name, args...)
	}
	environment := testHardeningEnvironment(t, runner)
	if _, err := enableHardening(context.Background(), environment); err == nil || !strings.Contains(err.Error(), "remote IPv4 context") {
		t.Fatalf("enable error = %v", err)
	}
}

func TestDisableHardeningWaitsForKeyMutationLock(t *testing.T) {
	environment := testHardeningEnvironment(t, successfulSSHRunner)
	if _, err := enableHardening(context.Background(), environment); err != nil {
		t.Fatal(err)
	}
	keysMu.Lock()
	started := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		close(started)
		_, err := disableHardening(context.Background(), environment)
		finished <- err
	}()
	<-started
	select {
	case err := <-finished:
		keysMu.Unlock()
		t.Fatalf("disable bypassed key mutation lock: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	keysMu.Unlock()
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestEnableHardeningRefusesUnmanagedTarget(t *testing.T) {
	environment := testHardeningEnvironment(t, successfulSSHRunner)
	if err := os.MkdirAll(filepath.Dir(environment.targetPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(environment.targetPath, []byte("PasswordAuthentication yes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := enableHardening(context.Background(), environment); err == nil || !strings.Contains(err.Error(), "not managed by Fluxo") {
		t.Fatalf("enable error = %v", err)
	}
}
