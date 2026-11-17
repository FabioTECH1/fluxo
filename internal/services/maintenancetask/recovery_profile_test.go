package maintenancetask

import "testing"

func TestRecoveryObservationPresenceIsNotAnEmptyValue(t *testing.T) {
	for _, field := range []string{"file", "directory", "external"} {
		before, after := newProfile(PackageCleanup), newProfile(PackageCleanup)
		switch field {
		case "file":
			before.Files["/unknown"] = ""
		case "directory":
			before.Directories["/unknown"] = nil
		case "external":
			before.External["unknown"] = ""
		}
		if compatibleRecoveryObservations(before, after, false) || compatibleRecoveryObservations(after, before, false) {
			t.Fatal("missing observation accepted as an empty value", field)
		}
	}
}

func TestRecoveryPreservesOnlyEstablishedDataAndBootExceptions(t *testing.T) {
	before, after := newProfile(ComposerUpdate), newProfile(ComposerUpdate)
	before.Files["/root/.cache/composer/old"] = "old-data"
	if !compatibleRecoveryObservations(before, after, false) {
		t.Fatal("permitted Composer cache effect rejected")
	}
	before.Files["/usr/local/bin/composer"] = "old-code"
	if compatibleRecoveryObservations(before, after, false) {
		t.Fatal("missing Composer executable accepted")
	}
	before, after = newProfile(CertificateRenewal), newProfile(CertificateRenewal)
	before.External["nginx-master"], after.External["nginx-master"] = "old-boot", "new-boot"
	if !compatibleRecoveryObservations(before, after, true) || compatibleRecoveryObservations(before, after, false) {
		t.Fatal("master boot exception changed")
	}
	delete(after.External, "nginx-master")
	if compatibleRecoveryObservations(before, after, true) {
		t.Fatal("missing master anchor accepted after reboot")
	}
}

func TestMixedProfileMarkersDoNotSelectV1Recovery(t *testing.T) {
	p := newProfile(PackageCleanup)
	p.Files["/usr/bin/apt-config"] = "recorded"
	if !legacyV1Profile(p) {
		t.Fatal("baseline observation selector missing")
	}
	for _, marker := range []string{"dpkg-bootstrap-contract", "apt-execution-profile", "apt-host-input-contract", "apt-platform-profile", "certbot-platform-profile"} {
		p.External[marker] = ""
		if legacyV1Profile(p) {
			t.Fatal("empty mixed-generation marker acquired V1 semantics", marker)
		}
		delete(p.External, marker)
	}
}
