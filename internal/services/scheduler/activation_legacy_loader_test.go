package scheduler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func priorBootActivationFixture() (journal, backendProofs, Entry, legacyHost, legacyHost) {
	j, p, e := loaderAdmissionFixture()
	j.Schema = 2
	j.Legacy = map[string]string{}
	e.Ready = true
	j.Entries[e.ID] = e
	a := p.Branches[e.Version]
	a.Host.Backend, a.Host.Group = "cronie-noble-1", "/system.slice/cronie.service"
	a.Host.Configuration = strings.Repeat("a", 64)
	a.Certified = true
	p.Branches[e.Version] = a
	current := a.Host
	current.Boot, current.PID, current.Start, current.GroupInode = uuid.NewString(), 99, "1234", 5678
	return j, p, e, a.Host, current
}

func TestPriorBootActivationPreservesEvidenceWithoutGrantingLoaderAuthority(t *testing.T) {
	for _, schema := range []int{2, 3, 4} {
		j, p, e, saved, current := priorBootActivationFixture()
		j.Schema = schema
		before, _ := json.Marshal([]any{j, p, e, saved, current})
		if err := validatePriorBootActivation(j, p, e, saved, current); err != nil {
			t.Fatal(schema, err)
		}
		a := p.Branches[e.Version]
		a.RetiredByBoot = uuid.NewString() // A later boot may follow retirement.
		p.Branches[e.Version] = a
		if err := validatePriorBootActivation(j, p, e, saved, current); err != nil {
			t.Fatal("retained natural retirement evidence refused", err)
		}
		a.RetiredByBoot = ""
		p.Branches[e.Version] = a
		after, _ := json.Marshal([]any{j, p, e, saved, current})
		if string(before) != string(after) {
			t.Fatal("conversion admission changed retained authority")
		}
		host, err := entryLoaderHost(j, p, e)
		if err != nil || host == nil || *host != saved || *host == current {
			t.Fatal("conversion admission replaced ordinary loader identity", host, err)
		}
		j.Schema = loaderEpochSchema
		if err := validatePriorBootActivation(j, p, e, saved, current); err == nil {
			t.Fatal("current format acquired historical conversion admission")
		}
	}
}

func TestPriorBootActivationRejectsUnresolvedOrChangedAuthority(t *testing.T) {
	for _, name := range []string{"same-boot", "provider", "configuration", "group", "malformed-current", "pending", "uncertified", "missing-proof", "wrong-proof", "not-received", "nonce", "binding", "hash", "missing-core", "deleted", "unready", "disabled", "legacy", "hold", "upgrade", "retirement", "established"} {
		t.Run(name, func(t *testing.T) {
			j, p, e, saved, current := priorBootActivationFixture()
			a := p.Branches[e.Version]
			target := a.Migration.Replacements[e.ID]
			switch name {
			case "same-boot":
				current.Boot = saved.Boot
			case "provider":
				current.Backend = jammyCronCandidateID
				current.Group = "/system.slice/cron.service"
			case "configuration":
				current.Configuration = strings.Repeat("b", 64)
			case "group":
				current.Group = "/system.slice/cron.service"
			case "malformed-current":
				current.PID = 0
			case "pending":
				a.Preparing = true
			case "uncertified":
				a.Certified = false
			case "wrong-proof":
				a.Host.PID++
			case "not-received":
				target.Received = false
			case "nonce":
				target.Nonce = uuid.NewString()
			case "binding":
				a.Bindings[e.ID] = uuid.NewString()
			case "hash":
				target.Hash = strings.Repeat("f", 64)
			case "missing-core":
				delete(j.Migrations, e.Version)
			case "deleted":
				a.Migration.Deletions[e.ID] = true
			case "unready":
				e.Ready = false
				j.Entries[e.ID] = e
			case "disabled":
				e.Enabled = false
				j.Entries[e.ID] = e
			case "legacy":
				j.Legacy[e.Version] = saved.Boot
			case "hold":
				j.Holds = map[string]string{e.Version: uuid.NewString()}
			case "upgrade":
				j.Upgrade = &upgradeLease{}
			case "retirement":
				j.HistoricalRetirement = &historicalRetirement{}
			case "established":
				j.LoaderEpochEstablished = true
			}
			a.Migration.Replacements[e.ID] = target
			p.Branches[e.Version] = a
			if name == "missing-proof" {
				delete(p.Branches, e.Version)
			}
			before, _ := json.Marshal([]any{j, p})
			if err := validatePriorBootActivation(j, p, e, saved, current); err == nil {
				t.Fatal("changed authority admitted for conversion")
			}
			after, _ := json.Marshal([]any{j, p})
			if string(before) != string(after) {
				t.Fatal("refusal changed retained evidence")
			}
		})
	}
}
