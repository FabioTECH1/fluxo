package maintenancetask

import (
	"context"
	"fluxo/internal/services/executiondomain"
	"path/filepath"
)

// RequiredSupport is independent of marketing versions and includes a prepared
// format floor. Losing state never permits an older recovery reader.
func RequiredSupport() (int, error) {
	f, err := executiondomain.ReadFloor("/var/lib/fluxo")
	if err != nil {
		return executiondomain.SystemTaskReaderSupport, err
	}
	if f != nil && f.SystemTaskSupport >= 2 {
		return f.SystemTaskSupport, nil
	}
	a, err := absent()
	if err != nil {
		return executiondomain.SystemTaskReaderSupport, err
	}
	if a {
		return 1, nil
	}
	j, err := read()
	if err != nil {
		return executiondomain.SystemTaskReaderSupport, err
	}
	if j.Schema == executiondomain.CurrentSystemTaskSchema {
		return executiondomain.SystemTaskReaderSupport, nil
	}
	if j.Schema == 2 {
		return 2, nil
	}
	return 1, nil
}

// Called only after namespace, immutable digest and exact registration trigger
// checks. A support integer alone does not authorize a narrowed profile policy.
func verifyHelperReaderContracts(ctx context.Context, path string) error {
	have, err := executiondomain.ReadHelperCapabilities(ctx, path, filepath.Base(path))
	if err != nil {
		return err
	}
	need := executiondomain.Capabilities()
	need.SchedulerSchemas = nil
	need.SchedulerSupport = 0
	need.RoleContracts = map[string][]string{"system-task-execute": need.RoleContracts["system-task-execute"], "system-task-profile": need.RoleContracts["system-task-profile"]}
	return executiondomain.RequireCapabilities(have, need)
}
