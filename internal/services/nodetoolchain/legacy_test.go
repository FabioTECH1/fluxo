package nodetoolchain

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

func TestLegacyPackageQualificationIsExact(t *testing.T) {
	for _, tc := range []struct {
		name, record, arch string
		valid              bool
	}{
		{"nodesource", "nodejs\tinstalled\t22.22.3-1nodesource1\tamd64\n", "amd64", true},
		{"arm64", "nodejs\tinstalled\t22.22.3-1nodesource1\tarm64\n", "arm64", true},
		{"removed", "nodejs\tconfig-files\t22.22.3-1nodesource1\tamd64", "amd64", false},
		{"incomplete", "nodejs\tunpacked\t22.22.3-1nodesource1\tamd64", "amd64", false},
		{"foreign arch", "nodejs\tinstalled\t22.22.3-1nodesource1\tarm64", "amd64", false},
		{"ubuntu", "nodejs\tinstalled\t22.22.3-0ubuntu1\tamd64", "amd64", false},
		{"lookalike", "nodejs\tinstalled\t22.22.3-1nodesource1-custom\tamd64", "amd64", false},
		{"another package", "npm\tinstalled\t22.22.3-1nodesource1\tamd64", "amd64", false},
		{"duplicate rows", "nodejs\tinstalled\t22.22.3-1nodesource1\tamd64\nnodejs\tinstalled\t22.22.3-1nodesource1\tamd64", "amd64", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, revision, err := parseLegacyPackage(tc.record, tc.arch)
			if (err == nil) != tc.valid {
				t.Fatal(v, revision, err)
			}
			if tc.valid && (v != "22.22.3" || revision != "22.22.3-1nodesource1") {
				t.Fatal(v, revision)
			}
		})
	}
}

func TestMigrationVersionNeverDowngradesOrChangesMajor(t *testing.T) {
	for _, tc := range []struct {
		before, after string
		valid         bool
	}{{"22.22.3", "22.22.3", true}, {"22.22.3", "22.23.4", true}, {"22.22.3", "22.22.2", false}, {"22.22.3", "24.22.1", false}, {"20.19.0", "20.20.0", false}, {"22.22.3", "not-a-version", false}} {
		if err := ValidateMigrationVersion(tc.before, tc.after); (err == nil) != tc.valid {
			t.Fatalf("%s -> %s: %v", tc.before, tc.after, err)
		}
	}
}

func legacyReceiptFixture() LegacySource {
	h := strings.Repeat("a", 64)
	return LegacySource{Schema: 1, Kind: "nodesource-apt-local-baseline", Version: "22.22.3", NPM: "10.9.8", Architecture: runtime.GOARCH, PackageVersion: "22.22.3-1nodesource1", Node: LegacyFile{Path: "/usr/bin/node", SHA256: h, Inode: 1, Mode: 0755}, Native: []LegacyFile{{Path: "/usr/lib/fixture", SHA256: h}}, NPMTreeSHA256: h, PackageFilesSHA256: h, ManagerTreeSHA256: h, Links: map[string]LegacyLink{"node": {}, "npm": {}, "npx": {}}, State: []byte(`{"official_node":false,"corepack":true,"bun":true}`), StateMode: 0600, ManagerLinks: map[string]LegacyLink{"corepack": {Present: true, Target: managedRoot + "/bin/corepack"}, "pnpm": {Present: true, Target: managedRoot + "/bin/pnpm"}, "yarn": {Present: true, Target: managedRoot + "/bin/yarn"}, "bun": {Present: true, Target: managedRoot + "/bin/bun"}}, Managers: map[string]string{"corepack": "0.35.0", "pnpm": "11.20.0", "yarn": "4.18.0", "bun": "1.3.14"}}
}

func TestMigrationRejectsIncompleteOrAmbiguousRollbackReceipt(t *testing.T) {
	source := legacyReceiptFixture()
	if err := validLegacySource(source); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []func(*LegacySource){func(s *LegacySource) { s.Schema = 0 }, func(s *LegacySource) { s.Node.Path = "/tmp/node" }, func(s *LegacySource) { s.NPMTreeSHA256 = "" }, func(s *LegacySource) { delete(s.Links, "node") }, func(s *LegacySource) { s.Links["npm"] = LegacyLink{Present: true, Target: "/tmp/npm"} }, func(s *LegacySource) { s.State = []byte(`{"corepack":true,"bun":true,"unknown":true}`) }, func(s *LegacySource) { s.StateMode = 0666 }, func(s *LegacySource) { s.Managers["bun"] = "" }, func(s *LegacySource) { s.Node.Inode = 0 }} {
		b, _ := json.Marshal(source)
		var changed LegacySource
		_ = json.Unmarshal(b, &changed)
		mutation(&changed)
		if err := validLegacySource(changed); err == nil {
			t.Fatalf("unsafe receipt accepted: %+v", changed)
		}
	}
}

func TestLegacyBaselineIsNotAnOfficialReleaseProof(t *testing.T) {
	source := legacyReceiptFixture()
	if err := ValidateReleaseActivation(Release{Version: source.Version, NPM: source.NPM, TreeSHA256: source.Node.SHA256}); err == nil {
		t.Fatal("local legacy receipt became official trust")
	}
}
