package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"fluxo/internal/syscmd"
)

// PanelBeforeStop is called while the installer owns the exclusive lifecycle
// lease and the old panel still owns its pipes and PrivateTmp. In supported
// releases both synchronous Scheduler Run now routes retain their shared lease
// through completion. This certifies Scheduler-origin work, not arbitrary
// custom commands or backups outside that admission contract.
func PanelBeforeStop(ctx context.Context) (*panelProof, error) {
	out, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/systemctl", "show", "fluxo.service", "--property=MainPID,ControlGroup,Delegate,User,ExecStart,ActiveState")
	if err != nil {
		return nil, err
	}
	p := properties(out)
	pid, err := strconv.Atoi(p["MainPID"])
	if err != nil || pid <= 1 || p["ActiveState"] != "active" {
		return nil, nil
	}
	if p["ControlGroup"] != "/system.slice/fluxo.service" || p["Delegate"] != "no" || p["User"] != "root" || !strings.HasPrefix(p["ExecStart"], "{ path=/usr/local/bin/fluxo ; argv[]=/usr/local/bin/fluxo ;") {
		return nil, errors.New("Panel execution domain is custom; review Scheduler migration before upgrading")
	}
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return nil, err
	}
	if strings.Contains(string(raw), p["ControlGroup"]) {
		return nil, errors.New("Run the installer from SSH outside the Fluxo service")
	}
	key, err := startKey(pid)
	if err != nil {
		return nil, err
	}
	version, err := syscmd.Run(ctx, 5*time.Second, fmt.Sprintf("/proc/%d/exe", pid), "--version")
	if err != nil {
		return nil, err
	}
	version = strings.TrimSpace(version)
	qualified := version == "fluxo version 0.4.34" || version == "fluxo version 0.4.35"
	if version != "fluxo version 0.4.34" && version != "fluxo version 0.4.35" {
		support, err := syscmd.Run(ctx, 5*time.Second, fmt.Sprintf("/proc/%d/exe", pid), "--scheduler-support")
		qualified = err == nil && strings.TrimSpace(support) == "2"
	}
	for {
		pids, err := groupPIDs(p["ControlGroup"])
		if err != nil {
			return nil, err
		}
		current, err := startKey(pid)
		if err != nil || current != key {
			return nil, errors.New("Panel identity changed while draining earlier Scheduler work")
		}
		if len(pids) == 1 && pids[pid] {
			break
		}
		select {
		case <-ctx.Done():
			return nil, errors.New("Earlier panel commands are still running. Let them finish and retry the installer; the panel has not been stopped")
		case <-time.After(100 * time.Millisecond):
		}
	}
	if !qualified {
		// Preserve the ordinary upgrade path, without manufacturing evidence for
		// historical manual admission this binary cannot verify. A subsequent
		// --reinstall against the compatible running panel establishes the proof.
		fmt.Fprintln(os.Stderr, "Earlier Fluxo manual Scheduler admission cannot be certified. After upgrading, rerun the installer with --reinstall before automatic PHP maintenance.")
		return nil, nil
	}
	boot, err := bootID()
	if err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	hash, err := fileDigest(exe)
	if err != nil {
		return nil, err
	}
	return &panelProof{Boot: boot, Binary: hash}, nil
}

func SavePanelProof(ctx context.Context, proof *panelProof) error {
	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	if j.Upgrade == nil || j.Upgrade.PID != os.Getpid() {
		return errors.New("Panel proof requires its installer lease")
	}
	j.PanelProof = proof
	return save(j)
}

// Keep legacy Run now functioning if startup adoption could not finish, but
// invalidate the earlier panel boundary before any untracked application runs.
func InvalidateLegacyManual(ctx context.Context, branch string) error {
	if !branchRE.MatchString(branch) {
		return errors.New("Invalid Scheduler branch")
	}
	if err := initialize(ctx); err != nil {
		return err
	}
	unlock, err := lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	j, err := read()
	if err != nil {
		return err
	}
	if j.Upgrade != nil || j.Holds[branch] != "" {
		return ErrMaintenance
	}
	boot, err := bootID()
	if err != nil {
		return err
	}
	j.PanelProof = nil
	j.Legacy[branch] = boot
	return save(j)
}
