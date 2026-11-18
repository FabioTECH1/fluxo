package maintenancetask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"sort"

	"fluxo/internal/services/executiondomain"
	"fluxo/internal/services/scheduler"
	"github.com/google/uuid"
)

type RegistrationBackendReview struct {
	TaskCoreSHA256       string                                `json:"task_core_sha256"`
	CurrentBackendSHA256 string                                `json:"current_backend_sha256"`
	AffectedIDs          []int                                 `json:"affected_ids"`
	Saved                map[int]scheduler.CronBackendIdentity `json:"saved"`
	Current              map[int]scheduler.CronBackendIdentity `json:"current"`
}

// Configuration requalification is deliberately distinct from ordinary V1
// equality. No reboot, restart, domain replacement or provider migration may
// obtain authority through this operation.
func sameBackendGeneration(a, b scheduler.CronBackendIdentity) bool {
	return a.Backend == b.Backend && a.Boot == b.Boot && a.PID == b.PID && a.Start == b.Start && a.Group == b.Group && a.GroupInode == b.GroupInode
}

func inspectRegistrationBackends(ctx context.Context, j journal, original []byte) (RegistrationBackendReview, error) {
	return inspectRegistrationBackendsForMode(ctx, j, original, false)
}

func inspectRegistrationBackendsForMode(ctx context.Context, j journal, original []byte, installer bool) (RegistrationBackendReview, error) {
	report := RegistrationBackendReview{TaskCoreSHA256: digest(original), AffectedIDs: []int{}, Saved: map[int]scheduler.CronBackendIdentity{}, Current: map[int]scheduler.CronBackendIdentity{}}
	if j.Schema != 3 {
		return report, errors.New("registration requalification requires an existing current task core")
	}
	boot, err := bootID()
	if err != nil {
		return report, err
	}
	cohorts := map[string]scheduler.CronBackendIdentity{}
	qualified := map[string]scheduler.CronBackendIdentity{}
	for id, r := range j.Entries {
		if !r.Enabled {
			continue
		}
		if !r.Published || r.Database != "/var/lib/fluxo/fluxo.db" || !installer && (!r.Received || r.Boot != boot) {
			return report, errors.New("finish original registration acknowledgement and current-boot reconciliation first")
		}
		if err = verifyRegistration(ctx, r); err != nil {
			return report, err
		}
		if err = verifyRequalificationHelperFloor(ctx, r.Helper); err != nil {
			return report, err
		}
		if err = verifyHelperReaderContracts(ctx, r.Helper); err != nil {
			return report, err
		}
		if err = scheduler.ValidateBackendIdentity(r.Backend); err != nil {
			return report, err
		}
		if saved, ok := cohorts[r.Backend.Backend]; !installer && ok && saved != r.Backend {
			return report, errors.New("inconsistent saved backend cohorts require explicit investigation")
		}
		cohorts[r.Backend.Backend] = r.Backend
		now, known := qualified[r.Backend.Backend]
		if !known {
			var e error
			now, e = scheduler.RequalifyCronBackend(ctx, r.Backend)
			if e != nil {
				return report, e
			}
			qualified[r.Backend.Backend] = now
		}
		if installer {
			if err = scheduler.ValidateInstallerBackendRenewal(r.Backend, now); err != nil {
				return report, err
			}
		} else if !sameBackendGeneration(r.Backend, now) {
			return report, errors.New("cron generation changed; configuration requalification cannot rebind it")
		}
		report.Saved[id], report.Current[id] = r.Backend, now
		if now == r.Backend && (!installer || r.Boot == now.Boot) {
			continue
		}
		if !installer && now.Backend != "ubuntu-jammy-cron-v1" && now.Backend != "ubuntu-resolute-cron-v1" {
			return report, errors.New("backend configuration requalification is not reviewed for this provider")
		}
		if err = scheduler.RequireCronBackendWrite(ctx, now, r.Helper); err != nil {
			return report, err
		}
		report.AffectedIDs = append(report.AffectedIDs, id)
	}
	sort.Ints(report.AffectedIDs)
	raw, _ := json.Marshal(report.Current)
	report.CurrentBackendSHA256 = digest(raw)
	return report, nil
}

// Caller holds the existing task gate. Inspection performs no initialization,
// publication, retirement or repair.
func InspectRegistrationBackendsLocked(ctx context.Context) (RegistrationBackendReview, error) {
	j, err := read()
	if err != nil {
		return RegistrationBackendReview{}, err
	}
	original, err := readRegular(filepath.Join(stateRoot, "state.json"), 8<<20)
	if err != nil {
		return RegistrationBackendReview{}, err
	}
	return inspectRegistrationBackends(ctx, j, original)
}

func registrationRequalificationDomainsEmpty(ctx context.Context, j journal) error {
	if err := settled(ctx, &j); err != nil {
		return err
	}
	for _, v := range j.Invocations {
		if v.Status == "submitted" || v.Status == "started" || v.Status == "failed" {
			return errors.New("resolve outstanding task execution authority before backend requalification")
		}
		n, err := domainCount(v.Unit)
		if err != nil {
			return err
		}
		if n != 0 {
			return errors.New("wait for all task descendants to finish naturally")
		}
	}
	return nil
}

func PlanRegistrationRequalificationLocked(ctx context.Context, coreSHA, backendSHA string) ([]executiondomain.ActivationFile, error) {
	return planRegistrationBackendChangeLocked(ctx, coreSHA, backendSHA, false)
}

func PlanInstallerRegistrationRenewalLocked(ctx context.Context, coreSHA, backendSHA string) ([]executiondomain.ActivationFile, error) {
	return planRegistrationBackendChangeLocked(ctx, coreSHA, backendSHA, true)
}

func planRegistrationBackendChangeLocked(ctx context.Context, coreSHA, backendSHA string, installer bool) ([]executiondomain.ActivationFile, error) {
	j, err := read()
	if err != nil {
		return nil, err
	}
	if err = registrationRequalificationDomainsEmpty(ctx, j); err != nil {
		return nil, err
	}
	original, err := readRegular(filepath.Join(stateRoot, "state.json"), 8<<20)
	if err != nil {
		return nil, err
	}
	review, err := inspectRegistrationBackendsForMode(ctx, j, original, installer)
	if err != nil {
		return nil, err
	}
	if review.TaskCoreSHA256 != coreSHA || review.CurrentBackendSHA256 != backendSHA || len(review.AffectedIDs) == 0 {
		return nil, errors.New("reviewed task core/backend changed or no requalification is needed")
	}
	seenBackends := map[scheduler.CronBackendIdentity]bool{}
	for _, now := range review.Current {
		if seenBackends[now] {
			continue
		}
		seenBackends[now] = true
		if err = scheduler.VerifyCronBackendEmpty(ctx, now); err != nil {
			return nil, err
		}
	}
	changes := []executiondomain.ActivationFile{}
	for _, id := range review.AffectedIDs {
		r := j.Entries[id]
		before, e := readRegular(cronPath(id), 1<<20)
		if e != nil {
			return nil, e
		}
		r.Backend, r.Nonce, r.Received = review.Current[id], uuid.NewString(), false
		if installer {
			r.Boot = r.Backend.Boot
		}
		j.Entries[id] = r
		changes = append(changes, executiondomain.ActivationFile{Path: cronPath(id), Before: digest(before), Data: []byte(cronContent(r)), Mode: 0644, Cron: true})
	}
	intended, err := executiondomain.MergeActivationJSON(original, j)
	if err != nil {
		return nil, err
	}
	changes = append([]executiondomain.ActivationFile{{Path: filepath.Join(stateRoot, "state.json"), Before: coreSHA, Data: intended, Mode: 0600}}, changes...)
	if err = validateRegistrationBackendChangePlan(original, intended, backendSHA, changes, installer); err != nil {
		return nil, err
	}
	return changes, nil
}

// Reconstruct the entire target from the source, admitting only Backend,
// Nonce and Received on the affected entries. Historical receipt/contract,
// helper, invocation and registration evidence remain byte-preserved by merge.
func ValidateRegistrationRequalificationPlan(original, intended []byte, backendSHA string, changes []executiondomain.ActivationFile) error {
	return validateRegistrationBackendChangePlan(original, intended, backendSHA, changes, false)
}

func ValidateInstallerRegistrationRenewalPlan(original, intended []byte, backendSHA string, changes []executiondomain.ActivationFile) error {
	return validateRegistrationBackendChangePlan(original, intended, backendSHA, changes, true)
}

func validateRegistrationBackendChangePlan(original, intended []byte, backendSHA string, changes []executiondomain.ActivationFile, installer bool) error {
	var old, next journal
	if len(original) == 0 || json.Unmarshal(original, &old) != nil || json.Unmarshal(intended, &next) != nil || old.Schema != 3 || next.Schema != 3 {
		return errors.New("task requalification cannot initialize or convert journal formats")
	}
	targets := map[int]scheduler.CronBackendIdentity{}
	triggers := map[string]string{}
	predecessors := map[string]string{}
	changed := 0
	for id, r := range old.Entries {
		target, ok := next.Entries[id]
		if !ok {
			return errors.New("task requalification removed a registration")
		}
		if !r.Enabled {
			continue
		}
		targets[id] = target.Backend
		if target.Backend == r.Backend && (!installer || target.Boot == r.Boot) {
			continue
		}
		if installer {
			if scheduler.ValidateInstallerBackendRenewal(r.Backend, target.Backend) != nil || target.Boot != target.Backend.Boot {
				return errors.New("installer task renewal changed the saved provider contract or target boot")
			}
		} else if !sameBackendGeneration(r.Backend, target.Backend) || scheduler.ValidateBackendIdentity(target.Backend) != nil || target.Backend.Backend != "ubuntu-jammy-cron-v1" && target.Backend.Backend != "ubuntu-resolute-cron-v1" {
			return errors.New("task requalification changed backend generation or immutable contract")
		}
		nonce, e := uuid.Parse(target.Nonce)
		if e != nil || nonce.String() != target.Nonce || target.Nonce == r.Nonce || target.Received || !installer && !r.Received || !r.Published {
			return errors.New("task requalification lacks a fresh unacknowledged nonce")
		}
		predecessors[cronPath(id)] = digest([]byte(cronContent(r)))
		r.Backend, r.Nonce, r.Received = target.Backend, target.Nonce, false
		if installer {
			r.Boot = target.Boot
		}
		old.Entries[id] = r
		triggers[cronPath(id)] = cronContent(r)
		changed++
	}
	raw, _ := json.Marshal(targets)
	if changed == 0 || digest(raw) != backendSHA || len(old.Entries) != len(next.Entries) || !reflect.DeepEqual(old, next) {
		return errors.New("task requalification changed authority outside the reviewed backend/nonce fields")
	}
	expected, err := executiondomain.MergeActivationJSON(original, old)
	if err != nil {
		return err
	}
	if !bytes.Equal(expected, intended) {
		return errors.New("task requalification changed unrecognized retained history")
	}
	if len(changes) != changed+1 {
		return errors.New("task requalification has an incomplete or extra target set")
	}
	coreFound := false
	for _, f := range changes {
		if f.Image || f.Directory || f.Lock || len(f.Fence) != 0 {
			return errors.New("task requalification cannot change helpers or infrastructure")
		}
		if f.Path == filepath.Join(stateRoot, "state.json") {
			if coreFound || f.Before != digest(original) || !bytes.Equal(f.Data, intended) || f.Mode != 0600 || f.Cron {
				return errors.New("task requalification core differs from its exact projection")
			}
			coreFound = true
			continue
		}
		expected, ok := triggers[f.Path]
		if !ok || !f.Cron || f.Mode != 0644 || string(f.Data) != expected || f.Before != predecessors[f.Path] {
			return errors.New("task requalification trigger escaped the affected set")
		}
		delete(triggers, f.Path)
	}
	if !coreFound || len(triggers) != 0 {
		return errors.New("task requalification lost its core or trigger")
	}
	return nil
}

// Recheck the archived target under its saved verifier without regenerating a
// target from today's host. Prepared-floor recovery may observe original or
// exact intended trigger bytes; identity continuity remains mandatory.
func VerifyRegistrationRequalificationTargetLocked(ctx context.Context, intended []byte) error {
	var j journal
	if err := json.Unmarshal(intended, &j); err != nil {
		return err
	}
	if j.Schema != 3 {
		return errors.New("task requalification target is not current")
	}
	if err := registrationRequalificationDomainsEmpty(ctx, j); err != nil {
		return err
	}
	seenBackends := map[scheduler.CronBackendIdentity]bool{}
	for _, r := range j.Entries {
		if !r.Enabled {
			continue
		}
		if !seenBackends[r.Backend] {
			if err := scheduler.VerifyCronBackendEmpty(ctx, r.Backend); err != nil {
				return err
			}
			seenBackends[r.Backend] = true
		}
		if err := verifyRequalificationHelperFloor(ctx, r.Helper); err != nil {
			return err
		}
		if err := verifyRetainedHelper(r); err != nil {
			return err
		}
		if err := verifyHelperReaderContracts(ctx, r.Helper); err != nil {
			return err
		}
	}
	return nil
}

// A committed retry verifies the sealed publication and preserves a genuine
// Received=true or subsequent invocation history. It never reapplies the target.
func VerifyRegistrationRequalificationCommittedLocked(ctx context.Context, intended []byte) error {
	var target journal
	if err := json.Unmarshal(intended, &target); err != nil {
		return err
	}
	current, err := read()
	if err != nil {
		return err
	}
	for id, r := range target.Entries {
		if !r.Enabled {
			continue
		}
		actual, ok := current.Entries[id]
		if !ok {
			return errors.New("requalified registration was subsequently removed; preserve sealed evidence")
		}
		r.Received = actual.Received
		if !reflect.DeepEqual(r, actual) {
			return errors.New("requalified registration changed after sealed publication")
		}
		if err = verifyRegistrationFiles(actual); err != nil {
			return err
		}
	}
	return nil
}

func verifyRequalificationHelperFloor(ctx context.Context, path string) error {
	f, err := executiondomain.ReadFloor("/var/lib/fluxo")
	if err != nil {
		return err
	}
	if f == nil {
		return errors.New("task requalification requires a retained reader floor")
	}
	have, err := executiondomain.ReadHelperCapabilities(ctx, path, filepath.Base(path))
	if err != nil {
		return err
	}
	need := executiondomain.ReaderCapabilities{Format: 1, SchedulerSupport: f.SchedulerSupport, SystemTaskSupport: f.SystemTaskSupport, SchedulerSchemas: []int{f.SchedulerSchema}, SystemTaskSchemas: []int{f.SystemTaskSchema}, Adapters: f.Adapters, RoleContracts: f.RoleContracts}
	return executiondomain.RequireCapabilities(have, need)
}
