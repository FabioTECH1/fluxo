package scheduler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func loaderStoreFixture() (loaderEpochStore, journal, Entry) {
	j, p, e := loaderAdmissionFixture()
	j.Schema = loaderEpochSchema
	j.LoaderEpochEstablished = false
	e.Ready = true
	j.Entries[e.ID] = e
	before, _ := json.Marshal(j)
	e.LoaderNonce = uuid.NewString()
	j.Entries = cloneLoaderEntries(j.Entries)
	j.Entries[e.ID] = e
	j.LoaderEpochEstablished = true
	after, _ := json.Marshal(j)
	provider := LoaderProvider{Identity: p.Branches[e.Version].Host, Profile: "test-static-policy", PolicyHash: strings.Repeat("a", 64), RuntimeHash: strings.Repeat("b", 64)}
	target := LoaderTarget{Binding: e.Identity, Nonce: e.LoaderNonce, TriggerHash: hashBytes([]byte(CronContent(e)))}
	epoch := LoaderEpoch{ID: uuid.NewString(), Provider: provider, Targets: map[int]LoaderTarget{e.ID: target}, Phase: "prepared"}
	plan := LoaderPublicationPlan{ID: uuid.NewString(), EpochID: epoch.ID, Epoch: epoch, OriginalCore: before, IntendedCore: after, Triggers: map[int]loaderTriggerPublication{e.ID: {Original: []byte("exact original launcher"), Intended: []byte(CronContent(e))}}, Phase: "intent"}
	s := emptyLoaderStore()
	s.Current = epoch.ID
	s.Epochs[epoch.ID] = epoch
	s.Publications[plan.ID] = plan
	s.Pending = plan.ID
	return s, j, e
}

func loaderFixturePublished(s loaderEpochStore) loaderEpochStore {
	raw, _ := json.Marshal(s)
	var detached loaderEpochStore
	_ = json.Unmarshal(raw, &detached) // separate map backing stores as well as values
	s = detached
	e := s.Epochs[s.Current]
	e.Phase = "published"
	for id, target := range e.Targets {
		target.Received = true
		e.Targets[id] = target
	}
	s.Epochs[e.ID] = e
	p := s.Publications[s.Pending]
	p.Phase = "committed"
	s.Publications[p.ID] = p
	s.Pending = ""
	return s
}

func TestLoaderEpochIntentAndAcknowledgementAreSeparate(t *testing.T) {
	s, j, e := loaderStoreFixture()
	raw, _ := json.Marshal(s)
	if _, err := decodeLoaderStore(raw); err != nil {
		t.Fatal("valid durable intent refused", err)
	}
	if _, err := loaderEntryBinding(j, s, e); err == nil {
		t.Fatal("prepared publication granted authority")
	}
	s = loaderFixturePublished(s)
	raw, _ = json.Marshal(s)
	if _, err := decodeLoaderStore(raw); err != nil {
		t.Fatal("published acknowledged epoch refused", err)
	}
	b, err := loaderEntryBinding(j, s, e)
	if err != nil || b.Nonce != e.LoaderNonce || b.Binding != e.Identity {
		t.Fatal(b, err)
	}
	// Retirement fields do not stand in for the current generation's receipt.
	epoch := s.Epochs[s.Current]
	target := epoch.Targets[e.ID]
	target.Received = false
	epoch.Targets[e.ID] = target
	s.Epochs[epoch.ID] = epoch
	if _, err = loaderEntryBinding(j, s, e); err == nil {
		t.Fatal("historical Ready/receipt admitted an unreceived epoch")
	}
}

func TestLoaderPublicationCannotRewriteRetirementOrChildren(t *testing.T) {
	for _, name := range []string{"migration-nonce", "legacy-boot", "ready", "migration-history", "invocations", "loader-nonce", "target-binding", "target-received", "unknown-field", "lost-plan", "lost-predecessor"} {
		t.Run(name, func(t *testing.T) {
			s, j, e := loaderStoreFixture()
			p := s.Publications[s.Pending]
			switch name {
			case "migration-nonce":
				e.MigrationNonce = uuid.NewString()
				j.Entries[e.ID] = e
			case "legacy-boot":
				e.LegacyBoot = uuid.NewString()
				j.Entries[e.ID] = e
			case "ready":
				e.Ready = !e.Ready
				j.Entries[e.ID] = e
			case "migration-history":
				j.Migrations = map[string]legacyMigration{}
			case "invocations":
				j.Invocations = map[string]invocation{"retained-child": {Unit: "retained-child"}}
			case "loader-nonce":
				e.LoaderNonce = uuid.NewString()
				j.Entries[e.ID] = e
			case "target-binding":
				epoch := s.Epochs[s.Current]
				v := epoch.Targets[e.ID]
				v.Binding = uuid.NewString()
				epoch.Targets[e.ID] = v
				s.Epochs[epoch.ID] = epoch
			case "target-received":
				epoch := s.Epochs[s.Current]
				v := epoch.Targets[e.ID]
				v.Received = true
				epoch.Targets[e.ID] = v
				s.Epochs[epoch.ID] = epoch
			case "lost-plan":
				delete(s.Publications, s.Pending)
			case "lost-predecessor":
				epoch := s.Epochs[s.Current]
				epoch.Predecessor = uuid.NewString()
				s.Epochs[epoch.ID] = epoch
			}
			if name != "lost-plan" {
				p.IntendedCore, _ = json.Marshal(j)
				s.Publications[p.ID] = p
			}
			raw, _ := json.Marshal(s)
			if name == "unknown-field" {
				var doc map[string]any
				_ = json.Unmarshal(raw, &doc)
				doc["future_authority"] = "must survive by refusal"
				raw, _ = json.Marshal(doc)
			}
			if _, err := decodeLoaderStore(raw); err == nil {
				t.Fatal("corrupt or lossy loader history accepted")
			}
		})
	}
}

func TestLoaderBindingRejectsChangedGenerationAndEntry(t *testing.T) {
	for _, name := range []string{"disabled", "recreated", "nonce", "missing-core-anchor", "pending", "not-received", "trigger-hash"} {
		t.Run(name, func(t *testing.T) {
			s, j, e := loaderStoreFixture()
			s = loaderFixturePublished(s)
			switch name {
			case "disabled":
				v := j.Entries[e.ID]
				v.Enabled = false
				j.Entries[e.ID] = v
			case "recreated":
				v := j.Entries[e.ID]
				v.Identity = uuid.NewString()
				j.Entries[e.ID] = v
			case "nonce":
				v := j.Entries[e.ID]
				v.LoaderNonce = uuid.NewString()
				j.Entries[e.ID] = v
			case "missing-core-anchor":
				j.LoaderEpochEstablished = false
			case "pending":
				s.Pending = uuid.NewString()
			case "not-received":
				epoch := s.Epochs[s.Current]
				v := epoch.Targets[e.ID]
				v.Received = false
				epoch.Targets[e.ID] = v
				s.Epochs[epoch.ID] = epoch
			case "trigger-hash":
				epoch := s.Epochs[s.Current]
				v := epoch.Targets[e.ID]
				v.TriggerHash = strings.Repeat("b", 64)
				epoch.Targets[e.ID] = v
				s.Epochs[epoch.ID] = epoch
			}
			if _, err := loaderEntryBinding(j, s, e); err == nil {
				t.Fatal("changed current loader authority accepted")
			}
		})
	}
}

func TestLoaderPolicyExcludesOnlyClassifiedRuntimeEnvironment(t *testing.T) {
	first := []byte("PATH=/usr/bin:/bin\x00INVOCATION_ID=" + strings.Repeat("a", 32) + "\x00SYSTEMD_EXEC_PID=123\x00JOURNAL_STREAM=9:100\x00CUSTOM_CONFIGURATION=preserve\x00")
	second := []byte("PATH=/usr/bin:/bin\x00INVOCATION_ID=" + strings.Repeat("b", 32) + "\x00SYSTEMD_EXEC_PID=456\x00JOURNAL_STREAM=9:200\x00CUSTOM_CONFIGURATION=preserve\x00")
	a, err := loaderStableEnvironment(first, 123)
	if err != nil {
		t.Fatal(err)
	}
	b, err := loaderStableEnvironment(second, 456)
	if err != nil || a != b {
		t.Fatal("runtime generation altered stable policy", err)
	}
	changed := []byte(strings.Replace(string(second), "CUSTOM_CONFIGURATION=preserve", "CUSTOM_CONFIGURATION=changed", 1))
	c, err := loaderStableEnvironment(changed, 456)
	if err != nil || c == a {
		t.Fatal("unknown configuration variable silently excluded", err)
	}
	for _, bad := range []string{"INVOCATION_ID=not-a-generation\x00", "SYSTEMD_EXEC_PID=999\x00", "JOURNAL_STREAM=not:a:stream\x00", "INVOCATION_ID=" + strings.Repeat("a", 32) + "\x00INVOCATION_ID=" + strings.Repeat("b", 32) + "\x00"} {
		if _, err := loaderStableEnvironment([]byte(bad), 123); err == nil {
			t.Fatal("ambiguous runtime metadata accepted")
		}
	}
}

func TestLoaderPublicationRecoveryAcceptsOnlyExactCrashSides(t *testing.T) {
	p := LoaderPublicationPlan{OriginalCore: []byte("original core"), IntendedCore: []byte("intended core"), Triggers: map[int]loaderTriggerPublication{1: {Original: []byte("old-one"), Intended: []byte("new-one")}, 2: {Absent: true, Intended: []byte("new-two")}}}
	for mask := 0; mask < 8; mask++ {
		core := p.OriginalCore
		if mask&1 != 0 {
			core = p.IntendedCore
		}
		one := loaderObservedTrigger{Data: p.Triggers[1].Original}
		if mask&2 != 0 {
			one.Data = p.Triggers[1].Intended
		}
		two := loaderObservedTrigger{Absent: true}
		if mask&4 != 0 {
			two = loaderObservedTrigger{Data: p.Triggers[2].Intended}
		}
		if err := validateLoaderRecoveryState(p, core, map[int]loaderObservedTrigger{1: one, 2: two}); err != nil {
			t.Fatal("exact interrupted publication refused", mask, err)
		}
	}
	for _, observed := range []map[int]loaderObservedTrigger{
		{1: {Data: []byte("operator change")}, 2: {Absent: true}},
		{1: {Absent: true}, 2: {Absent: true}},
		{1: {Data: p.Triggers[1].Original}, 2: {Data: []byte("unexpected existing file")}},
		{1: {Data: p.Triggers[1].Original}},
	} {
		if err := validateLoaderRecoveryState(p, p.OriginalCore, observed); err == nil {
			t.Fatal("recovery overwrote unrecorded trigger state")
		}
	}
	if err := validateLoaderRecoveryState(p, []byte("other legitimate core mutation"), map[int]loaderObservedTrigger{1: {Data: p.Triggers[1].Original}, 2: {Absent: true}}); err == nil {
		t.Fatal("recovery overwrote unrelated core mutation or children")
	}
}

func TestEstablishedLoaderStateCannotBecomeFreshAfterStorageLoss(t *testing.T) {
	s, j, _ := loaderStoreFixture()
	s = loaderFixturePublished(s)
	if err := validateLoaderPublicationCoreState(j, s); err != nil {
		t.Fatal(err)
	}
	if err := validateLoaderPublicationCoreState(j, emptyLoaderStore()); err == nil {
		t.Fatal("lost established store reinterpreted as fresh")
	}
	j.LoaderEpochEstablished = false
	if err := validateLoaderPublicationCoreState(j, s); err == nil {
		t.Fatal("established store adopted into a fresh core")
	}
	if err := validateLoaderPublicationCoreState(j, emptyLoaderStore()); err != nil {
		t.Fatal("explicit fence-only fresh core refused", err)
	}
	s.Pending = uuid.NewString()
	if err := validateLoaderPublicationCoreState(j, s); err == nil {
		t.Fatal("pending publication admitted ordinary core writer")
	}
}

func TestLoaderStoreCannotInferOmittedAuthorityBits(t *testing.T) {
	for _, name := range []string{"received", "received-null", "publication-received", "absent", "pending", "current"} {
		t.Run(name, func(t *testing.T) {
			s, _, _ := loaderStoreFixture()
			raw, _ := json.Marshal(s)
			var doc map[string]any
			_ = json.Unmarshal(raw, &doc)
			epoch := doc["epochs"].(map[string]any)[s.Current].(map[string]any)
			plan := doc["publications"].(map[string]any)[s.Pending].(map[string]any)
			switch name {
			case "received":
				delete(epoch["targets"].(map[string]any)["1"].(map[string]any), "received")
			case "received-null":
				epoch["targets"].(map[string]any)["1"].(map[string]any)["received"] = nil
			case "publication-received":
				delete(plan["epoch"].(map[string]any)["targets"].(map[string]any)["1"].(map[string]any), "received")
			case "absent":
				delete(plan["triggers"].(map[string]any)["1"].(map[string]any), "absent")
			case "pending":
				delete(doc, "pending")
			case "current":
				delete(doc, "current")
			}
			raw, _ = json.Marshal(doc)
			if _, err := decodeLoaderStore(raw); err == nil {
				t.Fatal("omitted/null authority inferred from a zero value")
			}
		})
	}
}

func TestNobleLoaderPolicyNeverAliasesHistoricalBackend(t *testing.T) {
	c, err := distributionCandidate(nobleLoaderPAMProfileID)
	if err != nil || c.release != "24.04" {
		t.Fatal(err)
	}
	if newDistributionBackend(nobleLoaderPAMProfileID) {
		t.Fatal("loader policy was exposed as a new persisted backend identity")
	}
	for _, contract := range BackendReaderContracts() {
		if contract == nobleLoaderPAMProfileID {
			t.Fatal("loader profile replaced an immutable backend reader meaning")
		}
	}
	h := proofFixture().Branches["8.4"].Host
	if h.Backend != "" || BackendReaderContract(h) != legacyNobleCronContract || ValidateBackendIdentity(h) != nil {
		t.Fatal("historical Noble identity meaning changed")
	}
}

func TestLoaderArchiveCompactionPreservesEveryEpochWithoutHistoryRewrites(t *testing.T) {
	s, j, e := loaderStoreFixture()
	s = loaderFixturePublished(s)
	for generation := 0; generation < 8; generation++ {
		oldID := s.Current
		old := s.Epochs[oldID]
		var oldPlan LoaderPublicationPlan
		for _, p := range s.Publications {
			if p.EpochID == oldID {
				oldPlan = p
			}
		}
		archived, _ := json.Marshal(loaderArchivedPair{Format: 1, Epoch: old, Publication: oldPlan})
		before, _ := json.Marshal(j)
		e.LoaderNonce = uuid.NewString()
		j.Entries = cloneLoaderEntries(j.Entries)
		j.Entries[e.ID] = e
		after, _ := json.Marshal(j)
		target := LoaderTarget{Binding: e.Identity, Nonce: e.LoaderNonce, TriggerHash: hashBytes([]byte(CronContent(e)))}
		next := LoaderEpoch{ID: uuid.NewString(), Provider: old.Provider, Predecessor: oldID, Targets: map[int]LoaderTarget{e.ID: target}, Phase: "prepared"}
		p := LoaderPublicationPlan{ID: uuid.NewString(), EpochID: next.ID, Epoch: next, OriginalCore: before, IntendedCore: after, Triggers: map[int]loaderTriggerPublication{e.ID: {Original: oldPlan.Triggers[e.ID].Intended, Intended: []byte(CronContent(e))}}, Phase: "intent"}
		s.Current = next.ID
		s.Pending = p.ID
		s.Epochs[next.ID] = next
		s.Publications[p.ID] = p
		raw, _ := json.Marshal(s)
		if _, err := decodeLoaderStore(raw); err != nil {
			t.Fatal("durable pre-archive intent invalid", generation, err)
		}
		ref := loaderArchiveRef{PublicationID: oldPlan.ID, Predecessor: old.Predecessor, SHA256: hashBytes(archived), Device: 1, Inode: uint64(generation + 1), Size: int64(len(archived)), ModifiedNS: 1, ChangedNS: 1}
		if err := recordLoaderArchiveReference(&s, oldID, ref); err != nil {
			t.Fatal(err)
		}
		raw, _ = json.Marshal(s)
		if _, err := decodeLoaderStore(raw); err != nil {
			t.Fatal("compacted pending intent invalid", generation, err)
		}
		if len(s.Epochs) != 1 || len(s.Publications) != 1 || len(s.Archives) != generation+1 {
			t.Fatal("active ledger retained old full snapshots or lost archive references")
		}
		s = loaderFixturePublished(s)
	}
	raw, _ := json.Marshal(s)
	if _, err := decodeLoaderStore(raw); err != nil {
		t.Fatal(err)
	}
	for id, ref := range s.Archives {
		delete(s.Archives, id)
		bad, _ := json.Marshal(s)
		if _, err := decodeLoaderStore(bad); err == nil {
			t.Fatal("lost historical archive was silently detached")
		}
		s.Archives[id] = ref
		break
	}
}

func TestLoaderRestoreNormalizationChangesOnlyFilesystemIdentity(t *testing.T) {
	s := emptyLoaderStore()
	id := uuid.NewString()
	r := loaderArchiveRef{PublicationID: uuid.NewString(), SHA256: strings.Repeat("a", 64), Device: 1, Inode: 2, Size: 100, ModifiedNS: 3, ChangedNS: 4}
	s.Archives[id] = r
	other := s
	other.Archives = map[string]loaderArchiveRef{id: r}
	changed := other.Archives[id]
	changed.Inode = 99
	changed.ChangedNS = 100
	other.Archives[id] = changed
	a, _ := json.Marshal(loaderWithoutArchiveIdentities(s))
	b, _ := json.Marshal(loaderWithoutArchiveIdentities(other))
	if string(a) != string(b) {
		t.Fatal("physical restore identity changed logical history")
	}
	changed.SHA256 = strings.Repeat("b", 64)
	other.Archives[id] = changed
	b, _ = json.Marshal(loaderWithoutArchiveIdentities(other))
	if string(a) == string(b) {
		t.Fatal("checksum change normalized away as metadata")
	}
	if s.Archives[id] != r {
		t.Fatal("normalization mutated the source archive reference")
	}
}

func TestLoaderNewPublicationRespectsUpgradeAndMaintenanceHolds(t *testing.T) {
	j := journal{}
	if loaderPublicationMayStart(j) != nil {
		t.Fatal("unheld publication refused")
	}
	j.Upgrade = &upgradeLease{ID: uuid.NewString()}
	if loaderPublicationMayStart(j) == nil {
		t.Fatal("installer hold allowed a new publication")
	}
	j.Upgrade = nil
	j.Holds = map[string]string{"8.4": uuid.NewString()}
	if loaderPublicationMayStart(j) == nil {
		t.Fatal("maintenance hold allowed a new publication")
	}
}

func TestInitializedEmptySchedulerNeedsPublishedEmptyEpoch(t *testing.T) {
	s, j, e := loaderStoreFixture()
	s = loaderFixturePublished(s)
	row := j.Entries[e.ID]
	row.Enabled = false
	j.Entries[e.ID] = row
	if _, err := loaderEpochReadySnapshot(j, s); err == nil {
		t.Fatal("stale enabled targets were treated as an empty epoch")
	}
	epoch := s.Epochs[s.Current]
	epoch.Targets = map[int]LoaderTarget{}
	s.Epochs[epoch.ID] = epoch
	if _, err := loaderEpochReadySnapshot(j, s); err != nil {
		t.Fatal("explicit published empty-target readiness refused", err)
	}
	j.LoaderEpochEstablished = false
	if _, err := loaderEpochReadySnapshot(j, emptyLoaderStore()); err == nil {
		t.Fatal("missing storage inferred as verified empty readiness")
	}
}

func TestLoaderStoreRejectsNullMandatoryAuthorityFields(t *testing.T) {
	prepared, _, _ := loaderStoreFixture()
	published := loaderFixturePublished(prepared)
	archived := loaderFixturePublished(prepared)
	archiveID := uuid.NewString()
	old := archived.Epochs[archived.Current]
	old.Predecessor = archiveID
	archived.Epochs[old.ID] = old
	for id, p := range archived.Publications {
		p.Epoch.Predecessor = archiveID
		archived.Publications[id] = p
	}
	archived.Archives[archiveID] = loaderArchiveRef{PublicationID: uuid.NewString(), SHA256: strings.Repeat("c", 64), Device: 1, Inode: 2, Size: 10, ModifiedNS: 1, ChangedNS: 1}
	for name, store := range map[string]loaderEpochStore{"prepared": prepared, "published": published, "archived": archived} {
		raw, _ := json.Marshal(store)
		if _, err := decodeLoaderStore(raw); err != nil {
			t.Fatal("valid starting ledger", name, err)
		}
		var doc map[string]any
		_ = json.Unmarshal(raw, &doc)
		var paths [][]string
		var walk func(map[string]any, []string)
		walk = func(object map[string]any, prefix []string) {
			for key, value := range object {
				path := append(append([]string(nil), prefix...), key)
				paths = append(paths, path)
				if nested, ok := value.(map[string]any); ok {
					walk(nested, path)
				}
			}
		}
		walk(doc, nil)
		for _, path := range paths {
			t.Run(name+"/"+strings.Join(path, "/"), func(t *testing.T) {
				var mutated map[string]any
				_ = json.Unmarshal(raw, &mutated)
				parent := mutated
				for _, key := range path[:len(path)-1] {
					parent = parent[key].(map[string]any)
				}
				parent[path[len(path)-1]] = nil
				bad, _ := json.Marshal(mutated)
				if _, err := decodeLoaderStore(bad); err == nil {
					t.Fatal("null authority silently became absence or a zero value")
				}
			})
		}
	}
}

func TestLoaderStoreRetainsExplicitAbsentTriggerPayload(t *testing.T) {
	s, _, e := loaderStoreFixture()
	p := s.Publications[s.Pending]
	var source journal
	_ = json.Unmarshal(p.OriginalCore, &source)
	delete(source.Entries, e.ID)
	p.OriginalCore, _ = json.Marshal(source)
	trigger := p.Triggers[e.ID]
	trigger.Original = nil
	trigger.Absent = true
	p.Triggers[e.ID] = trigger
	s.Publications[p.ID] = p
	raw, _ := json.Marshal(s)
	if _, err := decodeLoaderStore(raw); err != nil {
		t.Fatal("explicit new-trigger absence must retain its nullable original payload", err)
	}
	trigger.Absent = false
	p.Triggers[e.ID] = trigger
	s.Publications[p.ID] = p
	raw, _ = json.Marshal(s)
	if _, err := decodeLoaderStore(raw); err == nil {
		t.Fatal("nullable original payload granted existing-trigger authority")
	}
}
