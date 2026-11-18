package bridge

import (
	"errors"
	"reflect"

	"fluxo/internal/services/executiondomain"
)

// Archive interpretation is independent of the currently compiled readers.
// These are the literal contracts published by the corresponding generations.
func activationContract(format int) (executiondomain.ReaderCapabilities, error) {
	c := executiondomain.ReaderCapabilities{Format: 1, Activation: true,
		SchedulerSupport: 5, SystemTaskSupport: 3,
		SchedulerSchemas: []int{1, 2, 3, 4}, SystemTaskSchemas: []int{1, 2, 3},
		Adapters: []executiondomain.AdapterID{"systemd-cgroup-exit-v1", "systemd-persistent-v1"},
		RoleContracts: map[string][]string{
			"scheduler-execute":          {"systemd-cgroup-exit-v1", "systemd-persistent-v1"},
			"system-task-execute":        {"systemd-cgroup-exit-v1", "systemd-persistent-v1"},
			"system-task-profile":        {"apt-a0-noqueue-v3"},
			"system-task-profile-reader": {"legacy-host-inputs-v1", "apt-a0-host-inputs-v2", "apt-a0-noqueue-v3"},
			"scheduler-backend-reader":   {"ubuntu-noble-cron-v1", "cronie-noble-1", "ubuntu-jammy-cron-v1", "ubuntu-resolute-cron-v1"},
		}}
	if format == 2 {
		return c, nil
	}
	if format != 3 && format != 4 {
		return c, errors.New("unknown activation reader generation")
	}
	c.SchedulerSupport = 6
	c.SchedulerSchemas = append(c.SchedulerSchemas, 5)
	c.RoleContracts["scheduler-loader-reader"] = []string{"exact-entry-loader-epoch-v1"}
	c.RoleContracts["scheduler-execute"] = append(c.RoleContracts["scheduler-execute"], "exact-entry-loader-admission-v1", "scheduler-bootstrap-drop-exec-v1")
	if format == 4 {
		c.SchedulerSupport = 7
		c.RoleContracts["scheduler-historical-retirement"] = []string{"fenced-historical-retirement-v1"}
	}
	return c, nil
}

func activationFloorContractMatches(f executiondomain.ReaderFloor, format int) bool {
	c, err := activationContract(format)
	return err == nil && f.SchedulerSupport == c.SchedulerSupport && f.SystemTaskSupport == c.SystemTaskSupport &&
		reflect.DeepEqual(f.Adapters, c.Adapters) && reflect.DeepEqual(f.RoleContracts, c.RoleContracts)
}

func activationTargetFloor(p executionActivation, planBytes []byte, phase string) (executiondomain.ReaderFloor, error) {
	c, err := activationPlanContract(p)
	if err != nil {
		return executiondomain.ReaderFloor{}, err
	}
	s, t, err := activationTargetSchemas(p)
	if err != nil {
		return executiondomain.ReaderFloor{}, err
	}
	return executiondomain.ReaderFloor{Format: 1, Phase: phase, SchedulerSupport: c.SchedulerSupport, SystemTaskSupport: c.SystemTaskSupport,
		SchedulerSchema: s, SystemTaskSchema: t, Adapters: c.Adapters, RoleContracts: c.RoleContracts,
		ActivationID: p.ID, InstallationID: p.InstallationID, BinarySHA256: p.BinarySHA256, PlanSHA256: digest(planBytes)}, nil
}

func activationPlanContract(p executionActivation) (executiondomain.ReaderCapabilities, error) {
	if p.Format == 4 && taskBackendTransitionKind(p.Kind) {
		source, err := executiondomain.ParseReaderFloor(p.SourceFloor)
		if err != nil {
			return executiondomain.ReaderCapabilities{}, err
		}
		if err = validateTaskRequalificationSourceFloor(source); err != nil {
			return executiondomain.ReaderCapabilities{}, err
		}
		if source.SchedulerSupport == 6 {
			return activationContract(3)
		}
		return activationContract(4)
	}
	return activationContract(p.Format)
}
func activationPlanFloorContractMatches(f executiondomain.ReaderFloor, p executionActivation) bool {
	c, err := activationPlanContract(p)
	return err == nil && f.SchedulerSupport == c.SchedulerSupport && f.SystemTaskSupport == c.SystemTaskSupport && reflect.DeepEqual(f.Adapters, c.Adapters) && reflect.DeepEqual(f.RoleContracts, c.RoleContracts)
}
