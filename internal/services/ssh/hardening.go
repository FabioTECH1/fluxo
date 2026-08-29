package ssh

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"fluxo/internal/syscmd"
)

const managedHardeningConfig = `# Managed by Fluxo.
# Remove through Fluxo so the effective policy is validated before SSH reloads.
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes
PermitRootLogin prohibit-password
`

type SecurityStatus struct {
	Available                         bool   `json:"available"`
	PasswordAuthentication            string `json:"password_authentication"`
	KeyboardInteractiveAuthentication string `json:"keyboard_interactive_authentication"`
	PublicKeyAuthentication           string `json:"public_key_authentication"`
	PermitRootLogin                   string `json:"permit_root_login"`
	PasswordLoginEnabled              bool   `json:"password_login_enabled"`
	Hardened                          bool   `json:"hardened"`
	Managed                           bool   `json:"managed"`
	AuthorizedKeyCount                int    `json:"authorized_key_count"`
	AuthorizedKeysValid               bool   `json:"authorized_keys_valid"`
	CanHarden                         bool   `json:"can_harden"`
	Error                             string `json:"error,omitempty"`
}

type commandRunner func(context.Context, time.Duration, string, ...string) (string, error)

type hardeningEnvironment struct {
	targetPath string
	mainConfig string
	run        commandRunner
	keyCount   func() (int, error)
}

func defaultHardeningEnvironment() hardeningEnvironment {
	return hardeningEnvironment{
		targetPath: "/etc/ssh/sshd_config.d/00-fluxo-hardening.conf",
		mainConfig: "/etc/ssh/sshd_config",
		run:        syscmd.Run,
		keyCount:   authorizedKeyCount,
	}
}

var hardeningMu sync.Mutex

type sshPolicyContext struct {
	username    string
	description string
	connection  string
	remote      bool
}

var sshPolicyContexts = []sshPolicyContext{
	{username: "fluxo", description: "remote IPv4", connection: "user=fluxo,host=remote.invalid,addr=203.0.113.10", remote: true},
	{username: "fluxo", description: "remote IPv6", connection: "user=fluxo,host=remote.invalid,addr=2001:db8::10", remote: true},
	{username: "fluxo", description: "localhost", connection: "user=fluxo,host=localhost,addr=127.0.0.1"},
	{username: "fluxo", description: "localhost IPv6", connection: "user=fluxo,host=localhost,addr=::1"},
	{username: "root", description: "remote IPv4", connection: "user=root,host=remote.invalid,addr=203.0.113.10", remote: true},
	{username: "root", description: "remote IPv6", connection: "user=root,host=remote.invalid,addr=2001:db8::10", remote: true},
	{username: "root", description: "localhost", connection: "user=root,host=localhost,addr=127.0.0.1"},
	{username: "root", description: "localhost IPv6", connection: "user=root,host=localhost,addr=::1"},
}

func isManagedHardening(content []byte) bool {
	trimmed := strings.TrimSpace(string(content))
	return strings.HasPrefix(trimmed, "# Managed by Fluxo.") ||
		strings.HasPrefix(trimmed, "# Managed by the Fluxo installer.")
}

func readHardeningFile(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, false, errors.New("SSH hardening policy is not a regular file")
	}
	content, err := os.ReadFile(path)
	return content, true, err
}

func effectiveSetting(output, name string) string {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == name {
			return strings.ToLower(fields[1])
		}
	}
	return ""
}

func inspectSecurityStatus(ctx context.Context, environment hardeningEnvironment) SecurityStatus {
	status := SecurityStatus{}
	if count, err := environment.keyCount(); err != nil {
		status.Error = "Unable to validate the Fluxo authorized_keys file: " + err.Error()
	} else {
		status.AuthorizedKeyCount = count
		status.AuthorizedKeysValid = true
	}
	if content, exists, err := readHardeningFile(environment.targetPath); err == nil && exists {
		status.Managed = isManagedHardening(content)
	} else if err != nil && status.Error == "" {
		status.Error = "Unable to inspect Fluxo's SSH policy file"
	}
	status.Available = true
	status.Hardened = true
	for index, policyContext := range sshPolicyContexts {
		effective, err := environment.run(ctx, 15*time.Second, "sshd", "-T", "-C", policyContext.connection)
		if err != nil {
			status.Available = false
			status.Hardened = false
			if status.Error == "" {
				status.Error = "Unable to evaluate the effective OpenSSH configuration"
			}
			return status
		}
		passwordAuthentication := effectiveSetting(effective, "passwordauthentication")
		keyboardInteractive := effectiveSetting(effective, "kbdinteractiveauthentication")
		publicKeyAuthentication := effectiveSetting(effective, "pubkeyauthentication")
		permitRootLogin := effectiveSetting(effective, "permitrootlogin")
		if passwordAuthentication == "" || keyboardInteractive == "" || publicKeyAuthentication == "" {
			status.Available = false
			status.Hardened = false
			if status.Error == "" {
				status.Error = "OpenSSH did not report a complete effective authentication policy"
			}
			return status
		}
		if index == 0 {
			status.PasswordAuthentication = passwordAuthentication
			status.KeyboardInteractiveAuthentication = keyboardInteractive
			status.PublicKeyAuthentication = publicKeyAuthentication
		}
		if policyContext.username == "root" && status.PermitRootLogin == "" {
			status.PermitRootLogin = permitRootLogin
		}
		// Only a remote password path for the fluxo account can replace the
		// final fluxo public key. Root or localhost policy is not a safe
		// fallback for that account's remote access.
		if policyContext.username == "fluxo" && policyContext.remote &&
			(passwordAuthentication == "yes" || keyboardInteractive == "yes") {
			status.PasswordLoginEnabled = true
		}
		if passwordAuthentication != "no" || keyboardInteractive != "no" || publicKeyAuthentication != "yes" {
			status.Hardened = false
		}
	}
	status.CanHarden = status.Available && status.AuthorizedKeysValid && status.AuthorizedKeyCount > 0 && !status.Hardened
	return status
}

// GetSecurityStatus reports the effective SSH policy for the Fluxo system user.
func GetSecurityStatus(ctx context.Context) SecurityStatus {
	keysMu.Lock()
	defer keysMu.Unlock()
	return inspectSecurityStatus(ctx, defaultHardeningEnvironment())
}

func writeAtomicRootFile(path string, content []byte, mode os.FileMode) (resultErr error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("SSH configuration directory is not a secure directory")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".fluxo-sshd-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if resultErr != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func stagedConfiguration(environment hardeningEnvironment, candidatePath string) (string, error) {
	file, err := os.CreateTemp("", "fluxo-sshd-validation-")
	if err != nil {
		return "", err
	}
	path := file.Name()
	configuration := fmt.Sprintf("Include %s\nInclude %s\n", candidatePath, environment.mainConfig)
	if _, err := file.WriteString(configuration); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func validateStagedHardening(ctx context.Context, environment hardeningEnvironment, candidatePath string) error {
	validationPath, err := stagedConfiguration(environment, candidatePath)
	if err != nil {
		return err
	}
	defer os.Remove(validationPath)
	if _, err := environment.run(ctx, 15*time.Second, "sshd", "-t", "-f", validationPath); err != nil {
		return errors.New("OpenSSH rejected the staged hardening configuration")
	}
	for _, policyContext := range sshPolicyContexts {
		effective, err := environment.run(ctx, 15*time.Second, "sshd", "-T", "-f", validationPath,
			"-C", policyContext.connection)
		if err != nil {
			return errors.New("OpenSSH could not evaluate the staged hardening configuration")
		}
		if effectiveSetting(effective, "passwordauthentication") != "no" ||
			effectiveSetting(effective, "kbdinteractiveauthentication") != "no" ||
			effectiveSetting(effective, "pubkeyauthentication") != "yes" {
			return fmt.Errorf("the staged SSH policy does not enforce public-key-only authentication for %s in the %s context", policyContext.username, policyContext.description)
		}
	}
	return nil
}

func reloadSSH(ctx context.Context, environment hardeningEnvironment) error {
	if _, err := environment.run(ctx, 30*time.Second, "systemctl", "reload", "ssh"); err == nil {
		return nil
	}
	if _, err := environment.run(ctx, 30*time.Second, "systemctl", "reload", "sshd"); err == nil {
		return nil
	}
	return errors.New("OpenSSH could not be reloaded")
}

func restoreHardeningFile(path string, previous []byte, existed bool) error {
	if existed {
		return writeAtomicRootFile(path, previous, 0644)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func restoreAndReloadHardening(environment hardeningEnvironment, restore func() error, message, restoredMessage string) error {
	if err := restore(); err != nil {
		return fmt.Errorf("%s; the previous policy could not be restored: %w", message, err)
	}
	rollbackContext, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	if _, err := environment.run(rollbackContext, 15*time.Second, "sshd", "-t"); err != nil {
		return fmt.Errorf("%s; the previous policy was restored on disk but OpenSSH rejected it: %w", message, err)
	}
	if err := reloadSSH(rollbackContext, environment); err != nil {
		return fmt.Errorf("%s; the previous policy was restored on disk but OpenSSH could not be reconciled: %w", message, err)
	}
	return errors.New(message + "; " + restoredMessage)
}

func enableHardening(ctx context.Context, environment hardeningEnvironment) (SecurityStatus, error) {
	hardeningMu.Lock()
	defer hardeningMu.Unlock()
	count, err := environment.keyCount()
	if err != nil {
		return SecurityStatus{}, fmt.Errorf("validate Fluxo authorized keys: %w", err)
	}
	if count < 1 {
		return SecurityStatus{}, errors.New("add and test at least one SSH key for the fluxo user first")
	}
	previous, existed, readErr := readHardeningFile(environment.targetPath)
	if readErr != nil {
		return SecurityStatus{}, fmt.Errorf("read existing SSH policy: %w", readErr)
	}
	if existed && !isManagedHardening(previous) {
		return SecurityStatus{}, errors.New("refusing to overwrite an SSH policy file not managed by Fluxo")
	}
	if err := os.MkdirAll(filepath.Dir(environment.targetPath), 0755); err != nil {
		return SecurityStatus{}, err
	}
	directoryInfo, err := os.Lstat(filepath.Dir(environment.targetPath))
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode()&os.ModeSymlink != 0 {
		return SecurityStatus{}, errors.New("SSH configuration directory is not a secure directory")
	}
	candidate, err := os.CreateTemp(filepath.Dir(environment.targetPath), ".fluxo-sshd-candidate-")
	if err != nil {
		return SecurityStatus{}, err
	}
	candidatePath := candidate.Name()
	if _, err := candidate.WriteString(managedHardeningConfig); err != nil {
		_ = candidate.Close()
		_ = os.Remove(candidatePath)
		return SecurityStatus{}, err
	}
	if err := candidate.Chmod(0644); err != nil {
		_ = candidate.Close()
		_ = os.Remove(candidatePath)
		return SecurityStatus{}, err
	}
	if err := candidate.Close(); err != nil {
		_ = os.Remove(candidatePath)
		return SecurityStatus{}, err
	}
	defer os.Remove(candidatePath)
	if err := validateStagedHardening(ctx, environment, candidatePath); err != nil {
		return SecurityStatus{}, err
	}
	if err := writeAtomicRootFile(environment.targetPath, []byte(managedHardeningConfig), 0644); err != nil {
		return SecurityStatus{}, fmt.Errorf("install SSH hardening policy: %w", err)
	}
	rollback := func(message string) error {
		return restoreAndReloadHardening(environment, func() error {
			return restoreHardeningFile(environment.targetPath, previous, existed)
		}, message, "the previous policy was restored and OpenSSH reloaded")
	}
	if _, err := environment.run(ctx, 15*time.Second, "sshd", "-t"); err != nil {
		return SecurityStatus{}, rollback("OpenSSH rejected the installed policy")
	}
	status := inspectSecurityStatus(ctx, environment)
	if !status.Hardened {
		return SecurityStatus{}, rollback("the installed policy was not effective")
	}
	if err := reloadSSH(ctx, environment); err != nil {
		return SecurityStatus{}, rollback("OpenSSH reload failed")
	}
	return inspectSecurityStatus(ctx, environment), nil
}

// EnableHardening validates and atomically activates key-only SSH access.
func EnableHardening(ctx context.Context) (SecurityStatus, error) {
	if os.Getenv("FLUXO_ENV") != "prod" {
		return SecurityStatus{}, errors.New("SSH hardening is available only on a production Fluxo server")
	}
	keysMu.Lock()
	defer keysMu.Unlock()
	return enableHardening(ctx, defaultHardeningEnvironment())
}

func disableManagedHardening(ctx context.Context, environment hardeningEnvironment) (SecurityStatus, error) {
	hardeningMu.Lock()
	defer hardeningMu.Unlock()
	previous, exists, err := readHardeningFile(environment.targetPath)
	if err != nil {
		return SecurityStatus{}, err
	}
	if !exists {
		return inspectSecurityStatus(ctx, environment), nil
	}
	if !isManagedHardening(previous) {
		return SecurityStatus{}, errors.New("the effective SSH policy is not managed by Fluxo")
	}
	if err := os.Remove(environment.targetPath); err != nil {
		return SecurityStatus{}, err
	}
	rollback := func(message string) error {
		return restoreAndReloadHardening(environment, func() error {
			return writeAtomicRootFile(environment.targetPath, previous, 0644)
		}, message, "Fluxo hardening remains active and OpenSSH was reconciled")
	}
	if _, err := environment.run(ctx, 15*time.Second, "sshd", "-t"); err != nil {
		return SecurityStatus{}, rollback("OpenSSH rejected the restored server policy")
	}
	if err := reloadSSH(ctx, environment); err != nil {
		return SecurityStatus{}, rollback("OpenSSH reload failed")
	}
	return inspectSecurityStatus(ctx, environment), nil
}

// DisableManagedHardening removes only Fluxo's managed drop-in, revealing the
// underlying provider/server policy without forcing password login on.
func DisableManagedHardening(ctx context.Context) (SecurityStatus, error) {
	if os.Getenv("FLUXO_ENV") != "prod" {
		return SecurityStatus{}, errors.New("SSH hardening is available only on a production Fluxo server")
	}
	return disableHardening(ctx, defaultHardeningEnvironment())
}

func disableHardening(ctx context.Context, environment hardeningEnvironment) (SecurityStatus, error) {
	keysMu.Lock()
	defer keysMu.Unlock()
	return disableManagedHardening(ctx, environment)
}
