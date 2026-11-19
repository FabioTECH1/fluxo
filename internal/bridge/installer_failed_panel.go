package bridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fluxo/internal/syscmd"
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
			return panelStopped(ctx)
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
	for _, check := range []func() error{authenticate, empty, authenticate, reset, verify} {
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}

func installerFailedPanelDomainEmpty(group string) error {
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
