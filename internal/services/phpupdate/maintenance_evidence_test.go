package phpupdate

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestNaturalEvidenceUsesOnlyExactApprovedOrderingReferences(t *testing.T) {
	id := uuid.NewString()
	units := []MaintenanceRuntime{{Name: unitName(9, 2), Natural: true}, {Name: unitName(2, 1), Natural: true}, {Name: "php8.4-fpm.service"}}
	e, err := evidenceContract("node", id, units)
	if err != nil || !slices.Equal(e.Units, []string{unitName(2, 1), unitName(9, 2)}) {
		t.Fatal(e, err)
	}
	if err = e.validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []struct {
		owner, id string
		units     []MaintenanceRuntime
	}{
		{"foreign", id, units}, {"node", "../escape", units},
		{"node", id, []MaintenanceRuntime{{Name: "arbitrary.service", Natural: true}}},
		{"node", id, []MaintenanceRuntime{units[0], units[0]}},
		{"node", id, []MaintenanceRuntime{{Name: "fluxo-daemon-1.service other.service", Natural: true}}},
	} {
		if _, err := evidenceContract(bad.owner, bad.id, bad.units); err == nil {
			t.Fatal("unsafe reference accepted", bad)
		}
	}
	p := map[string]string{"Id": e.name(), "LoadState": "loaded", "ActiveState": "active", "FragmentPath": e.path(), "NeedDaemonReload": "no", "After": unitName(9, 2) + " " + unitName(2, 1), "UnitFileState": "static", "DefaultDependencies": "no", "StopWhenUnneeded": "no"}
	if err := e.verifyProperties(p, true); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"After": unitName(2, 1), "Wants": unitName(9, 2), "Requires": unitName(9, 2), "PartOf": "fluxo.service", "DropInPaths": "/etc/foreign.conf", "FragmentPath": "/run/foreign.target", "UnitFileState": "enabled", "ActiveState": "inactive", "DefaultDependencies": "yes", "StopWhenUnneeded": "yes", "NeedDaemonReload": "yes"} {
		old := p[key]
		p[key] = value
		if err := e.verifyProperties(p, true); err == nil {
			t.Fatal("changed or missing retention accepted", key, value)
		}
		p[key] = old
	}
}

func TestNaturalEvidencePersistsOwnershipBeforeAnySystemdEffect(t *testing.T) {
	ctx := context.Background()
	id := uuid.NewString()
	units := []MaintenanceRuntime{{Name: unitName(123, 1), Natural: true}}
	var receipt *MaintenanceEvidence
	failed := errors.New("injected journal failure")
	saved := false
	err := HoldMaintenanceEvidence(ctx, "php", id, units, &receipt, func() error { saved = true; return failed })
	if !saved || !errors.Is(err, failed) || receipt == nil || receipt.State != "preparing" {
		t.Fatal(saved, receipt, err)
	}
	if _, err := os.Lstat(receipt.path()); !os.IsNotExist(err) {
		t.Fatal("reference created despite failed journal", err)
	}
	if err := HoldMaintenanceEvidence(ctx, "node", id, units, &receipt, func() error { t.Fatal("foreign owner reached save"); return nil }); err == nil {
		t.Fatal("foreign ownership accepted")
	}
	if err := HoldMaintenanceEvidence(ctx, "php", id, nil, &receipt, func() error { t.Fatal("changed inventory reached save"); return nil }); err == nil {
		t.Fatal("lost reference accepted")
	}
	if err := ReleaseMaintenanceEvidence(ctx, "node", id, receipt, func() error { t.Fatal("foreign release reached save"); return nil }); err == nil {
		t.Fatal("foreign release accepted")
	}
	if err := verifyNaturalEvidence(ctx, nil, units[0].Name); err == nil {
		t.Fatal("dispatch without evidence retention accepted")
	}
}
