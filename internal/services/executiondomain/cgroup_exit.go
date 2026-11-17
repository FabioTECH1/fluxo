// Package executiondomain describes the execution contracts shared by guarded
// Scheduler and maintenance invocations. It does not own admission or journals.
package executiondomain

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/syscmd"
)

// CgroupExitV1 identifies the existing systemd service contract. Its meaning is
// immutable: a future adapter must use another identity, not alter this one.
// Existing unversioned invocation records already have this contract; this
// extraction intentionally does not write a new journal format.
const CgroupExitV1 = "systemd-cgroup-exit-v1"

var ErrUnsupportedHost = errors.New("Automatic Scheduler maintenance needs systemd 250 or newer with cgroup v2")

// RequireCgroupExitV1 preserves the existing minimum capability check. Passing
// it establishes execution-domain capability, not distribution/package-profile
// qualification. In particular it does not qualify Ubuntu 26.04 by OS label.
func RequireCgroupExitV1(ctx context.Context) error {
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		return ErrUnsupportedHost
	}
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", "--property=Version", "--value")
	if err != nil {
		return err
	}
	if !hasCgroupExit(strings.TrimSpace(out)) {
		return ErrUnsupportedHost
	}
	return nil
}

func hasCgroupExit(version string) bool {
	end := 0
	for end < len(version) && version[end] >= '0' && version[end] <= '9' {
		end++
	}
	n, err := strconv.Atoi(version[:end])
	return err == nil && n >= 250
}

// CgroupExitV1Arguments returns a fresh argument slice so callers cannot change
// the contract used by another invocation. Identity, command, log destination,
// credentials and working directory remain the caller's checked policy.
func CgroupExitV1Arguments() []string {
	return []string{
		"--property=Type=exec", "--property=ExitType=cgroup", "--property=Restart=no",
		"--property=NoNewPrivileges=yes", "--property=ProtectControlGroups=yes", "--property=Delegate=no",
	}
}

// CgroupExitV1Properties is the corresponding loaded-unit contract. Transient
// is observed rather than passed to systemd-run. Callers must also validate
// their own complete command, identity, environment and admission evidence.
func CgroupExitV1Properties() map[string]string {
	return map[string]string{
		"Type": "exec", "ExitType": "cgroup", "Restart": "no",
		"Delegate": "no", "NoNewPrivileges": "yes", "ProtectControlGroups": "yes", "Transient": "yes",
	}
}
