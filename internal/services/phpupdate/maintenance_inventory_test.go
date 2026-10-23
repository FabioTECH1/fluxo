package phpupdate

import "testing"

func TestMaintenanceInventoryRefreshBindsContractAndControllerNotJobPIDs(t *testing.T) {
	unit := func() MaintenanceUnit {
		return MaintenanceUnit{Enabled: "disabled", Definition: map[string]string{"User": "fluxo", "ExecStart": "fixed"}, Runtime: MaintenanceRuntime{Name: "fluxo-daemon-1.service", MainPID: 10, Restarts: "0", PIDs: map[int]string{10: "boot:controller", 11: "boot:job"}}}
	}
	old, next := unit(), unit()
	next.Runtime.PIDs = map[int]string{10: "boot:controller", 12: "boot:late-child"}
	if !sameMaintenanceUnitExecution(old, next) {
		t.Fatal("valid job turnover changed service identity")
	}
	for _, change := range []func(*MaintenanceUnit){
		func(u *MaintenanceUnit) { u.Enabled = "enabled" },
		func(u *MaintenanceUnit) { u.Definition["User"] = "root" },
		func(u *MaintenanceUnit) { u.Runtime.MainPID = 11 },
		func(u *MaintenanceUnit) { u.Runtime.PIDs[10] = "boot:reused-pid" },
		func(u *MaintenanceUnit) { u.Runtime.Restarts = "1" },
		func(u *MaintenanceUnit) { u.Runtime.Natural = true },
	} {
		changed := unit()
		change(&changed)
		if sameMaintenanceUnitExecution(old, changed) {
			t.Fatal("changed qualified service identity accepted", changed)
		}
	}
	old.Runtime.Octane = &octaneProcesses{Backend: "swoole", MasterPID: 11, ManagerPID: 12}
	old.Runtime.PIDs[12] = "boot:manager"
	next = unit()
	next.Runtime.Octane = &octaneProcesses{Backend: "swoole", MasterPID: 11, ManagerPID: 12}
	next.Runtime.PIDs[12] = "boot:manager"
	if !sameMaintenanceUnitExecution(old, next) {
		t.Fatal("identical native serving identity rejected")
	}
	next.Runtime.PIDs[12] = "boot:reused-native-pid"
	if sameMaintenanceUnitExecution(old, next) {
		t.Fatal("native manager PID reuse accepted")
	}
	consumer := func(u MaintenanceUnit) []MaintenanceConsumer {
		return []MaintenanceConsumer{{Units: []MaintenanceUnit{u}}}
	}
	if SameMaintenanceExecution(consumer(old), consumer(next)) {
		t.Fatal("review accepted native PID reuse")
	}
	next.Runtime.PIDs[12] = "boot:manager"
	if !SameMaintenanceExecution(consumer(old), consumer(next)) {
		t.Fatal("review rejected unchanged native anchors")
	}
	next.Runtime.Active = !old.Runtime.Active
	if SameMaintenanceExecution(consumer(old), consumer(next)) {
		t.Fatal("review replaced original native execution intent")
	}
}
