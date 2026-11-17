package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"fluxo/internal/services/executiondomain"
)

// The contract remains after completion. Removing the marker would erase the
// fact that a reader-fenced migration, rather than ordinary ready conversion,
// produced this generation. Activation must bind it to its immutable archive.
type historicalRetirement struct {
	Contract         string `json:"contract"`
	Operation        string `json:"operation"`
	SourceSHA256     string `json:"source_sha256"`
	Phase            string `json:"phase"`
	CompletionSHA256 string `json:"completion_sha256,omitempty"`
}

func historicalRetirementPending(j journal) bool {
	return j.HistoricalRetirement != nil && j.HistoricalRetirement.Phase == "pending"
}

func validateHistoricalRetirement(j journal) error {
	h := j.HistoricalRetirement
	if h == nil {
		return nil
	}
	if j.Schema != executiondomain.SchedulerLoaderSchema || h.Contract != executiondomain.SchedulerHistoricalRetirementContract ||
		!loaderUUID(h.Operation) || !loaderDigestValid(h.SourceSHA256) || h.Phase != "pending" && h.Phase != "complete" {
		return errors.New("invalid historical Scheduler retirement contract")
	}
	if h.Phase == "pending" {
		if h.CompletionSHA256 != "" {
			return errors.New("pending retirement cannot claim completion evidence")
		}
		if j.LoaderEpochEstablished {
			return errors.New("historical retirement cannot claim loader authority")
		}
		for _, e := range j.Entries {
			if e.LoaderNonce != "" {
				return errors.New("historical retirement cannot publish loader nonces")
			}
		}
		return nil
	}
	if !loaderDigestValid(h.CompletionSHA256) {
		return errors.New("completed retirement lacks its immutable evidence digest")
	}
	if len(j.Legacy) != 0 {
		return errors.New("completed historical retirement retains unresolved legacy branches")
	}
	for _, e := range j.Entries {
		if e.Enabled && !e.Ready {
			return errors.New("completed historical retirement retains an unready registration")
		}
	}
	return nil
}

func historicalActivationReady(ctx context.Context, j journal) error {
	if j.HistoricalRetirement != nil || len(j.Legacy) == 0 || j.LoaderEpochEstablished {
		return errors.New("historical activation requires unresolved original legacy authority without loader history")
	}
	if _, err := readBackendProofs(); err != nil {
		return err
	}
	for branch := range j.Legacy {
		if _, ok := j.Migrations[branch]; !ok {
			return errors.New("historical branch has no retained migration evidence")
		}
	}
	// Conversion must not cancel a historical application to obtain admission.
	host, err := qualifyBackendHost(ctx)
	if err != nil {
		return err
	}
	return legacyDomainDrained(ctx, host)
}

// Historical journals retain their original Certified fields. New backend
// attempts certify in a separate protected store; neither store substitutes for
// an unresolved obligation in the other.
func historicalRetirementResolved(j journal, p backendProofs) bool {
	if len(j.Legacy) != 0 || j.Upgrade != nil || len(j.Holds) != 0 {
		return false
	}
	for branch, m := range j.Migrations {
		if m.Certified {
			continue
		}
		a, ok := p.Branches[branch]
		if !ok || !a.Certified || a.Preparing || !attemptMatchesCore(j, branch, a) {
			return false
		}
	}
	for _, a := range p.Branches {
		if a.Preparing || !a.Certified {
			return false
		}
	}
	for _, e := range j.Entries {
		if e.Enabled && !e.Ready {
			return false
		}
	}
	return true
}

func completeHistoricalRetirement(ctx context.Context, j *journal, p backendProofs) error {
	if !historicalRetirementPending(*j) || !historicalRetirementResolved(*j, p) {
		return nil
	}
	for _, v := range j.Invocations {
		if err := invocationQuiescent(ctx, v); err != nil {
			return err
		}
	}
	// A previously certified sidecar is historical evidence, not permission to
	// overlook configuration/generation drift before the final transition.
	for _, a := range p.Branches {
		if a.RetiredByBoot == "" {
			if err := VerifyCronBackendEmpty(ctx, a.Host); err != nil {
				return err
			}
		}
	}
	next := *j.HistoricalRetirement
	next.Phase = "complete"
	existing, _, err := readLoaderProtectedFile(historicalCompletionPath(next.Operation), loaderFileBudget)
	if err == nil {
		// A crash after evidence publication may leave harmless receipt/history
		// evolution. Consume the original record; never regenerate or replay its
		// core/proofs. Current obligations were independently checked above.
		next.CompletionSHA256 = hashBytes(existing)
		if err = validateHistoricalCompletionBytes(existing, &next); err != nil {
			return err
		}
		j.HistoricalRetirement = &next
		return save(*j)
	}
	if !os.IsNotExist(err) {
		return err
	}
	core, err := json.Marshal(*j)
	if err != nil {
		return err
	}
	proof, err := os.ReadFile(proofPath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	record, err := json.Marshal(historicalCompletion{Contract: next.Contract, Operation: next.Operation, SourceSHA256: next.SourceSHA256, Core: core, Proofs: proof})
	if err != nil {
		return err
	}
	if err = createLoaderArchiveNoReplace(historicalCompletionPath(next.Operation), record); err != nil {
		return err
	}
	next.Phase = "complete"
	next.CompletionSHA256 = hashBytes(record)
	j.HistoricalRetirement = &next
	return save(*j)
}

type historicalCompletion struct {
	Contract     string `json:"contract"`
	Operation    string `json:"operation"`
	SourceSHA256 string `json:"source_sha256"`
	Core         []byte `json:"pending_core"`
	Proofs       []byte `json:"backend_proofs"`
}

func historicalCompletionPath(operation string) string {
	return filepath.Join(stateDir, "historical-retirement-"+operation+".json")
}

func verifyHistoricalCompletion(j journal) error {
	h := j.HistoricalRetirement
	if h == nil || h.Phase != "complete" {
		return nil
	}
	raw, _, err := readLoaderProtectedFile(historicalCompletionPath(h.Operation), loaderFileBudget)
	if err != nil {
		return err
	}
	if hashBytes(raw) != h.CompletionSHA256 {
		return errors.New("historical completion archive changed")
	}
	return validateHistoricalCompletionBytes(raw, h)
}

func validateHistoricalCompletionBytes(raw []byte, h *historicalRetirement) error {
	if len(raw) == 0 || len(raw) > loaderFileBudget || hashBytes(raw) != h.CompletionSHA256 {
		return errors.New("historical completion digest changed")
	}
	var record historicalCompletion
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&record); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("trailing historical completion data")
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return err
	}
	if record.Contract != h.Contract || record.Operation != h.Operation || record.SourceSHA256 != h.SourceSHA256 {
		return errors.New("historical completion belongs to another source or operation")
	}
	var snapshot journal
	if err := json.Unmarshal(record.Core, &snapshot); err != nil {
		return err
	}
	want := *h
	want.Phase, want.CompletionSHA256 = "pending", ""
	if !reflect.DeepEqual(snapshot.HistoricalRetirement, &want) {
		return errors.New("historical completion source contract changed")
	}
	if err := validateHistoricalRetirement(snapshot); err != nil {
		return err
	}
	p := backendProofs{Branches: map[string]backendAttempt{}}
	if record.Proofs != nil {
		var err error
		p, err = decodeBackendProofs(record.Proofs)
		if err != nil {
			return err
		}
	}
	if !historicalRetirementResolved(snapshot, p) {
		return errors.New("historical completion archive retains unresolved authority")
	}
	return nil
}

// Ordinary writers may neither remove the permanent contract nor rewind a
// completed phase. Activation is the sole initial creator of this field.
func historicalSaveTransition(j journal) error {
	raw, err := os.ReadFile(filepath.Join(stateDir, "state.json"))
	if os.IsNotExist(err) && j.HistoricalRetirement == nil {
		return nil
	}
	if err != nil {
		return err
	}
	if j.HistoricalRetirement == nil && !bytes.Contains(raw, []byte("\"historical_retirement\"")) {
		return nil
	}
	raw, _, err = readLoaderProtectedFile(filepath.Join(stateDir, "state.json"), 32<<20)
	if err != nil {
		return err
	}
	var previous struct {
		HistoricalRetirement *historicalRetirement `json:"historical_retirement"`
	}
	if err = json.Unmarshal(raw, &previous); err != nil {
		return err
	}
	a, b := previous.HistoricalRetirement, j.HistoricalRetirement
	if a == nil && b == nil {
		return nil
	}
	if a == nil || b == nil || a.Contract != b.Contract || a.Operation != b.Operation || a.SourceSHA256 != b.SourceSHA256 || a.Phase == "complete" && !reflect.DeepEqual(a, b) {
		return errors.New("ordinary Scheduler write cannot create, remove or rewind historical retirement")
	}
	return nil
}

// Sources are copied into the immutable activation archive. Nil is explicit
// observed absence. No trigger is changed by reader-format conversion.
func HistoricalActivationSourcesLocked() (map[string][]byte, error) {
	j, err := read()
	if err != nil {
		return nil, err
	}
	p, err := readBackendProofs()
	if err != nil {
		return nil, err
	}
	for _, a := range p.Branches {
		if a.Preparing {
			return nil, errors.New("recover backend publication before historical activation")
		}
	}
	out := map[string][]byte{}
	paths := []string{proofPath(), proofAnchor(), proofInitialization()}
	for id := range j.Entries {
		paths = append(paths, CronPath(id))
	}
	for branch := range j.Migrations {
		paths = append(paths, markerPath(branch))
	}
	for _, path := range paths {
		raw, e := os.ReadFile(path)
		if os.IsNotExist(e) {
			out[path] = nil
			continue
		}
		if e != nil {
			return nil, e
		}
		if e = owned(path, false); e != nil {
			return nil, e
		}
		if len(raw) > 1<<20 {
			return nil, errors.New("historical trigger evidence exceeds its budget")
		}
		out[path] = raw
	}
	raw, err := os.ReadFile(stateDir + "/state.json")
	if err != nil {
		return nil, err
	}
	return out, ValidateHistoricalActivationSources(raw, out)
}

func ValidateHistoricalActivationSources(original []byte, sources map[string][]byte) error {
	var j journal
	if err := json.Unmarshal(original, &j); err != nil {
		return err
	}
	if len(sources) != len(j.Entries)+len(j.Migrations)+3 {
		return errors.New("incomplete historical trigger inventory")
	}
	for id, e := range j.Entries {
		raw, ok := sources[CronPath(id)]
		if !ok || e.Enabled && string(raw) != CronContent(e) || !e.Enabled && raw != nil {
			return errors.New("historical registration trigger differs from archived intent")
		}
	}
	p := backendProofs{Branches: map[string]backendAttempt{}}
	proof, hasProof := sources[proofPath()]
	anchor, hasAnchor := sources[proofAnchor()]
	initializing, hasInitializing := sources[proofInitialization()]
	if !hasProof || !hasAnchor || !hasInitializing || initializing != nil || (proof == nil) != (anchor == nil) {
		return errors.New("historical backend publication is incomplete")
	}
	if proof != nil {
		var err error
		p, err = decodeBackendProofs(proof)
		if err != nil {
			return err
		}
	}
	for branch, m := range j.Migrations {
		raw, ok := sources[markerPath(branch)]
		if !ok {
			return errors.New("historical marker observation missing")
		}
		certified := m.Certified
		if a, ok := p.Branches[branch]; ok {
			if a.Preparing {
				return errors.New("historical backend publication is pending")
			}
			m, certified = a.Migration, a.Certified
		}
		if certified || len(m.Deletions) == 0 {
			if raw != nil {
				return errors.New("unexpected historical deletion marker")
			}
		} else if string(raw) != markerContent(branch, m) {
			return errors.New("historical deletion marker differs from archived authority")
		}
	}
	return nil
}

// The bridge holds this journal's gate. Task requalification can inspect
// Scheduler's execution obligations without requiring unrelated loader policy
// readiness or changing any Scheduler authority.
func ValidateTaskRequalificationLocked(ctx context.Context) error {
	j, err := read()
	if err != nil {
		return err
	}
	if j.Schema != 5 || historicalRetirementPending(j) || len(j.Legacy) != 0 || len(j.Holds) != 0 || j.Upgrade != nil {
		return errors.New("finish Scheduler historical, installer and maintenance obligations first")
	}
	p, err := readBackendProofs()
	if err != nil {
		return err
	}
	for _, a := range p.Branches {
		if a.Preparing || !a.Certified {
			return errors.New("finish Scheduler backend publication first")
		}
	}
	for _, v := range j.Invocations {
		if err = invocationQuiescent(ctx, v); err != nil {
			return err
		}
	}
	return nil
}
