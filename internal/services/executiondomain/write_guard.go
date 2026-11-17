package executiondomain

import (
	"errors"
	"fmt"
	"path/filepath"
)

// GuardBackendWrite is required before publishing any next-generation backend
// identity, including receipt/recovery paths. A release number or helper support
// integer cannot grant backend semantics absent from its actual role contract.
func GuardBackendWrite(dataDir, backend string, helper ReaderCapabilities) error {
	floor, err := currentWriteFloor(dataDir)
	if err != nil {
		return err
	}
	return validateBackendWriteContract(*floor, backend, helper)
}
func validateBackendWriteContract(floor ReaderFloor, backend string, helper ReaderCapabilities) error {
	if !roleContains(Capabilities(), "scheduler-backend-reader", backend) || !floorRoleContains(floor, "scheduler-backend-reader", backend) || !roleContains(helper, "scheduler-backend-reader", backend) {
		return fmt.Errorf("backend %q requires committed exact panel, floor and helper reader contracts", backend)
	}
	return RequireCapabilities(helper, ReaderCapabilities{Format: 1, SchedulerSupport: SchedulerReaderSupport, SystemTaskSupport: SystemTaskReaderSupport, SchedulerSchemas: []int{CurrentSchedulerSchema}, SystemTaskSchemas: []int{CurrentSystemTaskSchema}, Adapters: floor.Adapters, RoleContracts: map[string][]string{"scheduler-backend-reader": {backend}}})
}

func GuardAdapterWrite(dataDir, component string, id AdapterID) error {
	adapter, err := InvocationAdapter(id)
	if err != nil {
		return err
	}
	floor, err := currentWriteFloor(dataDir)
	if err != nil {
		return err
	}
	found := false
	for _, supported := range floor.Adapters {
		found = found || supported == adapter
	}
	if !found {
		return errors.New("selected execution adapter is not in the committed floor")
	}
	role := "scheduler-execute"
	if component == "system-task" {
		role = "system-task-execute"
	} else if component != "scheduler" {
		return errors.New("unknown execution authority role")
	}
	if !floorRoleContains(*floor, role, string(adapter)) {
		return errors.New("selected adapter lacks its committed caller role contract")
	}
	return nil
}

func currentWriteFloor(dataDir string) (*ReaderFloor, error) {
	if !filepath.IsAbs(dataDir) || filepath.Clean(dataDir) != dataDir {
		return nil, errors.New("execution contract guard requires canonical installed scope")
	}
	report, err := InspectCompatibility(dataDir)
	if err != nil {
		return nil, err
	}
	return validateCurrentWriteReport(report)
}

func validateCurrentWriteReport(report CompatibilityReport) (*ReaderFloor, error) {
	floor := report.Floor
	if floor == nil || floor.Phase != "committed" || floor.SchedulerSchema != CurrentSchedulerSchema || floor.SystemTaskSchema != CurrentSystemTaskSchema || floor.SchedulerSupport < SchedulerReaderSupport || floor.SystemTaskSupport < SystemTaskReaderSupport {
		return nil, errors.New("activate current reader-fenced cores before publishing new execution authority")
	}
	seen := map[string]bool{}
	for _, journal := range report.Journals {
		expected := CurrentSchedulerSchema
		if journal.Component == "system-task" {
			expected = CurrentSystemTaskSchema
		} else if journal.Component != "scheduler" {
			return nil, errors.New("unknown execution journal in contract write admission")
		}
		if seen[journal.Component] || journal.Schema != expected {
			return nil, errors.New("contract write requires both actual current cores")
		}
		seen[journal.Component] = true
	}
	if !seen["scheduler"] || !seen["system-task"] {
		return nil, errors.New("contract write requires both actual current cores")
	}
	return floor, nil
}
func roleContains(c ReaderCapabilities, role, contract string) bool {
	for _, supported := range c.RoleContracts[role] {
		if supported == contract {
			return true
		}
	}
	return false
}
func floorRoleContains(f ReaderFloor, role, contract string) bool {
	for _, supported := range f.RoleContracts[role] {
		if supported == contract {
			return true
		}
	}
	return false
}
