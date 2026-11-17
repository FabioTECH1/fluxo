package scheduler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fluxo/internal/services/executiondomain"
)

func schedulerHierarchyEmpty(unit string) error {
	pids, err := groupPIDs("/system.slice/" + unit)
	if err != nil {
		return err
	}
	if len(pids) != 0 {
		return errors.New("Scheduler descendants must finish naturally")
	}
	raw, err := os.ReadFile(filepath.Join("/sys/fs/cgroup/system.slice", unit, "pids.current"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil || n != 0 {
		return errors.New("Scheduler hierarchy is not naturally empty")
	}
	return nil
}
func retireEmptySchedulerUnit(ctx context.Context, v invocation, p map[string]string) error {
	if !v.Started || !executiondomain.UnitQuiescent(v.Adapter, p) {
		return errors.New("Scheduler authority or execution metadata is not quiescent")
	}
	return executiondomain.StopNaturallyEmpty(ctx, v.Adapter, v.Unit, func() error { return schedulerHierarchyEmpty(v.Unit) })
}
