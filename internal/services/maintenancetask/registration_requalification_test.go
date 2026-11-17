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

func registrationRequalificationFixture(t *testing.T) ([]byte, []byte, string, []executiondomain.ActivationFile) {
	t.Helper()
	c, ok := Qualify(9, 0, "0 0 * * 0", PackageCleanupCommand, "root")
	if !ok {
		t.Fatal("fixture")
	}
	boot := uuid.NewString()
	r := registration{Contract: c, Database: "/var/lib/fluxo/fluxo.db", Identity: uuid.NewString(), Nonce: uuid.NewString(), Helper: filepath.Join(stateRoot, "helpers", strings.Repeat("a", 64)), Enabled: true, Published: true, Received: true, Boot: boot, Predecessor: LegacyContent(c), Backend: scheduler.CronBackendIdentity{Backend: "ubuntu-resolute-cron-v1", Boot: boot, PID: 40, Start: "400", Group: "/system.slice/cron.service", GroupInode: 77, Configuration: strings.Repeat("b", 64)}}
	j := journal{Schema: 3, Entries: map[int]registration{9: r, 10: {Enabled: false}}, Invocations: map[string]invocation{}}
	original, _ := json.Marshal(j)
	before := digest([]byte(cronContent(r)))
	r.Backend.Configuration = strings.Repeat("c", 64)
	r.Nonce = uuid.NewString()
	r.Received = false
	j.Entries[9] = r
	intended, _ := executiondomain.MergeActivationJSON(original, j)
	targets, _ := json.Marshal(map[int]scheduler.CronBackendIdentity{9: r.Backend})
	files := []executiondomain.ActivationFile{{Path: filepath.Join(stateRoot, "state.json"), Before: digest(original), Data: intended, Mode: 0600}, {Path: cronPath(9), Before: before, Data: []byte(cronContent(r)), Mode: 0644, Cron: true}}
	return original, intended, digest(targets), files
}

func TestRegistrationRequalificationChangesOnlyConfigurationAndFreshAcknowledgement(t *testing.T) {
	original, intended, backend, files := registrationRequalificationFixture(t)
	if err := ValidateRegistrationRequalificationPlan(original, intended, backend, files); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*journal){
		"boot":     func(j *journal) { r := j.Entries[9]; r.Backend.Boot = uuid.NewString(); j.Entries[9] = r },
		"pid":      func(j *journal) { r := j.Entries[9]; r.Backend.PID++; j.Entries[9] = r },
		"start":    func(j *journal) { r := j.Entries[9]; r.Backend.Start = "401"; j.Entries[9] = r },
		"domain":   func(j *journal) { r := j.Entries[9]; r.Backend.GroupInode++; j.Entries[9] = r },
		"provider": func(j *journal) { r := j.Entries[9]; r.Backend.Backend = "ubuntu-jammy-cron-v1"; j.Entries[9] = r },
		"received": func(j *journal) { r := j.Entries[9]; r.Received = true; j.Entries[9] = r },
		"nonce": func(j *journal) {
			var old journal
			_ = json.Unmarshal(original, &old)
			r := j.Entries[9]
			r.Nonce = old.Entries[9].Nonce
			j.Entries[9] = r
		},
		"helper": func(j *journal) {
			r := j.Entries[9]
			r.Helper = filepath.Join(stateRoot, "helpers", strings.Repeat("d", 64))
			j.Entries[9] = r
		},
		"database":           func(j *journal) { r := j.Entries[9]; r.Database = "/other/fluxo.db"; j.Entries[9] = r },
		"identity":           func(j *journal) { r := j.Entries[9]; r.Identity = uuid.NewString(); j.Entries[9] = r },
		"command":            func(j *journal) { r := j.Entries[9]; r.Contract.Command = "/bin/true"; j.Entries[9] = r },
		"predecessor":        func(j *journal) { r := j.Entries[9]; r.Predecessor = "rewritten history"; j.Entries[9] = r },
		"unaffected":         func(j *journal) { r := j.Entries[10]; r.Enabled = true; j.Entries[10] = r },
		"invocation history": func(j *journal) { j.Invocations["invented"] = invocation{Status: "completed"} },
	} {
		t.Run(name, func(t *testing.T) {
			var next journal
			_ = json.Unmarshal(intended, &next)
			change(&next)
			bad, _ := executiondomain.MergeActivationJSON(original, next)
			changed := append([]executiondomain.ActivationFile(nil), files...)
			changed[0].Data = bad
			changed[1].Data = []byte(cronContent(next.Entries[9]))
			targets, _ := json.Marshal(map[int]scheduler.CronBackendIdentity{9: next.Entries[9].Backend})
			if ValidateRegistrationRequalificationPlan(original, bad, digest(targets), changed) == nil {
				t.Fatal("unreviewed authority survived projection")
			}
		})
	}
	for name, change := range map[string]func([]executiondomain.ActivationFile){
		"wrong predecessor": func(f []executiondomain.ActivationFile) { f[1].Before = strings.Repeat("d", 64) },
		"helper refresh":    func(f []executiondomain.ActivationFile) { f[1].Image = true },
		"missing trigger":   func(f []executiondomain.ActivationFile) { f[1].Cron = false },
	} {
		t.Run(name, func(t *testing.T) {
			bad := append([]executiondomain.ActivationFile(nil), files...)
			change(bad)
			if ValidateRegistrationRequalificationPlan(original, intended, backend, bad) == nil {
				t.Fatal("bad publication accepted")
			}
		})
	}
}
