package phpupdate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fluxo/internal/database"
	"fluxo/internal/services/phprepository"
	"fmt"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestRepositoryConsumedPlansCannotStartAnotherPublication(t *testing.T) {
	m := testStore(t)
	p := repositoryPlan{ID: uuid.NewString(), CreatedAt: time.Now().Unix()}
	raw, _ := json.Marshal(p)
	if _, err := database.DB.Exec("INSERT INTO php_repository_plans(id,payload,created_at) VALUES (?,?,?)", p.ID, string(raw), p.CreatedAt); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"failed", "committed"} {
		if _, err := database.DB.Exec("INSERT OR REPLACE INTO php_repository_migrations(plan_id,state,before_policy,created_at) VALUES (?,?,?,?)", p.ID, state, "before", time.Now().Unix()); err != nil {
			t.Fatal(err)
		}
		if _, err := m.EnqueueRepository(context.Background(), "repository_apply", p.ID, uuid.NewString(), "test", ""); !errors.Is(err, ErrReviewChanged) {
			t.Fatalf("%s plan reused: %v", state, err)
		}
		if active, _ := ActiveOperation(); active != nil {
			t.Fatal("consumed plan launched publication")
		}
	}
	if _, err := database.DB.Exec("UPDATE php_repository_migrations SET state='pending' WHERE plan_id=?", p.ID); err != nil {
		t.Fatal(err)
	}
	status := m.RepositoryStatus(context.Background())
	if status.State != "interrupted" || status.ResumePlanID != p.ID || status.Review == nil {
		t.Fatalf("pending intent lost resume review: %+v", status)
	}
	busy, err := Applying()
	if err != nil || !busy {
		t.Fatal("pending intent lost durable maintenance gate", err)
	}
	if _, err = m.Enqueue(context.Background(), "check", "", "", "", "test", ""); !errors.Is(err, ErrRecoveryRequired) {
		t.Fatal("check admitted across partial source publication", err)
	}
}

func TestRepositoryCurrentRequiresDesiredConfigurationAndPinnedKey(t *testing.T) {
	key := fmt.Sprintf("%x", sha256.Sum256(phprepository.Key))
	c := repositoryConfiguration{SuryEntries: 1, PolicyHash: "policy", KeyHash: key, KeyFingerprint: phprepository.Fingerprint}
	if !repositoryConfigurationCurrent(c, "policy", key) {
		t.Fatal("current configuration rejected")
	}
	if repositoryConfigurationCurrent(c, "policy", "wrong") {
		t.Fatal("incorrect authorization key accepted")
	}
	c.KeyHash = ""
	if repositoryConfigurationCurrent(c, "policy", key) {
		t.Fatal("missing key accepted")
	}
	c.KeyHash = key
	c.Changes = []repositoryFile{{Path: "/etc/apt/preferences.d/fluxo-php-vendors"}}
	if repositoryConfigurationCurrent(c, "policy", key) {
		t.Fatal("old preferences skipped repair")
	}
}
func TestRepositoryRecoveryAuthorizesExactCommittedManifestTransitionsOnly(t *testing.T) {
	testStore(t)
	g := recoveryGeneration{ID: uuid.NewString(), Archives: archiveManifest{PolicyHash: "old"}}
	id := uuid.NewString()
	if _, err := database.DB.Exec("INSERT INTO php_repository_migrations(plan_id,state,before_policy,after_policy,created_at) VALUES (?,'pending','old','new',1)", id); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec("INSERT INTO php_repository_recovery_authorizations(plan_id,generation_id,manifest_hash,before_policy,after_policy) VALUES (?,?,?,'old','new')", id, g.ID, digest(g.Archives)); err != nil {
		t.Fatal(err)
	}
	if recoveryPolicyAuthorized(g, "new") {
		t.Fatal("uncommitted migration authorized recovery")
	}
	if _, err := database.DB.Exec("UPDATE php_repository_migrations SET state='committed' WHERE plan_id=?", id); err != nil {
		t.Fatal(err)
	}
	if !recoveryPolicyAuthorized(g, "new") || recoveryPolicyAuthorized(g, "unrelated") {
		t.Fatal("exact policy transition failed")
	}
	g.Archives.Schema = 2
	if recoveryPolicyAuthorized(g, "new") {
		t.Fatal("different manifest inherited migration authorization")
	}
}

func TestRepositoryPriorTerminalJournalDoesNotStrandUnpublishedIntent(t *testing.T) {
	m := testStore(t)
	prior, current := uuid.NewString(), uuid.NewString()
	for _, pair := range [][2]string{{"committed", "committed"}, {"failed", "restored"}, {"pending", "pending"}} {
		if _, err := database.DB.Exec("INSERT OR REPLACE INTO php_repository_migrations(plan_id,state,before_policy,created_at) VALUES (?,?,?,?)", prior, pair[0], "before", 1); err != nil {
			t.Fatal(err)
		}
		if got := m.repositoryIntentUnpublished(current, prior, pair[1]); got != (pair[0] != "pending") {
			t.Fatalf("prior journal %v: unpublished=%v", pair, got)
		}
	}
	if m.repositoryIntentUnpublished(current, "unknown", "committed") || m.repositoryIntentUnpublished(current, current, "pending") || m.repositoryIntentUnpublished(current, prior, "ambiguous") {
		t.Fatal("ambiguous publication retired maintenance intent")
	}
	if !m.repositoryIntentUnpublished(current, current, "restored") {
		t.Fatal("restored publication remained blocked")
	}
}
