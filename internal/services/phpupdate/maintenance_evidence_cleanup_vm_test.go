package phpupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// No reboot or package transaction: model the exact static/inactive target
// boundary and invoke the production PHP cleanup caller on fresh durable reads.
func TestVMVerifiedCleanupReleasesInactiveEvidenceWithoutDispatch(t *testing.T) {
	requireRecoveryVM(t)
	m := testStore(t)
	ctx := context.Background()
	const name = "fluxo-daemon-199092.service"
	vmUnit(t, name, "/usr/bin/true", "/")
	u := unitSnapshot{Name: name, Natural: true, State: "inactive"}
	g := vmGeneration(t, m, []unitSnapshot{u})
	if err := holdNaturalEvidence(ctx, &g); err != nil {
		t.Fatal(err)
	}
	// If an assertion fails, restore only this disposable reference for normal
	// test teardown. This is not a production recovery path or outcome proof.
	t.Cleanup(func() {
		fresh, err := getGeneration(g.ID)
		if err == nil && fresh.NaturalEvidence != nil && fresh.NaturalEvidence.State == "held" {
			if err = holdNaturalEvidence(ctx, &fresh); err != nil {
				t.Error(err)
			}
		}
	})
	vmRun(t, "/usr/bin/systemctl", "stop", g.NaturalEvidence.name())
	if err := verifyNaturalEvidence(ctx, g.NaturalEvidence, name); !errors.Is(err, errEvidenceInactive) {
		t.Fatal("inactive reference accepted for dispatch or unconfirmed result reads", err)
	}
	if err := m.releaseBarriers(ctx, g); err == nil {
		t.Fatal("unverified inactive cleanup accepted")
	}
	g.State, g.VerifiedGoal = "releasing", "after"
	if err := m.releaseBarriers(ctx, g); err == nil {
		t.Fatal("unsaved verification accepted")
	}
	if err := saveGeneration(g); err != nil {
		t.Fatal(err)
	}
	if err := ReleaseMaintenanceEvidence(ctx, "php", uuid.NewString(), g.NaturalEvidence, func() error { t.Fatal("foreign generation reached authorization"); return nil }, func() error { return saveGeneration(g) }); err == nil {
		t.Fatal("foreign cleanup accepted")
	}
	path, content := g.NaturalEvidence.path(), g.NaturalEvidence.content()
	if err := os.WriteFile(path, []byte(content+"Wants="+name+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	vmRun(t, "/usr/bin/systemctl", "daemon-reload")
	if err := m.releaseBarriers(ctx, g); err == nil {
		t.Fatal("replaced inactive reference accepted")
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	dir := path + ".d"
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(dir) })
	dropIn := filepath.Join(dir, "audit.conf")
	if err := os.WriteFile(dropIn, []byte("[Unit]\nWants="+name+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(dropIn) })
	vmRun(t, "/usr/bin/systemctl", "daemon-reload")
	if err := m.releaseBarriers(ctx, g); err == nil {
		t.Fatal("foreign inactive override accepted")
	}
	if err := os.Remove(dropIn); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	vmRun(t, "/usr/bin/systemctl", "daemon-reload")
	// Simulate a crash after removing exact unit files, before row cleanup.
	if err := m.prepareBarrier(ctx, g, u); err != nil {
		t.Fatal(err)
	}
	b, err := getBarrier(name)
	if err != nil {
		t.Fatal(err)
	}
	b.State = "releasing"
	if err = saveBarrier(b); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{b.Marker, b.Path} {
		if err = os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.Remove(filepath.Dir(b.Path)) })
	vmRun(t, "/usr/bin/systemctl", "daemon-reload")
	for i := 0; i < 2; i++ {
		fresh, e := getGeneration(g.ID)
		if e != nil {
			t.Fatal(e)
		}
		if e = m.releaseBarriers(ctx, fresh); e != nil {
			t.Fatal("fresh verified inactive cleanup", i, e)
		}
	}
	fresh, err := getGeneration(g.ID)
	if err != nil || fresh.NaturalEvidence.State != "released" {
		t.Fatal("cleanup receipt not durable", fresh.NaturalEvidence, err)
	}
	if _, err = os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("inactive reference not removed", err)
	}
	p := unitProperties(vmRun(t, "/usr/bin/systemctl", "show", name, "--property=ActiveState,ExecMainPID"))
	if p["ActiveState"] != "inactive" || p["ExecMainPID"] != "0" {
		t.Fatal("cleanup dispatched inert stopped consumer", p)
	}
}
