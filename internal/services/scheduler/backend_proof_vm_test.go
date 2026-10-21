package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fluxo/internal/syscmd"
	"github.com/google/uuid"
)

func unboundBackendVMFixture(t *testing.T) (Entry, legacyHost) {
	t.Helper()
	e, _ := vmFixture(t)
	ctx := context.Background()
	h, err := qualifyBackendHost(ctx)
	if err != nil {
		t.Fatal(err)
	}
	u, err := lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer u()
	j, err := read()
	if err != nil {
		t.Fatal(err)
	}
	e = j.Entries[e.ID]
	e.LegacyBoot = h.Boot
	e.Ready = false
	e.MigrationNonce = uuid.NewString()
	j.Entries[e.ID] = e
	j.Legacy[e.Version] = h.Boot
	j.Migrations[e.Version] = legacyMigration{Replacements: map[int]legacyTrigger{e.ID: {Nonce: e.MigrationNonce, Hash: hashBytes([]byte(CronContent(e)))}}, Deletions: map[int]bool{}, Reason: "Previous qualification failed"}
	if err = save(j); err != nil {
		t.Fatal(err)
	}
	if err = publishCron(CronPath(e.ID), []byte(CronContent(e))); err != nil {
		t.Fatal(err)
	}
	return e, h
}

func oldMutation(t *testing.T, entry Entry, legacy, action string) {
	t.Helper()
	if err := oldMutationError(t, entry, legacy, action); err != nil {
		t.Fatal("immutable v0.4.36 mutation failed", err)
	}
}

func oldMutationError(t *testing.T, entry Entry, legacy, action string) error {
	t.Helper()
	path := filepath.Join(stateDir, "old-mutation.json")
	b, _ := json.Marshal(oldMutationRecipe{Entry: entry, Legacy: legacy})
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := syscmd.Run(context.Background(), 10*time.Second, "/root/scheduler-v036-mutation-tests", "fixture-scheduler-"+action, path); err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

func TestVMBackendPreparedRecoveryPreservesOldHelperMutations(t *testing.T) {
	for _, action := range []string{"add", "disable", "delete", "recreate"} {
		t.Run(action, func(t *testing.T) {
			e, h := unboundBackendVMFixture(t)
			ctx := context.Background()
			u, err := lock(ctx)
			if err != nil {
				t.Fatal(err)
			}
			j, err := read()
			if err != nil {
				t.Fatal(err)
			}
			p, err := readBackendProofs()
			if err != nil {
				t.Fatal(err)
			}
			if err = prepareBackendAttempt(ctx, &j, &p, e.Version, h); err != nil {
				t.Fatal(err)
			}
			first := p.Branches[e.Version].ID
			u()
			db, err := sql.Open("sqlite", databasePath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			other := e
			if action == "add" {
				other.ID = e.ID + 1
				other.Identity = ""
				other.MigrationNonce = ""
				if _, err = db.Exec("INSERT INTO crons VALUES(?,1,?,'* * * * *','fluxo')", other.ID, other.Command); err != nil {
					t.Fatal(err)
				}
				legacy := fmt.Sprintf("# Fluxo Cron ID: %d\n* * * * * fluxo cd %s && %s >> /var/log/fluxo/cron-%d.log 2>&1\n", other.ID, other.Directory, other.Command, other.ID)
				if err = publishCron(CronPath(other.ID), []byte(legacy)); err != nil {
					t.Fatal(err)
				}
				oldMutation(t, other, legacy, "materialize")
			} else {
				oldMutation(t, e, "", "disable")
				if action == "delete" {
					if _, err = db.Exec("DELETE FROM crons WHERE id=?", e.ID); err != nil {
						t.Fatal(err)
					}
				}
				if action == "recreate" {
					oldMutation(t, e, "", "materialize")
				}
			}
			if err = ReconcileBackendMigrations(ctx); err != nil {
				t.Fatal("prepared recovery", err)
			}
			u, err = lock(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer u()
			j, err = read()
			if err != nil {
				t.Fatal(err)
			}
			p, err = readBackendProofs()
			if err != nil {
				t.Fatal(err)
			}
			a := p.Branches[e.Version]
			if a.ID == first || len(a.PreviousAttempts) == 0 {
				t.Fatal("superseded attempt lost its history")
			}
			if a.Preparing || !attemptMatchesCore(j, e.Version, a) {
				t.Fatal("recovered attempt is not consistently published")
			}
			if action == "add" {
				if len(a.Migration.Replacements) != 2 {
					t.Fatal("new old-helper obligation was lost")
				}
				if a.Migration.Replacements[e.ID].Received || a.Migration.Replacements[other.ID].Received {
					t.Fatal("reload proof was invented")
				}
			}
			if action == "disable" || action == "delete" {
				if !a.Migration.Deletions[e.ID] {
					t.Fatal("retirement obligation lost")
				}
				if _, err = os.Lstat(CronPath(e.ID)); !os.IsNotExist(err) {
					t.Fatal("absent trigger was recreated", err)
				}
			}
			if action == "recreate" && a.Bindings[e.ID] == e.Identity {
				t.Fatal("new registration identity was not adopted")
			}
			if a.Certified {
				t.Fatal("recovered attempt certified before receipts")
			}
		})
	}
}
