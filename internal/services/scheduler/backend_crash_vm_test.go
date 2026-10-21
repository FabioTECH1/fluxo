package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestVMBackendInitializationCrashBoundariesAndLostHistory(t *testing.T) {
	for _, stage := range []string{"intent", "proof", "anchor", "lost-anchor", "lost-proof", "empty-object", "null", "missing-format"} {
		t.Run(stage, func(t *testing.T) {
			e, h := unboundBackendVMFixture(t)
			ctx := context.Background()
			u, _ := lock(ctx)
			j, err := read()
			if err != nil {
				t.Fatal(err)
			}
			p, _ := readBackendProofs()
			if err = prepareBackendAttempt(ctx, &j, &p, e.Version, h); err != nil {
				t.Fatal(err)
			}
			attempt := p.Branches[e.Version].ID
			raw, err := os.ReadFile(proofPath())
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(CronPath(e.ID))
			interrupted := stage == "intent" || stage == "proof" || stage == "anchor"
			if interrupted {
				if err = atomicFile(proofInitialization(), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "intent" || stage == "proof" || stage == "lost-anchor" {
				if err = os.Remove(proofAnchor()); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "intent" || stage == "lost-proof" {
				if err = os.Remove(proofPath()); err != nil {
					t.Fatal(err)
				}
			}
			malformed := map[string]string{"empty-object": "{}", "null": "null", "missing-format": `{"branches":{}}`}
			if value, ok := malformed[stage]; ok {
				if err = atomicFile(proofPath(), []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
			}
			u()
			err = ReconcileBackendMigrations(ctx)
			if interrupted {
				if err != nil {
					t.Fatal("interrupted initialization did not resume", err)
				}
				p, err = readBackendProofs()
				if err != nil {
					t.Fatal(err)
				}
				if p.Branches[e.Version].ID != attempt || p.Branches[e.Version].Certified {
					t.Fatal("recovery rebound or certified without receipts")
				}
				if _, err = os.Lstat(proofInitialization()); !os.IsNotExist(err) {
					t.Fatal("initialization was not completed")
				}
				if support, err := RequiredSupport(); support != 3 || err != nil {
					t.Fatal("compatibility negotiation", support, err)
				}
			} else {
				if err == nil {
					t.Fatal("lost or incomplete established evidence was accepted")
				}
				after, _ := os.ReadFile(CronPath(e.ID))
				if string(before) != string(after) {
					t.Fatal("blocked recovery changed trigger")
				}
				if _, err := RequiredSupport(); err == nil {
					t.Fatal("compatibility negotiation ignored lost history")
				}
			}
		})
	}
}

func applyVMRegistrationMutation(t *testing.T, e Entry, action string, old bool) {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	materialize := func(entry Entry, legacy string) {
		if old {
			oldMutation(t, entry, legacy, "materialize")
			return
		}
		canonical, ok := Qualify(entry.ID, entry.SiteID, entry.Directory, "* * * * *", entry.Command, "fluxo")
		if !ok {
			t.Fatal("unsupported mutation fixture")
		}
		if err := Materialize(ctx, canonical, legacy, true); err != nil {
			t.Fatal(err)
		}
	}
	if action == "add" {
		e.ID++
		e.Identity, e.MigrationNonce = "", ""
		if _, err = db.Exec("INSERT INTO crons VALUES(?,1,?,'* * * * *','fluxo')", e.ID, e.Command); err != nil {
			t.Fatal(err)
		}
		legacy := fmt.Sprintf("# Fluxo Cron ID: %d\n* * * * * fluxo cd %s && %s >> /var/log/fluxo/cron-%d.log 2>&1\n", e.ID, e.Directory, e.Command, e.ID)
		if err = publishCron(CronPath(e.ID), []byte(legacy)); err != nil {
			t.Fatal(err)
		}
		materialize(e, legacy)
		return
	}
	if old {
		if err := oldMutationError(t, e, "", "disable"); err != nil {
			// Immutable old helpers cannot understand the new prepared marker.
			// They may fail after recording deletion but before removing its file.
			// Recovery must report that incomplete deletion, then the current
			// action can be retried without SSH edits or lost history.
			if recoveryErr := ReconcileBackendMigrations(ctx); recoveryErr == nil || !strings.Contains(recoveryErr.Error(), "Scheduler deletion is incomplete") {
				t.Fatal("incomplete old deletion was not identified", err, recoveryErr)
			}
			if err := Disable(ctx, e.ID); err != nil {
				t.Fatal("current retry could not complete old mutation", err)
			}
			if err := os.Remove(CronPath(e.ID)); err != nil {
				t.Fatal(err)
			}
		}
	} else {
		if err = Disable(ctx, e.ID); err != nil {
			t.Fatal(err)
		}
		if err = os.Remove(CronPath(e.ID)); err != nil {
			t.Fatal(err)
		}
	}
	if action == "delete" {
		if _, err = db.Exec("DELETE FROM crons WHERE id=?", e.ID); err != nil {
			t.Fatal(err)
		}
	}
	if action == "recreate" {
		materialize(e, "")
	}
}

func TestVMBackendInitializationPreservesConcurrentMutations(t *testing.T) {
	for _, stage := range []string{"intent", "proof"} {
		for _, old := range []bool{false, true} {
			for _, action := range []string{"add", "disable", "delete", "recreate"} {
				t.Run(fmt.Sprintf("%s/old-%t/%s", stage, old, action), func(t *testing.T) {
					e, h := unboundBackendVMFixture(t)
					ctx := context.Background()
					u, _ := lock(ctx)
					j, err := read()
					if err != nil {
						t.Fatal(err)
					}
					p, _ := readBackendProofs()
					if err = prepareBackendAttempt(ctx, &j, &p, e.Version, h); err != nil {
						t.Fatal(err)
					}
					original := p.Branches[e.Version].ID
					raw, _ := os.ReadFile(proofPath())
					if err = atomicFile(proofInitialization(), raw, 0600); err != nil {
						t.Fatal(err)
					}
					if err = os.Remove(proofAnchor()); err != nil {
						t.Fatal(err)
					}
					if stage == "intent" {
						if err = os.Remove(proofPath()); err != nil {
							t.Fatal(err)
						}
					}
					u()
					applyVMRegistrationMutation(t, e, action, old)
					if err = ReconcileBackendMigrations(ctx); err != nil {
						t.Fatal("initialization mutation recovery", err)
					}
					p, err = readBackendProofs()
					if err != nil {
						t.Fatal(err)
					}
					a := p.Branches[e.Version]
					if a.Certified || a.Preparing || a.ID == original || len(a.PreviousAttempts) == 0 || a.Host != h {
						t.Fatal("mutation discarded original domain or invented readiness")
					}
					if action == "add" && len(a.Migration.Replacements) != 2 {
						t.Fatal("added obligation lost")
					}
					if action == "disable" || action == "delete" {
						if !a.Migration.Deletions[e.ID] {
							t.Fatal("deletion history lost")
						}
						if _, err = os.Lstat(CronPath(e.ID)); !os.IsNotExist(err) {
							t.Fatal("deleted trigger recreated")
						}
					}
				})
			}
		}
	}
}

func TestVMBackendMissingFirstDeletionMarkerRecovers(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(fmt.Sprint("foreign-marker-", foreign), func(t *testing.T) {
			e, h := unboundBackendVMFixture(t)
			ctx := context.Background()
			u, _ := lock(ctx)
			j, err := read()
			if err != nil {
				t.Fatal(err)
			}
			p, _ := readBackendProofs()
			if err = prepareBackendAttempt(ctx, &j, &p, e.Version, h); err != nil {
				t.Fatal(err)
			}
			m := j.Migrations[e.Version]
			m.Replacements = map[int]legacyTrigger{}
			m.Deletions[e.ID] = true
			m.Marker = uuid.NewString()
			m.MarkerPhase = 1
			j.Migrations[e.Version] = m
			entry := j.Entries[e.ID]
			entry.Enabled = false
			j.Entries[e.ID] = entry
			if err = save(j); err != nil {
				t.Fatal(err)
			}
			if err = os.Remove(CronPath(e.ID)); err != nil {
				t.Fatal(err)
			}
			if foreign {
				if err = publishCron(markerPath(e.Version), []byte("# unrelated marker\n")); err != nil {
					t.Fatal(err)
				}
			}
			u()
			err = ReconcileBackendMigrations(ctx)
			if foreign {
				if err == nil {
					t.Fatal("unrelated marker accepted")
				}
				return
			}
			if err != nil {
				t.Fatal("recorded unpublished marker did not resume", err)
			}
			p, err = readBackendProofs()
			if err != nil {
				t.Fatal(err)
			}
			a := p.Branches[e.Version]
			if !a.Migration.Deletions[e.ID] || a.Certified || a.Migration.MarkerReceived || a.Migration.MarkerPhase != 1 {
				t.Fatal("deletion proof lost or fabricated")
			}
			if _, err = os.Lstat(CronPath(e.ID)); !os.IsNotExist(err) {
				t.Fatal("application trigger recreated")
			}
		})
	}
}

func TestVMBackendPartialPublicationMutationMatrix(t *testing.T) {
	for _, stage := range []string{"core-projected", "first-file", "marker-B-prepared"} {
		for _, old := range []bool{false, true} {
			for _, action := range []string{"add", "disable", "delete", "recreate"} {
				t.Run(fmt.Sprintf("%s/old-%t/%s", stage, old, action), func(t *testing.T) {
					e, h := unboundBackendVMFixture(t)
					stable := e
					stable.ID += 4
					applyVMRegistrationMutation(t, stable, "add", false)
					ctx := context.Background()
					u, _ := lock(ctx)
					j, err := read()
					if err != nil {
						t.Fatal(err)
					}
					m := j.Migrations[e.Version]
					m.Deletions[e.ID+9] = true
					m.Marker, m.MarkerPhase = uuid.NewString(), 1
					j.Migrations[e.Version] = m
					if err = save(j); err != nil {
						t.Fatal(err)
					}
					if err = publishMigrationMarker(e.Version, m); err != nil {
						t.Fatal(err)
					}
					p, _ := readBackendProofs()
					if err = prepareBackendAttempt(ctx, &j, &p, e.Version, h); err != nil {
						t.Fatal(err)
					}
					a := p.Branches[e.Version]
					if stage == "marker-B-prepared" {
						if err = publishBackendAttempt(ctx, &j, &p, e.Version); err != nil {
							t.Fatal(err)
						}
						a = p.Branches[e.Version]
						a.Previous = cloneMigration(j.Migrations[e.Version])
						a.Migration.PreviousMarker = a.Migration.Marker
						a.Migration.Marker, a.Migration.MarkerPhase, a.Preparing = uuid.NewString(), 2, true
						for id, target := range a.Migration.Replacements {
							target.Received = true
							a.Migration.Replacements[id] = target
						}
						p.Branches[e.Version] = a
						if err = saveBackendProofs(p); err != nil {
							t.Fatal(err)
						}
					} else {
						for id, target := range a.Migration.Replacements {
							entry := j.Entries[id]
							entry.MigrationNonce = target.Nonce
							j.Entries[id] = entry
						}
						j.Migrations[e.Version] = cloneMigration(a.Migration)
						if err = save(j); err != nil {
							t.Fatal(err)
						}
						if stage == "first-file" {
							if err = publishCron(CronPath(e.ID), []byte(CronContent(j.Entries[e.ID]))); err != nil {
								t.Fatal(err)
							}
						}
					}
					u()
					applyVMRegistrationMutation(t, e, action, old)
					if err = ReconcileBackendMigrations(ctx); err != nil {
						t.Fatal("partial mutation recovery", err)
					}
					p, err = readBackendProofs()
					if err != nil {
						t.Fatal(err)
					}
					a = p.Branches[e.Version]
					want := 2
					if action == "add" {
						want = 3
					}
					if action == "disable" || action == "delete" {
						want = 1
					}
					if len(a.Migration.Replacements) != want || !a.Migration.Deletions[e.ID+9] || a.Preparing || a.Certified || a.Host != h || len(a.PreviousAttempts) == 0 {
						t.Fatal("partial mutation lost obligations or readiness boundary")
					}
					for _, target := range a.Migration.Replacements {
						if target.Received {
							t.Fatal("new attempt reused a previous receipt")
						}
					}
					if action == "disable" || action == "delete" {
						if _, err = os.Lstat(CronPath(e.ID)); !os.IsNotExist(err) {
							t.Fatal("absent application trigger recreated")
						}
					}
				})
			}
		}
	}
}

func TestVMBackendInitializationKeepsChangedDomainAndHandlesNaturalBoot(t *testing.T) {
	for _, mode := range []string{"identity", "boot", "certified-identity"} {
		t.Run(mode, func(t *testing.T) {
			e, h := unboundBackendVMFixture(t)
			ctx := context.Background()
			u, _ := lock(ctx)
			j, err := read()
			if err != nil {
				t.Fatal(err)
			}
			p, _ := readBackendProofs()
			if err = prepareBackendAttempt(ctx, &j, &p, e.Version, h); err != nil {
				t.Fatal(err)
			}
			if mode == "certified-identity" {
				if err = publishBackendAttempt(ctx, &j, &p, e.Version); err != nil {
					t.Fatal(err)
				}
			}
			a := p.Branches[e.Version]
			if mode == "boot" {
				a.Boot = uuid.NewString()
				a.Host.Boot = a.Boot
				j.Legacy[e.Version] = a.Boot
				entry := j.Entries[e.ID]
				entry.LegacyBoot = a.Boot
				j.Entries[e.ID] = entry
			} else {
				a.Host.Start = "999999999999"
			}
			if mode == "certified-identity" {
				a.Certified = true
				for id, target := range a.Migration.Replacements {
					target.Received = true
					a.Migration.Replacements[id] = target
				}
			}
			p.Branches[e.Version] = a
			if err = save(j); err != nil {
				t.Fatal(err)
			}
			if mode != "certified-identity" {
				raw, _ := json.Marshal(p)
				if err = atomicFile(proofInitialization(), raw, 0600); err != nil {
					t.Fatal(err)
				}
				if err = os.Remove(proofAnchor()); err != nil {
					t.Fatal(err)
				}
				if err = os.Remove(proofPath()); err != nil {
					t.Fatal(err)
				}
			} else {
				if err = saveBackendProofs(p); err != nil {
					t.Fatal(err)
				}
			}
			u()
			err = ReconcileBackendMigrations(ctx)
			p, readErr := readBackendProofs()
			if readErr != nil {
				t.Fatal("storage initialization stayed trapped", readErr)
			}
			actual := p.Branches[e.Version]
			if mode == "identity" {
				if err == nil || actual.Host != a.Host || actual.Certified {
					t.Fatal("unresolved old domain was rebound or certified", err)
				}
			} else if mode == "boot" {
				if err != nil || !actual.Certified || actual.RetiredByBoot != h.Boot {
					t.Fatal("natural boot boundary did not reconcile", err)
				}
			} else {
				if err != nil || actual.Certified || actual.Host != h || len(actual.PreviousAttempts) == 0 {
					t.Fatal("new domain skipped fresh proof after earlier certification", err)
				}
			}
		})
	}
}

// These snapshots model the durable on-disk boundaries, rather than mocking
// receipt authority. Real daemon tests separately authenticate every receipt.
func TestVMBackendPartialPublicationRetainsOldHelperAddition(t *testing.T) {
	for _, stage := range []string{"core-projected", "first-file", "marker-B-projected", "certified-before-core"} {
		t.Run(stage, func(t *testing.T) {
			e, h := unboundBackendVMFixture(t)
			ctx := context.Background()
			u, _ := lock(ctx)
			j, err := read()
			if err != nil {
				t.Fatal(err)
			}
			m := j.Migrations[e.Version]
			m.Deletions[e.ID+9] = true
			m.Marker, m.MarkerPhase = uuid.NewString(), 1
			j.Migrations[e.Version] = m
			if err = save(j); err != nil {
				t.Fatal(err)
			}
			if err = publishMigrationMarker(e.Version, m); err != nil {
				t.Fatal(err)
			}
			p, _ := readBackendProofs()
			if err = prepareBackendAttempt(ctx, &j, &p, e.Version, h); err != nil {
				t.Fatal(err)
			}
			a := p.Branches[e.Version]
			if stage == "marker-B-projected" || stage == "certified-before-core" {
				if err = publishBackendAttempt(ctx, &j, &p, e.Version); err != nil {
					t.Fatal(err)
				}
				a = p.Branches[e.Version]
				if stage == "marker-B-projected" {
					a.Previous = cloneMigration(j.Migrations[e.Version])
					a.Migration.PreviousMarker = a.Migration.Marker
					a.Migration.Marker, a.Migration.MarkerPhase = uuid.NewString(), 2
					a.Preparing = true
				} else {
					a.Certified = true
					for id, target := range a.Migration.Replacements {
						target.Received = true
						a.Migration.Replacements[id] = target
					}
					a.Migration.MarkerPhase, a.Migration.MarkerReceived = 2, true
					if err = publishCron(markerPath(e.Version), []byte(markerContent(e.Version, a.Migration))); err != nil {
						t.Fatal(err)
					}
				}
				p.Branches[e.Version] = a
				if err = saveBackendProofs(p); err != nil {
					t.Fatal(err)
				}
			}
			for id, target := range a.Migration.Replacements {
				entry := j.Entries[id]
				entry.MigrationNonce = target.Nonce
				j.Entries[id] = entry
			}
			j.Migrations[e.Version] = cloneMigration(a.Migration)
			if err = save(j); err != nil {
				t.Fatal(err)
			}
			if stage == "first-file" {
				if err = publishCron(CronPath(e.ID), []byte(CronContent(j.Entries[e.ID]))); err != nil {
					t.Fatal(err)
				}
			}
			u()
			other := e
			other.ID++
			other.Identity, other.MigrationNonce = "", ""
			db, err := sql.Open("sqlite", databasePath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err = db.Exec("INSERT INTO crons VALUES(?,1,?,'* * * * *','fluxo')", other.ID, other.Command); err != nil {
				t.Fatal(err)
			}
			legacy := "# Fluxo Cron ID: " + strconv.Itoa(other.ID) + "\n* * * * * fluxo cd " + other.Directory + " && " + other.Command + " >> /var/log/fluxo/cron-" + strconv.Itoa(other.ID) + ".log 2>&1\n"
			if err = publishCron(CronPath(other.ID), []byte(legacy)); err != nil {
				t.Fatal(err)
			}
			oldMutation(t, other, legacy, "materialize")
			if err = ReconcileBackendMigrations(ctx); err != nil {
				t.Fatal("partial publication recovery", err)
			}
			p, err = readBackendProofs()
			if err != nil {
				t.Fatal(err)
			}
			a = p.Branches[e.Version]
			if len(a.Migration.Replacements) != 2 || !a.Migration.Deletions[e.ID+9] || a.Certified || a.Preparing {
				t.Fatal("recovery lost obligations or invented readiness")
			}
			if len(a.PreviousAttempts) == 0 {
				t.Fatal("previous attempt history was discarded")
			}
			if _, err = os.Lstat(CronPath(e.ID + 9)); !os.IsNotExist(err) {
				t.Fatal("deleted trigger recreated")
			}
		})
	}
}
