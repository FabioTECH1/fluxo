package nodeupdate

import (
	"context"
	"errors"
	"testing"
	"time"

	"fluxo/internal/services/nodetoolchain"
	"github.com/google/uuid"
)

func TestLegacyDiscoveryRequiresSeparateExplicitMigration(t *testing.T) {
	m := testManager(t)
	source := nodetoolchain.LegacySource{Schema: 1, Kind: "nodesource-apt-local-baseline", Version: "22.22.3", NPM: "10.9.8"}
	m.Inspect = func(context.Context) (nodetoolchain.OwnedRuntime, error) {
		return nodetoolchain.OwnedRuntime{}, nodetoolchain.ErrNotManaged
	}
	m.InspectLegacy = func(context.Context) (nodetoolchain.LegacySource, error) { return source, nil }
	m.Find = func(_ context.Context, version string) (nodetoolchain.Release, error) {
		if version != source.Version {
			t.Fatal(version)
		}
		return testRelease("22.23.4"), nil
	}
	m.Stage = func(_ context.Context, r nodetoolchain.Release, _ nodetoolchain.ProgressFunc) (nodetoolchain.Release, error) {
		return r, nil
	}
	o, err := m.Enqueue(context.Background(), "check", "", uuid.NewString(), "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.check(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	status, err := ReadStatus()
	if err != nil || !status.Snapshot.MigrationAvailable || status.Snapshot.UpdateAvailable || status.Snapshot.Notice || status.Snapshot.LegacySource == nil {
		t.Fatal("legacy runtime silently became managed/update eligible", status, err)
	}
	p := Plan{ConsumerPolicy: consumerPolicy, ID: uuid.NewString(), Kind: "migrate", CreatedAt: time.Now().Unix(), DiscoveryID: status.Snapshot.DiscoveryID, Migration: &source, After: *status.Snapshot.Candidate}
	if err = savePlan(p); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Enqueue(context.Background(), "apply", p.ID, uuid.NewString(), "admin", ""); !errors.Is(err, ErrChanged) {
		t.Fatal("ordinary apply accepted migration", err)
	}
	request := uuid.NewString()
	first, err := m.Enqueue(context.Background(), "migrate", p.ID, request, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.Enqueue(context.Background(), "migrate", p.ID, request, "admin", "")
	if err != nil || again.ID != first.ID {
		t.Fatal("migration replay duplicated or blocked", err)
	}
}

func TestMigrationDiscoveryFailsClosedOnLegacySourceDrift(t *testing.T) {
	m := testManager(t)
	calls := 0
	m.InspectLegacy = func(context.Context) (nodetoolchain.LegacySource, error) {
		calls++
		return nodetoolchain.LegacySource{Version: "22.22.3", NPM: "10.9.8", PackageVersion: string(rune('a' + calls))}, nil
	}
	m.Find = func(context.Context, string) (nodetoolchain.Release, error) { return testRelease("22.23.4"), nil }
	m.Stage = func(_ context.Context, r nodetoolchain.Release, _ nodetoolchain.ProgressFunc) (nodetoolchain.Release, error) {
		return r, nil
	}
	if _, err := m.discoverMigration(context.Background()); !errors.Is(err, ErrChanged) {
		t.Fatal("changed source published a migration", err)
	}
}

func TestMigrationDiscoveryAllowsSameVersionWithoutInventingBundledNPM(t *testing.T) {
	m := testManager(t)
	m.InspectLegacy = func(context.Context) (nodetoolchain.LegacySource, error) {
		return nodetoolchain.LegacySource{Version: "22.22.3", NPM: "10.9.8"}, nil
	}
	m.Find = func(context.Context, string) (nodetoolchain.Release, error) { return nodetoolchain.Release{}, nil }
	m.Authenticate = func(_ context.Context, v, npm string) (nodetoolchain.Release, error) {
		if npm != "" {
			t.Fatal("legacy npm asserted as signed archive npm", npm)
		}
		return testRelease(v), nil
	}
	m.Stage = func(_ context.Context, r nodetoolchain.Release, _ nodetoolchain.ProgressFunc) (nodetoolchain.Release, error) {
		return r, nil
	}
	s, err := m.discoverMigration(context.Background())
	if err != nil || s.Candidate == nil || s.Candidate.Version != "22.22.3" || s.Installed.Root != "" {
		t.Fatal(s, err)
	}
}
