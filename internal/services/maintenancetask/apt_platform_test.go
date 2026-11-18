package maintenancetask

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func TestAPTPlatformSelectionRequiresExactVendorLTSIdentity(t *testing.T) {
	for _, row := range []struct {
		release, codename, build string
		qualified                bool
	}{
		{"22.04", "jammy", "2.4.15ubuntu0.1", true}, {"24.04", "noble", "2.8.4ubuntu0.1", true}, {"26.04", "resolute", "3.2.1ubuntu0.1", true},
	} {
		raw := fmt.Sprintf("ID=ubuntu\nVERSION_ID=\"%s\"\nVERSION_CODENAME=%s\nUBUNTU_CODENAME=%s\n", row.release, row.codename, row.codename)
		p, err := aptPlatformForRelease([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if p.aptVersion != row.build || p.release != row.release || p.executionQualified != (row.qualified && (row.release == "24.04" || runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")) || p.inputRevision == "" || len(p.inputs) < 14 {
			t.Fatal("incorrect platform contract", p, err)
		}
		for _, bad := range []string{
			strings.Replace(raw, "ID=ubuntu", "ID=debian", 1),
			raw + "VERSION_ID=\"24.04\"\n",
			strings.Replace(raw, "VERSION_CODENAME="+row.codename, "VERSION_CODENAME=other", 1),
			strings.Replace(raw, "VERSION_ID=\""+row.release+"\"", "VERSION_ID=\"26.10\"", 1),
		} {
			if _, err := aptPlatformForRelease([]byte(bad)); err == nil {
				t.Fatal("unreviewed OS identity accepted")
			}
		}
	}
}

func TestAPTPlatformInputPoliciesAreSeparateAndImmutableInMeaning(t *testing.T) {
	if jammyAPTConfigInputs["01autoremove"].sha256 == nobleAPTConfigInputs["01autoremove"].sha256 {
		t.Fatal("Jammy protection policy silently treated as Noble")
	}
	if resoluteAPTConfigInputs["20packagekit"].sha256 == nobleAPTConfigInputs["20packagekit"].sha256 {
		t.Fatal("changed Resolute hook text silently treated as Noble")
	}
	for _, inputs := range []map[string]aptConfigInput{jammyAPTConfigInputs, nobleAPTConfigInputs, resoluteAPTConfigInputs} {
		if !inputs["01-vendor-ubuntu"].required || !inputs["01autoremove"].required {
			t.Fatal("core default input became optional")
		}
	}
}

func TestAPTSourceProtocolCandidateDoesNotLeakURIs(t *testing.T) {
	for _, source := range []string{
		"deb [arch=amd64 signed-by=/etc/apt/keyrings/vendor.gpg] https://user:private-token@example.test/ubuntu noble main\n",
		"deb file:/srv/apt ./\n",
	} {
		if n, err := validateAPTClassicSources([]byte(source)); err != nil || n != 1 {
			t.Fatal("standard source candidate refused", err, n)
		}
	}
	for _, source := range []string{
		"deb ssh://user:private-token@example.test/repo noble main\n",
		"deb mirror+https://example.test/repo noble main\n",
		"deb https://example.test/%0ainjected noble main\n",
		"deb [custom-exec=/root/helper] https://example.test/repo noble main\n",
	} {
		_, err := validateAPTClassicSources([]byte(source))
		if err == nil {
			t.Fatal("unreviewed source protocol/format accepted")
		}
		if strings.Contains(err.Error(), "private-token") || strings.Contains(err.Error(), "example.test") {
			t.Fatal("URI or credentials leaked in diagnostic")
		}
	}
	valid := "Types: deb deb-src\nURIs: https://example.test/ubuntu\n https://second.test/ubuntu\nSuites: noble noble-updates\nComponents: main\nSigned-By:\n -----BEGIN PGP PUBLIC KEY BLOCK-----\n .\n public-key-data\n -----END PGP PUBLIC KEY BLOCK-----\n"
	if n, err := validateAPTDeb822Sources([]byte(valid)); err != nil || n != 2 {
		t.Fatal("standard Deb822 source refused", err, n)
	}
	for _, bad := range []string{valid + "URIs: https://third.test/ubuntu\n", strings.Replace(valid, "https://example.test/ubuntu", "ssh://private-token@example.test/repo", 1), valid + "Unknown-Execution-Field: /root/helper\n"} {
		if _, err := validateAPTDeb822Sources([]byte(bad)); err == nil {
			t.Fatal("ambiguous Deb822 source accepted")
		}
	}
	if n, err := validateAPTDeb822Sources([]byte("Types: deb\nEnabled: No\nURIs: ssh://disabled.example/repo\n")); err != nil || n != 0 {
		t.Fatal("disabled source granted authority or was needlessly interpreted", err, n)
	}
}
