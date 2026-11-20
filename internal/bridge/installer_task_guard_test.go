package bridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/executiondomain/testfixture"
	"fluxo/internal/services/maintenancetask"
	"fluxo/internal/services/scheduler"
)

func TestHistoricalGuardCrashBoundaries(t *testing.T) {
	for _, kind := range []string{activationFresh, activationHistoricalRetirement, activationInstallerHistoricalTasks, activationInstallerHistoricalCombined} {
		t.Run(kind, func(t *testing.T) {
			if !testfixture.InRoot(t, "../services/maintenancetask/testdata") {
				return
			}
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			const data = "/var/lib/fluxo"
			p, image := stageHistoricalTaskUnitArchive(t, kind)
			empty := installerExecutionIntent{Format: 1, Image: p.BinarySHA256, InstallationID: p.InstallationID, Version: p.Version, Phase: "pending", Service: strings.Repeat("c", 64), WasActive: true}
			raw, err := json.Marshal(empty)
			must(err)

			stage := installerStagedArchive(data, p.ID)
			request := activationRequest{InstallerPrepared: func(executionActivation, []byte) error { return nil }, InstallerIntentSHA256: digest(raw)}
			must(archiveActivationSources(stage, &p, request))
			if p.InstallerTaskGuard != digest(raw) || p.Kind != kind || installerHistoricalTaskKind(kind) != (len(p.HistoricalTaskRetirements) > 0) {
				t.Fatal("incorrect guard or retirement authority")
			}
			must(validateActivationPlan(p))
			must(validateActivationSources(stage, p))
			must(writeActivationJSON(stage+"/plan.json", p))
			raw, err = json.Marshal(p)
			must(err)
			want := empty
			want.Operation, want.Plan = p.ID, digest(raw)
			pointer := filepath.Join(data, installerExecutionRecord)
			must(writeActivationJSON(pointer, empty))
			// Before the atomic anchor publication this stage is unowned.
			_, adopted, err := guardedInstallerIntent(data, &empty)
			must(err)
			if adopted {
				t.Fatal("adopted an unpointed stage")
			}
			must(installTaskGuard(taskInitAnchor, p.ID, want.Plan, p.InstallerTaskGuard))
			// Crash before updating the ordinary installer pointer. A helper
			// which already passed its first read has this same 32-byte read
			// inside its flock, before finishInitialization accepts ready.
			if _, err := readProtected(taskInitAnchor, 32); err == nil {
				t.Fatal("legacy post-flock anchor read passed")
			}
			got, adopted, err := guardedInstallerIntent(data, &empty)
			must(err)
			if !adopted || *got != want {
				t.Fatal("lost staged operation")
			}
			for _, mutate := range []func(*installerExecutionIntent){
				func(i *installerExecutionIntent) { i.Service = strings.Repeat("a", 64) },
				func(i *installerExecutionIntent) { i.WasActive = false },
				func(i *installerExecutionIntent) { i.InstallationID = strings.Repeat("e", 32) },
				func(i *installerExecutionIntent) { i.Image = strings.Repeat("a", 64) },
			} {
				other := empty
				mutate(&other)
				if _, _, err := guardedInstallerIntent(data, &other); err == nil {
					t.Fatal("adopted unrelated pending intent")
				}
			}
			backup := filepath.Join(stage, "before-0")
			source, err := readProtected(backup, 32<<20)
			must(err)
			must(writeActivationFile(backup, []byte("changed"), 0600))
			if _, _, err := guardedInstallerIntent(data, &empty); err == nil {
				t.Fatal("accepted altered original core backup")
			}
			must(writeActivationFile(backup, source, 0600))
			must(writeActivationFile(stage+"/image", []byte("foreign"), 0700))
			if _, _, err := guardedInstallerIntent(data, &empty); err == nil {
				t.Fatal("accepted altered staged image")
			}
			must(writeActivationFile(stage+"/image", image, 0700))
			must(writeActivationFile(stage+"/plan.json", append(raw, ' '), 0600))
			if _, _, err := guardedInstallerIntent(data, &empty); err == nil {
				t.Fatal("accepted altered staged plan")
			}
			must(writeActivationFile(stage+"/plan.json", raw, 0600))
			must(writeActivationJSON(pointer, got))
			must(prepareInstallerRecovery(data, *got))
			checkGuard := func() {
				t.Helper()
				if _, err := readProtected(taskInitAnchor, 32); err == nil {
					t.Fatal("legacy helper can enter")
				}
				aux, err := activationAuxiliary()
				must(err)
				must(guardedAuxiliary(p, want.Plan, aux))
				if err := guardedAuxiliary(p, want.Plan, map[string]string{taskInitAnchor: digest([]byte("1\n"))}); err == nil {
					t.Fatal("lost guard accepted with original task core")
				}
				if err := releaseInstallerTaskGuard(data, p, raw); err == nil {
					t.Fatal("released guard before exact task core")
				}
			}
			checkGuard()
			for _, f := range p.Files {
				if f.Image {
					must(applyActivationFile(f, image))
					checkGuard()
				}
			}
			for _, f := range p.Files {
				if f.Path == "/var/lib/fluxo-scheduler/state.json" {
					must(applyActivationFile(f, image))
					checkGuard()
				}
			}
			for _, f := range p.Files {
				if f.Path == "/var/lib/fluxo-system-tasks/state.json" {
					must(applyActivationFile(f, image))
				}
			}
			if err := releaseInstallerTaskGuard(data, p, raw); err == nil {
				t.Fatal("exact task3 core opened ordinary gate before prepared floor")
			}
			// The task3 CLI preflight rejects both the guarded floorless cut and
			// the restored-anchor cut with a prepared floor. Only commit admits it.
			if _, err = executiondomain.InspectCompatibility(data); err == nil {
				t.Fatal("ordinary entry admitted before prepared floor")
			}
			floor, err := activationTargetFloor(p, raw, "prepared")
			must(err)
			must(writeActivationJSON(filepath.Join(data, "execution-readers.json"), floor))
			must(releaseInstallerTaskGuard(data, p, raw))
			must(releaseInstallerTaskGuard(data, p, raw))
			aux, err := activationAuxiliary()
			must(err)
			must(guardedAuxiliary(p, want.Plan, aux))
			if _, err = executiondomain.InspectCompatibility(data); err == nil {
				t.Fatal("ordinary task3 admitted with prepared floor")
			}
			if err = executiondomain.GuardAdapterWrite(data, "system-task", executiondomain.CgroupExitV1); err == nil {
				t.Fatal("task3 dispatch admitted with prepared floor")
			}
			for _, f := range p.Files {
				if f.Cron {
					must(applyActivationFile(f, image))
				}
			}
			floor.Phase = "committed"
			must(writeActivationJSON(filepath.Join(data, "execution-readers.json"), floor))
			_, err = executiondomain.InspectCompatibility(data)
			must(err)
			must(executiondomain.GuardAdapterWrite(data, "system-task", executiondomain.CgroupExitV1))
			after, err := readProtected(taskInitAnchor, 32)
			must(err)
			if !bytes.Equal(after, []byte("1\n")) {
				t.Fatal("lost original initialization proof")
			}
		})
	}
}

func TestFloorlessWithdrawalNamespaceAcrossCrash(t *testing.T) {
	for _, stage := range []bool{true, false} {
		root := t.TempDir()
		parent := filepath.Join(root, "execution-activations")
		retired := filepath.Join(root, "retired")
		src := filepath.Join(root, "staged")
		if !stage {
			src = filepath.Join(parent, "operation")
		}
		if err := os.MkdirAll(src, 0700); err != nil {
			t.Fatal(err)
		}
		if err := checkHistoricalArchiveNamespace(parent, src); err != nil {
			t.Fatal(err)
		}
		if err := withdrawalRename(src, retired); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(parent, "foreign"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := finishHistoricalArchiveMove(parent, retired); err == nil {
			t.Fatal("removed unrelated archive")
		}
		if err := os.Remove(filepath.Join(parent, "foreign")); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := finishHistoricalArchiveMove(parent, retired); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := os.Lstat(parent); !os.IsNotExist(err) {
			t.Fatal("floorless authority namespace survived")
		}
		if _, err := os.Stat(retired); err != nil {
			t.Fatal(err)
		}
	}
}

// File-level no-write evidence is independent of the live host qualification
// and per-invocation receipt proof exercised by the task service tests.
func TestHistoricalWithdrawalDiskProjections(t *testing.T) {
	if !testfixture.InRoot(t, "../services/maintenancetask/testdata") {
		return
	}
	const data = "/var/lib/fluxo"
	p, image := stageHistoricalTaskUnitArchive(t, activationInstallerHistoricalCombined)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(p)
	must(err)
	intent := installerExecutionIntent{Format: 1, Operation: p.ID, Plan: digest(raw), Image: p.BinarySHA256, Version: p.Version, InstallationID: p.InstallationID}
	dir := installerStagedArchive(data, p.ID)
	projection := func() error {
		var seen []withdrawalObservation
		_, err := readWithdrawalArchive(dir, intent, true, &seen)
		return err
	}
	// This fixture has no live invocation: a byte-identical task source must
	// take exact replay. Model distinct bytes only for testing the outer
	// projection; they still require independent semantic proof to withdraw.
	if projection() == nil {
		t.Fatal("exact source admitted for historical withdrawal")
	}
	const task = "/var/lib/fluxo-system-tasks/state.json"
	src, err := readProtected(task, 8<<20)
	must(err)
	must(writeActivationFile(task, append(src, ' '), 0600))
	must(projection())
	must(prepareInstallerRecovery(data, intent))
	dir = filepath.Join(data, "execution-activations", p.ID)
	must(projection())
	for _, f := range p.Files {
		before, e := os.ReadFile(f.Path)
		if e != nil && !os.IsNotExist(e) {
			t.Fatal(e)
		}
		must(applyHistoricalTestTarget(f, image))
		if projection() == nil {
			t.Fatalf("accepted published %s", f.Path)
		}
		if os.IsNotExist(e) {
			must(os.Remove(f.Path))
		} else {
			must(writeActivationFile(f.Path, before, os.FileMode(f.Mode)))
		}
	}
	must(projection())
	if !historicalFirstWriteDistinct(p) {
		t.Fatal("missed distinct first helper")
	}
	q := p
	q.Files = append([]executiondomain.ActivationFile(nil), p.Files...)
	for i, f := range q.Files {
		if f.Image {
			q.Files[i].Before = p.BinarySHA256
			break
		}
	}
	if historicalFirstWriteDistinct(q) {
		t.Fatal("accepted indistinguishable first write")
	}
	must(writeActivationFile(dir+"/before-0", []byte("altered"), 0600))
	if projection() == nil {
		t.Fatal("accepted altered backup")
	}
	// Bind the source database and every pre-existing initialization anchor.
	dbpath := filepath.Join(data, "fluxo.db")
	must(writeActivationFile(dbpath, []byte("database"), 0600))
	p.Database, err = activationDatabaseHashes(data)
	must(err)
	aux, err := activationAuxiliary()
	must(err)
	p.Auxiliary = aux
	must(validateWithdrawalSources(p, p.InstallationID, p.Database, aux))
	must(writeActivationFile(dbpath, []byte("changed"), 0600))
	changed, err := activationDatabaseHashes(data)
	must(err)
	if validateWithdrawalSources(p, p.InstallationID, changed, aux) == nil || validateWithdrawalSources(p, "foreign", p.Database, aux) == nil {
		t.Fatal("accepted foreign database or identity")
	}
	must(writeActivationFile(taskInitAnchor, []byte("activation:foreign\n"), 0600))
	changed, err = activationAuxiliary()
	must(err)
	if validateWithdrawalSources(p, p.InstallationID, p.Database, changed) == nil {
		t.Fatal("accepted partial fence")
	}
}

func applyHistoricalTestTarget(f executiondomain.ActivationFile, image []byte) error {
	raw := f.Data
	if f.Image {
		raw = image
	}
	return writeActivationFile(f.Path, raw, os.FileMode(f.Mode))
}

// Run the actual locked task planner and archive-selection path for original
// absence and existing schemas, without constructing a retirement witness.
func TestInstallerGuardPlanningScope(t *testing.T) {
	if !testfixture.InRoot(t, "") {
		return
	}
	const base = "/var/lib/fluxo-system-tasks"
	const stage = "/var/lib/fluxo"
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.Mkdir(stage, 0700))
	for _, source := range []int{0, 1, 2, 3} {
		if source > 0 {
			must(os.MkdirAll(base, 0700))
			must(writeActivationFile(base+".initialized", []byte("1\n"), 0600))
			must(writeActivationFile(base+".lock", []byte("ready\n"), 0600))
			must(writeActivationFile(base+"/state.json", []byte(fmt.Sprintf(`{"schema":%d,"entries":{},"invocations":{}}`, source)), 0600))
		}
		var proofs []scheduler.CronBackendRetirement
		files, err := maintenancetask.PlanInstallerHistoricalActivationLocked(t.Context(), strings.Repeat("a", 64), "1af605c7-82ca-4245-9fc5-1240cae7d8ab", &proofs)
		must(err)
		if len(proofs) != 0 {
			t.Fatal("invented retirement proof")
		}
		for _, kind := range []string{activationFresh, activationHistoricalRetirement, activationTaskHelperRefresh, activationInstallerTaskBackendRenewal} {
			for _, installer := range []bool{false, true} {
				p := executionActivation{Format: 4, Kind: kind, Files: files, Auxiliary: map[string]string{taskInitAnchor: digest([]byte("1\n"))}}
				r := activationRequest{}
				if installer {
					r.InstallerPrepared = func(executionActivation, []byte) error { return nil }
					r.InstallerIntentSHA256 = strings.Repeat("f", 64)
				}
				must(archiveActivationSources(stage, &p, r))
				want := installer && (kind == activationFresh || kind == activationHistoricalRetirement) && (source == 1 || source == 2)
				if (p.InstallerTaskGuard != "") != want {
					t.Fatalf("source %d kind %s installer %t guard %q", source, kind, installer, p.InstallerTaskGuard)
				}
			}
		}
	}
}
