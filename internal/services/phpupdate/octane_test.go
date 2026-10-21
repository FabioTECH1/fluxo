package phpupdate

import "testing"

func TestUnqualifiedNativePackageChangeIsRejectedBeforeMaintenance(t *testing.T) {
	impact := Impact{Workers: []Consumer{{Octane: &OctaneDescriptor{Backend: "swoole"}}}}
	inv := Inventory{Version: "8.4", Changes: []Package{{Name: "php8.4-cli"}, {Name: "php8.4-common"}}}
	if err := validateNativeChanges(inv, impact); err != nil {
		t.Fatal(err)
	}
	inv.Changes = append(inv.Changes, Package{Name: "php8.4-swoole", Installed: "6.2.3-1", Candidate: "6.2.3-2"})
	if validateNativeChanges(inv, impact) == nil {
		t.Fatal("a native rebuild bypassed compatibility qualification")
	}
	impact.Workers[0].Octane.Backend = "roadrunner"
	if err := validateNativeChanges(inv, impact); err != nil {
		t.Fatal("unused Swoole extension incorrectly blocked RoadRunner", err)
	}
}
