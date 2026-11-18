package maintenancetask

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/scheduler"
	"github.com/google/uuid"
)

func installerRenewalProjection(t *testing.T, original []byte, next journal) ([]byte, string, []executiondomain.ActivationFile) {
	t.Helper()
	var old journal
	if err := json.Unmarshal(original, &old); err != nil {
		t.Fatal(err)
	}
	intended, err := executiondomain.MergeActivationJSON(original, next)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[int]scheduler.CronBackendIdentity{}
	files := []executiondomain.ActivationFile{{Path: filepath.Join(stateRoot, "state.json"), Before: digest(original), Data: intended, Mode: 0600}}
	for id, r := range old.Entries {
		if !r.Enabled {
			continue
		}
		target := next.Entries[id]
		targets[id] = target.Backend
		if r != target {
			files = append(files, executiondomain.ActivationFile{Path: cronPath(id), Before: digest([]byte(cronContent(r))), Data: []byte(cronContent(target)), Cron: true, Mode: 0644})
		}
	}
	raw, _ := json.Marshal(targets)
	return intended, digest(raw), files
}

func TestInstallerRegistrationRenewalArchivesGenerationWithoutChangingManualMeaning(t *testing.T) {
	for _, backend := range []string{"ubuntu-resolute-cron-v1", "ubuntu-jammy-cron-v1", "cronie-noble-1", ""} {
		for _, acrossBoot := range []bool{false, true} {
			for _, received := range []bool{false, true} {
				original, _, _, _ := registrationRequalificationFixture(t)
				var old journal
				_ = json.Unmarshal(original, &old)
				r := old.Entries[9]
				r.Backend.Backend, r.Received = backend, received
				if backend == "cronie-noble-1" {
					r.Backend.Group = "/system.slice/cronie.service"
				}
				if backend == "" {
					r.Backend.Group, r.Backend.Configuration = "", ""
				}
				old.Entries[9] = r
				original, _ = json.Marshal(old)
				r.Backend.PID++
				r.Backend.Start = "900"
				r.Backend.GroupInode++
				if acrossBoot {
					r.Backend.Boot = uuid.NewString()
					r.Boot = r.Backend.Boot
				}
				r.Nonce, r.Received = uuid.NewString(), false
				old.Entries[9] = r
				intended, sha, files := installerRenewalProjection(t, original, old)
				if err := ValidateInstallerRegistrationRenewalPlan(original, intended, sha, files); err != nil {
					t.Fatal(backend, acrossBoot, received, err)
				}
				if ValidateRegistrationRequalificationPlan(original, intended, sha, files) == nil {
					t.Fatal("frozen manual operation acquired generation renewal")
				}
			}
		}
	}
}

func TestInstallerRegistrationRenewalRefusesUnrelatedAuthorityChanges(t *testing.T) {
	original, _, _, _ := registrationRequalificationFixture(t)
	var old journal
	_ = json.Unmarshal(original, &old)
	r := old.Entries[9]
	r.Backend.PID++
	r.Backend.Start = "900"
	r.Backend.GroupInode++
	r.Backend.Boot = uuid.NewString()
	r.Boot = r.Backend.Boot
	r.Nonce, r.Received = uuid.NewString(), false
	old.Entries[9] = r
	valid, _, _ := installerRenewalProjection(t, original, old)
	for name, change := range map[string]func(*journal){
		"provider":   func(j *journal) { r := j.Entries[9]; r.Backend.Backend = "ubuntu-jammy-cron-v1"; j.Entries[9] = r },
		"group":      func(j *journal) { r := j.Entries[9]; r.Backend.Group = "/system.slice/other.service"; j.Entries[9] = r },
		"wrong-boot": func(j *journal) { r := j.Entries[9]; r.Boot = uuid.NewString(); j.Entries[9] = r },
		"fake-ack":   func(j *journal) { r := j.Entries[9]; r.Received = true; j.Entries[9] = r },
		"same-nonce": func(j *journal) {
			var o journal
			_ = json.Unmarshal(original, &o)
			r := j.Entries[9]
			r.Nonce = o.Entries[9].Nonce
			j.Entries[9] = r
		},
		"helper": func(j *journal) {
			r := j.Entries[9]
			r.Helper = filepath.Join(stateRoot, "helpers", strings.Repeat("d", 64))
			j.Entries[9] = r
		},
		"command":            func(j *journal) { r := j.Entries[9]; r.Contract.Command = "/bin/true"; j.Entries[9] = r },
		"database":           func(j *journal) { r := j.Entries[9]; r.Database = "/tmp/fluxo.db"; j.Entries[9] = r },
		"identity":           func(j *journal) { r := j.Entries[9]; r.Identity = uuid.NewString(); j.Entries[9] = r },
		"predecessor":        func(j *journal) { r := j.Entries[9]; r.Predecessor = "replacement"; j.Entries[9] = r },
		"history":            func(j *journal) { j.Invocations["invented"] = invocation{Status: "completed"} },
		"extra-registration": func(j *journal) { j.Entries[11] = j.Entries[9] },
	} {
		t.Run(name, func(t *testing.T) {
			var next journal
			_ = json.Unmarshal(valid, &next)
			change(&next)
			intended, sha, files := installerRenewalProjection(t, original, next)
			if ValidateInstallerRegistrationRenewalPlan(original, intended, sha, files) == nil {
				t.Fatal("unrelated authority change accepted")
			}
		})
	}
}
