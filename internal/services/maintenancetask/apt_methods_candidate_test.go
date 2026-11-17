package maintenancetask

import (
	"context"
	"strings"
	"testing"
	"time"
)

func aptMethodMetadataFixture(version string) ([]byte, []byte, map[string]bool, map[string]string) {
	regular, aliases, err := aptMethodPolicy(version)
	if err != nil {
		panic(err)
	}
	var sums, listing string
	for name, required := range regular {
		if !required {
			continue
		}
		path := aptMethodsDirectory + "/" + name
		listing += path + "\n"
		sums += strings.Repeat("a", 32) + "  " + strings.TrimPrefix(path, "/") + "\n"
	}
	for name := range aliases {
		listing += aptMethodsDirectory + "/" + name + "\n"
	}
	return []byte(sums), []byte(listing), regular, aliases
}

func TestAPTMethodCandidateUsesClosedPackageMembership(t *testing.T) {
	for _, version := range []string{"2.4.14", "2.8.3", "3.2.0", "2.4.15ubuntu0.1", "2.8.4ubuntu0.1", "3.2.1ubuntu0.1"} {
		sums, listing, regular, aliases := aptMethodMetadataFixture(version)
		if _, err := parseAPTMethodManifests(sums, listing, regular, aliases); err != nil {
			t.Fatal(err)
		}
		for name, changed := range map[string][]byte{
			"extra method":     append(append([]byte(nil), listing...), []byte(aptMethodsDirectory+"/unreviewed-wrapper\n")...),
			"duplicate method": append(append([]byte(nil), listing...), []byte(aptMethodsDirectory+"/http\n")...),
			"missing method":   []byte(strings.Replace(string(listing), aptMethodsDirectory+"/http\n", "", 1)),
			"alias traversal":  append(append([]byte(nil), listing...), []byte(aptMethodsDirectory+"/../methods/http\n")...),
		} {
			t.Run(version+"/"+name, func(t *testing.T) {
				if _, err := parseAPTMethodManifests(sums, changed, regular, aliases); err == nil {
					t.Fatal("unreviewed method inventory accepted")
				}
			})
		}
	}
}

func TestVMAPTCandidateMethodAndSourceInventory(t *testing.T) {
	if !aptProfileVMGuard() {
		t.Skip("explicit root opt-in on dedicated acceptance VM only")
	}
	p := newProfile(PackageCleanup)
	spec, err := selectAPTPlatform(&p)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err = packageInputs(ctx, &p, "apt"); err != nil {
		t.Fatal(err)
	}
	if err = inspectAPTMethodInventory(spec, &p); err != nil {
		t.Fatal(err)
	}
	if err = inspectAPTSourceInputs(&p); err != nil {
		t.Fatal(err)
	}
	t.Logf("read-only candidate inventory passed: Ubuntu %s APT %s, vendor method entries=%d, effective source URI count=%s; no dispatch authority granted", spec.release, spec.aptVersion, len(p.Directories[aptMethodsDirectory]), p.External["apt-source-uri-count"])
}
