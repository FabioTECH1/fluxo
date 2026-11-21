package maintenancetask

import (
	"context"
	"testing"

	"fluxo/internal/services/executiondomain"
)

func TestOptionalProfileNeedsCapableActualHelper(t *testing.T) {
	for _, marker := range []struct{ key, value string }{
		{"php-imagick-startup", imagickStartupProfile},
		{"certbot-directory-hook-contract", certbotDirectoryHookContract},
		{"certbot-directory-hook-contract", certbotDirectoryHookExtendedContract},
	} {
		p := newProfile(ComposerUpdate)
		p.External[marker.key] = marker.value
		need := profileHelperRequirements(p)
		if executiondomain.RequireCapabilities(executiondomain.Capabilities(), need) == nil {
			t.Fatal("older helper profile claim accepted optional semantics")
		}
		if err := executiondomain.RequireCapabilities(HelperCapabilities(), need); err != nil {
			t.Fatal(err)
		}
		if requireProfileHelper(context.Background(), "/does/not/exist", p) == nil {
			t.Fatal("panel capability substituted for the actual retained helper")
		}
	}
	if len(executiondomain.Capabilities().RoleContracts[optionalProfileRole]) != 0 {
		t.Fatal("optional execution feature changed the global reader floor")
	}
	if err := requireProfileHelper(context.Background(), "/does/not/exist", newProfile(ComposerUpdate)); err != nil {
		t.Fatal("unchanged profiles acquired an unrelated helper requirement", err)
	}
}

func TestExtendedHookCannotUseV1HelperClaim(t *testing.T) {
	old := HelperCapabilities()
	old.RoleContracts[optionalProfileRole] = []string{imagickStartupProfile, certbotDirectoryHookContract}
	p := newProfile(CertificateRenewal)
	p.External["certbot-directory-hook-contract"] = certbotDirectoryHookExtendedContract
	if executiondomain.RequireCapabilities(old, profileHelperRequirements(p)) == nil {
		t.Fatal("v1 helper authorized a hook grammar it cannot parse")
	}
	p.External["certbot-directory-hook-contract"] = certbotDirectoryHookContract
	if err := executiondomain.RequireCapabilities(old, profileHelperRequirements(p)); err != nil {
		t.Fatal("unchanged v1 hook acquired v2 requirements", err)
	}
}
