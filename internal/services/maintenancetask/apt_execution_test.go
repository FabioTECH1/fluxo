package maintenancetask

import (
	"fluxo/internal/services/executiondomain"
	"reflect"
	"testing"
)

func TestAPTNoQueueAuthorityAndExactSolver(t *testing.T) {
	for _, version := range []string{"2.4.14", "2.8.3", "3.2.0", "2.4.15ubuntu0.1", "2.8.4ubuntu0.1", "3.2.1ubuntu0.1"} {
		solver := "internal"
		if version == "3.2.0" || version == "3.2.1ubuntu0.1" {
			solver = "3.0"
		}
		p := newProfile(PackageCleanup)
		p.External["package:apt"] = version
		p.External["apt-execution-profile"] = aptCleanupExecutionProfile
		p.External["apt-fixed-solver"] = solver
		args, err := cleanupAPTArguments(p)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"-o", "Debug::pkgAcqArchive::NoQueue=true", "--no-download", "-o", "APT::Solver=" + solver, "-o", "APT::Planner=internal"}
		if !reflect.DeepEqual(args, want) {
			t.Fatal("archive/solver fence changed", args)
		}
		p.External["apt-fixed-solver"] = "external"
		if _, err = cleanupAPTArguments(p); err == nil {
			t.Fatal("mismatched solver accepted")
		}
		p.External["apt-fixed-solver"] = solver
		for _, old := range []string{"", "legacy-host-inputs-v1", "apt-a0-host-inputs-v2"} {
			p.External["apt-execution-profile"] = old
			if _, err = cleanupAPTArguments(p); err == nil {
				t.Fatal("history profile granted V3 execution", old)
			}
		}
	}
	if _, err := cleanupAPTSolver("3.2.1"); err == nil {
		t.Fatal("unreviewed version accepted")
	}
}

func TestAPTProfileHelperExecutionAndHistoryAreSeparate(t *testing.T) {
	current := executiondomain.Capabilities()
	for _, old := range []string{"legacy-host-inputs-v1", "apt-a0-host-inputs-v2"} {
		history := false
		for _, reader := range current.RoleContracts["system-task-profile-reader"] {
			history = history || reader == old
		}
		if !history {
			t.Fatal("historical profile reader removed", old)
		}
		stale := executiondomain.Capabilities()
		stale.RoleContracts["system-task-profile"] = []string{old}
		if executiondomain.RequireCapabilities(stale, current) == nil {
			t.Fatal("old helper execution capability accepted", old)
		}
	}
}
