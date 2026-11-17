package maintenancetask

import (
	"context"
	"testing"
)

func TestCertbotCompatibilityRejectsUnreviewedReleaseAndInterpreter(t *testing.T) {
	for _, row := range []struct{ release, python, certbot string }{
		{"22.04", "3.10", "1.21.0-"}, {"24.04", "3.12", "2.9.0-"}, {"26.04", "3.14", "4.0.0-"},
	} {
		python, certbot, err := certbotCompatibility(row.release)
		if err != nil || python != row.python || certbot != row.certbot {
			t.Fatal("reviewed vendor pair changed", row.release, python, certbot, err)
		}
	}
	for _, release := range []string{"", "20.04", "22.10", "26.10", "26.04.1", "debian"} {
		if _, _, err := certbotCompatibility(release); err == nil {
			t.Fatal("unreviewed release admitted", release)
		}
	}
	for _, minor := range []string{"", "3.11", "3.14t", "3.15", "3.10/../../bin/sh"} {
		p := newProfile(CertificateRenewal)
		if err := qualifyPython(context.Background(), &p, minor); err == nil || len(p.Files) != 0 {
			t.Fatal("unsupported interpreter reached host inspection", minor, err)
		}
	}
}

func TestCertbotNamespaceShimsStayExactAndReleaseScoped(t *testing.T) {
	for name, owner := range map[string]string{
		"zope.component-4.3.0-nspkg.pth": "python3-zope.component",
		"zope.event-4.4-nspkg.pth":       "python3-zope.event",
		"zope.hookable-5.1.0-nspkg.pth":  "python3-zope.hookable",
		"zope.interface-5.4.0-nspkg.pth": "python3-zope.interface",
	} {
		if certbotNamespacePackage("3.10", name) != owner || certbotNamespacePackage("3.14", name) != "" || certbotNamespacePackage("3.12", name) != "" {
			t.Fatal("namespace admission crossed a reviewed release", name)
		}
	}
	if certbotNamespacePackage("3.12", "zope.interface-6.1-nspkg.pth") != "python3-zope.interface" {
		t.Fatal("existing Noble namespace shim lost")
	}
	for _, name := range []string{"zope.interface-5.5.0-nspkg.pth", "zope.interface-5.4.0-other.pth", "custom.pth", "../zope.interface-5.4.0-nspkg.pth"} {
		if certbotNamespacePackage("3.10", name) != "" {
			t.Fatal("unreviewed executable startup addition admitted", name)
		}
	}
}
