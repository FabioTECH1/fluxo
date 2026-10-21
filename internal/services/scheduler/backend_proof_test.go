package scheduler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func proofFixture() backendProofs {
	boot := uuid.NewString()
	m := legacyMigration{Replacements: map[int]legacyTrigger{1: {Nonce: uuid.NewString(), Hash: strings.Repeat("a", 64)}}, Deletions: map[int]bool{}}
	return backendProofs{Format: 1, Branches: map[string]backendAttempt{"8.4": {
		ID: uuid.NewString(), Boot: boot, Host: legacyHost{Boot: boot, PID: 10, Start: "20", GroupInode: 30},
		Bindings: map[int]string{1: uuid.NewString()}, Migration: m, Previous: cloneMigration(m), Preparing: true, PreviousAttempts: []backendAttemptEvidence{},
	}}}
}

func TestProofTransitionFieldsCannotBeOmitted(t *testing.T) {
	fixture := proofFixture()
	valid, _ := json.Marshal(fixture)
	if _, err := decodeBackendProofs(valid); err != nil {
		t.Fatal(err)
	}
	for _, malformed := range []string{`{}`, `null`, `{"format":1}`, `{"branches":{}}`, `{"format":1,"branches":{}}`} {
		if _, err := decodeBackendProofs([]byte(malformed)); err == nil {
			t.Fatalf("accepted incomplete history: %s", malformed)
		}
	}
	for _, key := range []string{"preparing", "certified", "previous", "published_at"} {
		t.Run(key, func(t *testing.T) {
			var doc map[string]any
			if err := json.Unmarshal(valid, &doc); err != nil {
				t.Fatal(err)
			}
			a := doc["branches"].(map[string]any)["8.4"].(map[string]any)
			delete(a, key)
			bad, _ := json.Marshal(doc)
			if _, err := decodeBackendProofs(bad); err == nil {
				t.Fatal("missing transition authority was accepted")
			}
		})
	}
	var doc map[string]any
	_ = json.Unmarshal(valid, &doc)
	a := doc["branches"].(map[string]any)["8.4"].(map[string]any)
	target := a["migration"].(map[string]any)["replacements"].(map[string]any)["1"].(map[string]any)
	delete(target, "received")
	bad, _ := json.Marshal(doc)
	if _, err := decodeBackendProofs(bad); err == nil {
		t.Fatal("missing receipt field was accepted")
	}
}

func TestRetainedAttemptKeepsRegistrationBindings(t *testing.T) {
	p := proofFixture()
	a := p.Branches["8.4"]
	a.PreviousAttempts = []backendAttemptEvidence{{ID: uuid.NewString(), Host: a.Host, Migration: cloneMigration(a.Previous), Bindings: a.Bindings}}
	p.Branches["8.4"] = a
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeBackendProofs(raw)
	if err != nil || decoded.Branches["8.4"].PreviousAttempts[0].Bindings[1] != a.Bindings[1] {
		t.Fatal("retained registration binding lost", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	history := doc["branches"].(map[string]any)["8.4"].(map[string]any)["previous_attempts"].([]any)[0].(map[string]any)
	delete(history, "bindings")
	bad, _ := json.Marshal(doc)
	if _, err := decodeBackendProofs(bad); err == nil {
		t.Fatal("incomplete retained registration history accepted")
	}
}
