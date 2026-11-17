package scheduler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestDistributionBackendWriteNeedsActualHelperBeforeEvidence(t *testing.T) {
	previous := stateDir
	stateDir = t.TempDir()
	t.Cleanup(func() { stateDir = previous })
	for _, id := range []string{jammyCronCandidateID, resoluteCronCandidateID} {
		if err := saveBackendProofs(distributionProofFixture(id)); err == nil {
			t.Fatal("published new backend without the actual compatible helper", id)
		}
		for _, name := range []string{"backend-proofs.json", "backend-proofs.initialized", "backend-proofs.initializing"} {
			if _, err := os.Lstat(filepath.Join(stateDir, name)); !os.IsNotExist(err) {
				t.Fatal("unadmitted writer touched persistent backend evidence", name, err)
			}
		}
	}
}

func distributionProofFixture(id string) backendProofs {
	p := proofFixture()
	p.Format = 2
	a := p.Branches["8.4"]
	a.Host.Backend = id
	a.Host.Group = "/system.slice/cron.service"
	a.Host.Configuration = strings.Repeat("a", 64)
	p.Branches["8.4"] = a
	return p
}

func TestDistributionBackendProofNeedsVersionedReaders(t *testing.T) {
	for _, id := range []string{jammyCronCandidateID, resoluteCronCandidateID} {
		p := distributionProofFixture(id)
		raw, _ := json.Marshal(p)
		decoded, err := decodeBackendProofs(raw)
		if err != nil || decoded.Branches["8.4"].Host != p.Branches["8.4"].Host {
			t.Fatal("versioned identity lost", id, err)
		}
		p.Format = 1
		raw, _ = json.Marshal(p)
		if _, err = decodeBackendProofs(raw); err == nil {
			t.Fatal("new identity accepted by historical proof format", id)
		}
	}
}

func TestBackendHistoryCannotSmuggleUnknownOrUnversionedIdentity(t *testing.T) {
	p := proofFixture()
	a := p.Branches["8.4"]
	history := backendAttemptEvidence{ID: uuid.NewString(), Host: a.Host, Migration: cloneMigration(a.Previous), Bindings: a.Bindings}
	history.Host.Backend = jammyCronCandidateID
	history.Host.Group = "/system.slice/cron.service"
	history.Host.Configuration = strings.Repeat("b", 64)
	a.PreviousAttempts = []backendAttemptEvidence{history}
	p.Branches["8.4"] = a
	raw, _ := json.Marshal(p)
	if _, err := decodeBackendProofs(raw); err == nil {
		t.Fatal("historical format accepted a new identity in retained evidence")
	}
	p.Format = 2
	raw, _ = json.Marshal(p)
	if _, err := decodeBackendProofs(raw); err != nil {
		t.Fatal("versioned retained evidence refused", err)
	}
	a.PreviousAttempts[0].Host.Backend = "unknown-future-backend"
	p.Branches["8.4"] = a
	raw, _ = json.Marshal(p)
	if _, err := decodeBackendProofs(raw); err == nil {
		t.Fatal("unknown retained backend accepted")
	}
}

func TestBackendIdentityDoesNotReinterpretLegacyMeaning(t *testing.T) {
	h := proofFixture().Branches["8.4"].Host
	if err := ValidateBackendIdentity(h); err != nil || backendReaderContract(h) != legacyNobleCronContract {
		t.Fatal("legacy identity meaning changed", err)
	}
	for _, mutation := range []func(*legacyHost){
		func(h *legacyHost) { h.Backend = "unknown" },
		func(h *legacyHost) { h.Group = "/other" },
		func(h *legacyHost) { h.Configuration = strings.Repeat("a", 64) },
		func(h *legacyHost) { h.Boot = "" },
	} {
		next := h
		mutation(&next)
		if ValidateBackendIdentity(next) == nil {
			t.Fatal("ambiguous historical backend accepted")
		}
	}
	for _, id := range []string{jammyCronCandidateID, resoluteCronCandidateID} {
		next := distributionProofFixture(id).Branches["8.4"].Host
		if err := ValidateBackendIdentity(next); err != nil {
			t.Fatal(err)
		}
		next.Group = "/system.slice/cronie.service"
		if ValidateBackendIdentity(next) == nil {
			t.Fatal("distribution identity accepted another provider's domain")
		}
	}
}

func TestBackendProofUnknownFieldsArePreservedByRefusal(t *testing.T) {
	raw, _ := json.Marshal(distributionProofFixture(jammyCronCandidateID))
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	doc["future_authority"] = "must not disappear on save"
	raw, _ = json.Marshal(doc)
	if _, err := decodeBackendProofs(raw); err == nil {
		t.Fatal("unknown authority could be silently dropped")
	}
}
