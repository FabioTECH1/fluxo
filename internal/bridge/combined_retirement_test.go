package bridge

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/executiondomain/testfixture"
	"fluxo/internal/services/scheduler"
)

// Unit fixture for the archive writer/ordinary reader boundary. Host
// qualification and fresh Cronie acknowledgements belong to native acceptance.
func TestCombinedRetirementUnitArchiveReopens(t *testing.T) {
	if !testfixture.InRoot(t, "../services/maintenancetask/testdata") {
		return
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string) []byte {
		t.Helper()
		raw, err := os.ReadFile(path)
		must(err)
		return raw
	}
	const dataDir = "/var/lib/fluxo"
	const schedulerDir = "/var/lib/fluxo-scheduler"
	const taskDir = "/var/lib/fluxo-system-tasks"
	const cronPath = "/etc/cron.d/fluxo-cron-9"
	p := historicalTaskArchiveFixture()
	p.Kind = activationInstallerHistoricalCombined
	image := []byte("unit fixture image")
	p.BinarySHA256 = digest(image)
	archive := filepath.Join(dataDir, "execution-activations", p.ID)
	for _, dir := range []string{archive, schedulerDir + "/helpers", taskDir + "/helpers"} {
		must(os.MkdirAll(dir, 0700))
	}
	for _, dir := range []string{schedulerDir, taskDir} {
		must(writeActivationFile(dir+".initialized", []byte("1\n"), 0600))
	}
	schedulerSource := []byte(`{"schema":2,"entries":{},"invocations":{},"holds":{},"released":{},"upgrade":null,"last_sweep":{},"panel_proof":null,
	"legacy":{"8.4":"083c861b-86fb-405d-9484-09eada7ce31c"},"migrations":{"8.4":{"host":null,"replacements":{},"deletions":{},"marker":"",
	"marker_phase":0,"marker_received":false,"previous_marker":"","certified":false,"reason":""}}}`)
	must(writeActivationFile(schedulerDir+"/state.json", schedulerSource, 0600))
	var err error
	p.HistoricalSources, err = scheduler.HistoricalActivationSourcesLocked()
	must(err)
	schedulerTarget, err := executiondomain.MergeActivationJSON(schedulerSource, map[string]any{
		"schema":                    5,
		"loader_epochs_established": false,
		"historical_retirement": map[string]string{
			"contract":      executiondomain.SchedulerHistoricalRetirementContract,
			"operation":     p.ID,
			"source_sha256": digest(schedulerSource),
			"phase":         "pending",
		},
	})
	must(err)

	taskSource := read("/inputs/v040-cronie-schema1.json")
	cronSource := read("/inputs/v040-cronie-schema1.cron")
	oldHelper := taskDir + "/helpers/" + strings.Repeat("b", 64)
	newHelper := taskDir + "/helpers/" + p.BinarySHA256
	const oldNonce = "283c861b-86fb-405d-9484-09eada7ce31c"
	const newNonce = "383c861b-86fb-405d-9484-09eada7ce31c"
	taskTarget, err := executiondomain.MergeActivationJSON(taskSource, map[string]any{
		"schema": 3,
		"entries": map[string]any{"9": map[string]any{
			"received":     false,
			"refresh_from": map[string]string{"helper": oldHelper, "nonce": oldNonce},
			"helper":       newHelper,
			"nonce":        newNonce,
			"backend":      p.HistoricalTaskRetirements[0].Current,
		}},
	})
	must(err)
	cronTarget := strings.NewReplacer(oldHelper, newHelper, oldNonce, newNonce).Replace(string(cronSource))
	must(writeActivationFile(taskDir+"/state.json", taskSource, 0600))
	must(writeActivationFile(cronPath, cronSource, 0644))
	p.Files = []executiondomain.ActivationFile{
		{Path: schedulerDir + "/state.json", Before: digest(schedulerSource), Data: schedulerTarget, Mode: 0600},
		{Path: taskDir + "/state.json", Before: digest(taskSource), Data: taskTarget, Mode: 0600},
		{Path: newHelper, Before: "absent", Image: true, Mode: 0700},
		{Path: schedulerDir + "/helpers/" + p.BinarySHA256, Before: "absent", Image: true, Mode: 0700},
		{Path: schedulerDir + "/dispatcher", Before: "absent", Image: true, Mode: 0700},
		{Path: cronPath, Before: digest(cronSource), Data: []byte(cronTarget), Cron: true, CronMTime: 1, Mode: 0644},
	}
	must(validateActivationPlan(p))
	for i, f := range p.Files {
		if f.Before != "absent" {
			must(writeActivationFile(filepath.Join(archive, fmt.Sprintf("before-%d", i)), read(f.Path), 0600))
		}
	}
	must(writeActivationFile(archive+"/image", image, 0700))
	must(writeActivationJSON(archive+"/plan.json", p))
	floor, err := activationTargetFloor(p, read(archive+"/plan.json"), "prepared")
	must(err)
	must(writeActivationJSON(dataDir+"/execution-readers.json", floor))
	for _, f := range p.Files {
		must(applyActivationFile(f, image))
	}
	floor.Phase = "committed"
	must(validateCommittedActivationArchive(dataDir, floor))
	must(writeActivationJSON(dataDir+"/execution-readers.json", floor))
	scheduler.SetDatabasePath(dataDir + "/fluxo.db")
	must(scheduler.Quiescent(t.Context()))

	changed := bytes.Replace(schedulerTarget, []byte(digest(schedulerSource)), []byte(strings.Repeat("0", 64)), 1)
	must(writeActivationFile(schedulerDir+"/state.json", changed, 0600))
	if err := scheduler.Quiescent(t.Context()); err == nil || !strings.Contains(err.Error(), "historical source digest differs") {
		t.Fatalf("changed source digest: %v", err)
	}
	must(writeActivationFile(schedulerDir+"/state.json", schedulerTarget, 0600))

	// Rebind the plan digest so the negative actually exercises kind admission.
	p.Kind = activationInstallerHistoricalTasks
	must(writeActivationJSON(archive+"/plan.json", p))
	floor.PlanSHA256 = digest(read(archive + "/plan.json"))
	must(writeActivationJSON(dataDir+"/execution-readers.json", floor))
	if err := scheduler.Quiescent(t.Context()); err == nil || !strings.Contains(err.Error(), "archive is not a historical retirement operation") {
		t.Fatalf("task-only archive substitution: %v", err)
	}
}
