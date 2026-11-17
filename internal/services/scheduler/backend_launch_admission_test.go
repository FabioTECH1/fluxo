package scheduler

import (
	"context"
	"testing"

	"fluxo/internal/services/executiondomain"
	"github.com/google/uuid"
)

func loaderAdmissionFixture() (journal, backendProofs, Entry) {
	p := proofFixture()
	a := p.Branches["8.4"]
	e := Entry{ID: 1, SiteID: 2, Database: "/var/lib/fluxo/fluxo.db", Version: "8.4", Directory: "/home/fluxo/example", Command: "php8.4 /home/fluxo/example/artisan schedule:run", Identity: a.Bindings[1], Enabled: true, MigrationNonce: a.Migration.Replacements[1].Nonce}
	target := a.Migration.Replacements[1]
	target.Hash, target.Received = hashBytes([]byte(CronContent(e))), true
	a.Migration.Replacements[1] = target
	a.Preparing = false
	p.Branches[e.Version] = a
	j := journal{Schema: executiondomain.CurrentSchedulerSchema, Entries: map[int]Entry{e.ID: e}, Migrations: map[string]legacyMigration{e.Version: cloneMigration(a.Migration)}}
	return j, p, e
}

func TestSchedulerLoaderAcknowledgementDoesNotDemandWholeBranchDrain(t *testing.T) {
	j, p, e := loaderAdmissionFixture()
	// Entry.Ready and branch.Certified remain false while prior work drains.
	host, err := entryLoaderHost(j, p, e)
	if err != nil || host == nil || *host != p.Branches[e.Version].Host {
		t.Fatal(host, err)
	}
	a := p.Branches[e.Version]
	a.Certified = true
	p.Branches[e.Version] = a
	// Retirement of an unrelated registration must not turn the per-entry
	// receipt check into the whole-branch migration predicate.
	j.Entries[2] = Entry{ID: 2, Enabled: false}
	a.Migration.Replacements[2] = legacyTrigger{Nonce: uuid.NewString(), Received: true}
	p.Branches[e.Version] = a
	if _, err := entryLoaderHost(j, p, e); err != nil {
		t.Fatal("certified exact acknowledgement refused", err)
	}
}

func TestSchedulerCertifiedReceiptStillRejectsWrongOrRetiredNonce(t *testing.T) {
	j, p, _ := loaderAdmissionFixture()
	a := p.Branches["8.4"]
	for _, input := range []struct {
		id    int
		nonce string
	}{{1, uuid.NewString()}, {1, ""}, {0, a.Migration.Marker}, {2, uuid.NewString()}} {
		if err := verifyCertifiedEntryReceipt(context.Background(), j, input.nonce, input.id, &a.Host); err == nil {
			t.Fatal("certification bypassed receipt identity", input.id)
		}
	}
}

func TestSchedulerLoaderAcknowledgementRejectsStaleAuthority(t *testing.T) {
	for _, name := range []string{"not-received", "wrong-nonce", "wrong-hash", "wrong-binding", "preparing", "disabled", "recreated", "missing-core", "core-nonce", "deleted", "unknown-backend"} {
		t.Run(name, func(t *testing.T) {
			j, p, e := loaderAdmissionFixture()
			a := p.Branches[e.Version]
			target := a.Migration.Replacements[e.ID]
			switch name {
			case "not-received":
				target.Received = false
			case "wrong-nonce":
				target.Nonce = uuid.NewString()
			case "wrong-hash":
				target.Hash = hashBytes([]byte("changed"))
			case "wrong-binding":
				a.Bindings[e.ID] = uuid.NewString()
			case "preparing":
				a.Preparing = true
			case "disabled":
				current := e
				current.Enabled = false
				j.Entries[e.ID] = current
			case "recreated":
				current := e
				current.Identity = uuid.NewString()
				j.Entries[e.ID] = current
			case "missing-core":
				delete(j.Migrations, e.Version)
			case "core-nonce":
				m := j.Migrations[e.Version]
				v := m.Replacements[e.ID]
				v.Nonce = uuid.NewString()
				m.Replacements[e.ID] = v
				j.Migrations[e.Version] = m
			case "deleted":
				a.Migration.Deletions[e.ID] = true
			case "unknown-backend":
				a.Host.Backend = "unrecognized-provider"
			}
			a.Migration.Replacements[e.ID] = target
			p.Branches[e.Version] = a
			if _, err := entryLoaderHost(j, p, e); err == nil {
				t.Fatal("stale loader authority admitted")
			}
		})
	}
}

func TestSchedulerLegacyLoaderEvidenceNeedsExactNonceReceipt(t *testing.T) {
	j, p, e := loaderAdmissionFixture()
	a := p.Branches[e.Version]
	m := j.Migrations[e.Version]
	m.Host = &a.Host
	j.Migrations[e.Version] = m
	p.Branches = map[string]backendAttempt{}
	if _, err := entryLoaderHost(j, p, e); err != nil {
		t.Fatal(err)
	}
	target := m.Replacements[e.ID]
	target.Received = false
	m.Replacements[e.ID] = target
	j.Migrations[e.Version] = m
	if _, err := entryLoaderHost(j, p, e); err == nil {
		t.Fatal("legacy readiness replaced loader receipt")
	}
	e.MigrationNonce = ""
	e.Ready = true
	j.Entries[e.ID] = e
	if _, err := entryLoaderHost(j, p, e); err == nil {
		t.Fatal("current schema admitted nonce-less ready entry")
	}
	j.Schema = 2
	if host, err := entryLoaderHost(j, p, e); err != nil || host != nil {
		t.Fatal("historical nonce-less meaning changed", err)
	}
}
