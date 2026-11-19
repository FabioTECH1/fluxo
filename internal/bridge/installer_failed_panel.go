package bridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fluxo/internal/syscmd"
	"golang.org/x/sys/unix"
)

func installerFailedRecoveryBound(v *installerExecutionIntent, image, version, installation string) bool {
	return v != nil && v.Phase == "pending" && v.Operation != "" && v.Plan != "" &&
		v.Image == image && v.Version == version && v.InstallationID == installation
}

// Resetting failed status never starts or stops a process. Only a durable exact
// installer operation may clear this bookkeeping after its plan/image and empty
// panel domain are authenticated. Ordinary failed installations remain refused.
func recoverInstallerFailedPanel(ctx context.Context, dataDir, image, service string, intent installerExecutionIntent) error {
	out, err := run(ctx, "/usr/bin/systemctl", "show", "fluxo.service", "--property=LoadState,ActiveState,MainPID,ControlPID,ControlGroup")
	if err != nil {
		return err
	}
	return installerFailedPanelTransition(properties(out), intent, service,
		func() error {
			if _, err := inspectInstallerRecoveryArchive(dataDir, intent); err != nil {
				return err
			}
			binding, active, err := installerPanelBindingState(ctx, dataDir, image, true)
			if err != nil {
				return err
			}
			if active || binding != intent.Service {
				return errors.New("installer recovery panel binding changed")
			}
			return nil
		},
		func() error { return installerFailedPanelDomainEmpty("/sys/fs/cgroup/system.slice/fluxo.service") },
		func() error {
			_, err := syscmd.Run(ctx, 15*time.Second, "/usr/bin/systemctl", "reset-failed", "fluxo.service")
			return err
		},
		func() error {
			binding, active, err := installerPanelBinding(ctx, dataDir, image)
			if err != nil {
				return err
			}
			if active || binding != intent.Service {
				return errors.New("installer recovery panel changed after resetting failed status")
			}
			if err := panelStopped(ctx); err != nil {
				return err
			}
			return installerFailedPanelDomainEmpty("/sys/fs/cgroup/system.slice/fluxo.service")
		})
}

func installerFailedPanelTransition(p map[string]string, intent installerExecutionIntent, service string, authenticate, empty, reset, verify func() error) error {
	if p["ActiveState"] != "failed" {
		return nil
	}
	if intent.Phase != "pending" || intent.Operation == "" || intent.Plan == "" || intent.Service != service ||
		p["LoadState"] != "loaded" || p["MainPID"] != "0" || p["ControlPID"] != "0" ||
		(p["ControlGroup"] != "" && p["ControlGroup"] != "/system.slice/fluxo.service") {
		return errors.New("failed panel is not an empty exact installer recovery; preserve its state")
	}
	for _, check := range []func() error{authenticate, empty, authenticate, empty, reset, verify} {
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}

func installerFailedPanelDomainEmpty(group string) error {
	root := filepath.Dir(filepath.Dir(group))
	if err := installerFailedPanelHierarchy(root); err != nil {
		return err
	}
	return installerPanelDomainEmpty(group)
}

func installerFailedPanelHierarchy(root string) error {
	// A missing leaf proves nothing if an ancestor or the unified mount is
	// hidden. Require the same complete cgroup2 view as the service manager.
	for _, path := range []string{root, filepath.Join(root, "system.slice")} {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("cannot inspect the panel cgroup hierarchy")
		}
	}
	var filesystem unix.Statfs_t
	if err := unix.Statfs(root, &filesystem); err != nil {
		return err
	}
	if uint64(filesystem.Type) != uint64(unix.CGROUP2_SUPER_MAGIC) {
		return errors.New("failed panel recovery requires the unified cgroup2 hierarchy")
	}
	self, err := os.Stat("/proc/self/ns/cgroup")
	if err != nil {
		return err
	}
	init, err := os.Stat("/proc/1/ns/cgroup")
	if err != nil {
		return err
	}
	if !os.SameFile(self, init) {
		return errors.New("failed panel recovery requires the service manager cgroup namespace")
	}
	raw, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	full := false
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 6 && strings.HasPrefix(fields[4], root+"/") {
			return errors.New("failed panel recovery refuses hidden cgroup submounts")
		}
		if len(fields) > 6 && fields[4] == root && fields[3] == "/" && strings.Contains(line, " - cgroup2 ") {
			full = true
		}
	}
	if !full {
		return errors.New("failed panel recovery cannot inspect the complete cgroup hierarchy")
	}
	return nil
}

func installerPanelDomainEmpty(group string) error {
	info, err := os.Lstat(group)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("unexpected failed panel execution domain")
	}
	raw, err := os.ReadFile(filepath.Join(group, "cgroup.events"))
	if err != nil {
		return err
	}
	seen := false
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "populated" {
			continue
		}
		if seen || len(fields) != 2 || fields[1] != "0" {
			return errors.New("wait for the failed panel execution domain to empty")
		}
		seen = true
	}
	if !seen {
		return errors.New("cannot establish an empty failed panel execution domain")
	}
	return nil
}
