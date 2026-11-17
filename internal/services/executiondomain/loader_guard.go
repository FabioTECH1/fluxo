package executiondomain

import "errors"

// Staged immutable protocol targets. These constants do not advertise or select
// producers; compiled Capabilities and current core readers must also support
// them before GuardLoaderWrite can grant publication authority.
const SchedulerLoaderSchema = 5
const SchedulerLoaderSupport = 6
const SchedulerLoaderReaderContract = "exact-entry-loader-epoch-v1"
const SchedulerLoaderExecutionContract = "exact-entry-loader-admission-v1"

func loaderWriterRequirements() ReaderCapabilities {
	return ReaderCapabilities{Format: 1, SchedulerSupport: SchedulerLoaderSupport, SystemTaskSupport: 3, SchedulerSchemas: []int{SchedulerLoaderSchema}, SystemTaskSchemas: []int{3}, RoleContracts: map[string][]string{
		"scheduler-loader-reader": {SchedulerLoaderReaderContract},
		"scheduler-execute":       {SchedulerLoaderExecutionContract, SchedulerBootstrapContract},
	}}
}

// GuardLoaderWrite must precede every new loader epoch/publication/receipt or
// recovery mutation. Exact actual helper capability inspection belongs to the
// caller; neither its integer flag nor the panel's own claims can replace it.
func GuardLoaderWrite(dataDir string, helper ReaderCapabilities) error {
	need := loaderWriterRequirements()
	if e := RequireCapabilities(Capabilities(), need); e != nil {
		return errors.New("loader publication remains disabled until compiled schema5/bootstrap readers are complete")
	}
	floor, e := currentWriteFloor(dataDir)
	if e != nil {
		return e
	}
	return validateLoaderWriteContract(*floor, helper, Capabilities())
}
func validateLoaderWriteContract(floor ReaderFloor, helper, compiled ReaderCapabilities) error {
	if floor.Phase != "committed" || floor.SchedulerSchema != SchedulerLoaderSchema || floor.SystemTaskSchema != 3 || floor.SchedulerSupport < SchedulerLoaderSupport || floor.SystemTaskSupport < 3 {
		return errors.New("loader writer requires committed schema5/task3 reader floor")
	}
	need := loaderWriterRequirements()
	need.Adapters = floor.Adapters
	if e := RequireCapabilities(compiled, need); e != nil {
		return e
	}
	haveFloor := ReaderCapabilities{Format: 1, SchedulerSupport: floor.SchedulerSupport, SystemTaskSupport: floor.SystemTaskSupport, SchedulerSchemas: []int{floor.SchedulerSchema}, SystemTaskSchemas: []int{floor.SystemTaskSchema}, Adapters: floor.Adapters, RoleContracts: floor.RoleContracts}
	if e := RequireCapabilities(haveFloor, need); e != nil {
		return e
	}
	return RequireCapabilities(helper, need)
}
