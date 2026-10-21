package phpupdate

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fluxo/internal/syscmd"
)

//go:embed archive_retention_fixture_vm_test.py
var archiveRetentionVMFixture string

// Reuse independently authenticated repository evidence from the preserved
// migration acceptance run. Never invent or rewrite its policy authorization.
func vmRetentionReuseRepositoryEvidence(t *testing.T, m *Manager) {
	t.Helper()
	ob, err := m.observePackages(context.Background(), "", "8.4")
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile("/usr/share/keyrings/fluxo-sury-archive.gpg")
	if err != nil {
		t.Fatal(err)
	}
	keyHash := sha256.Sum256(key)
	paths, err := filepath.Glob("/var/lib/fluxo-php-recovery-acceptance-runs/*/php-repository/authorization.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		raw, e := os.ReadFile(path)
		if e != nil {
			continue
		}
		var auth struct {
			Policy string `json:"policy_hash"`
			Key    string `json:"key_sha256"`
		}
		if json.Unmarshal(raw, &auth) != nil || auth.Policy != ob.PolicyHash || auth.Key != hex.EncodeToString(keyHash[:]) {
			continue
		}
		if e = rootOwned(path); e != nil {
			t.Fatal(e)
		}
		catalog, e := os.ReadFile(filepath.Join(filepath.Dir(path), "retained", "catalog.json"))
		if e != nil {
			t.Fatal(e)
		}
		var packages []PackageArchive
		if e = json.Unmarshal(catalog, &packages); e != nil {
			t.Fatal(e)
		}
		destination := filepath.Join(m.repositoryDirectory(), "retained")
		if e = ensureDurableRecoveryDirectory(destination); e != nil {
			t.Fatal(e)
		}
		for _, a := range packages {
			if !validArchiveMetadata(a) {
				t.Fatal("invalid preserved authenticated fixture metadata")
			}
			source := filepath.Join(filepath.Dir(path), "retained", a.SHA256+".deb")
			if e = rootOwned(source); e != nil {
				t.Fatal(e)
			}
			b, e := os.ReadFile(source)
			if e != nil {
				t.Fatal(e)
			}
			sum := sha256.Sum256(b)
			if int64(len(b)) != a.Size || hex.EncodeToString(sum[:]) != a.SHA256 {
				t.Fatal("preserved authenticated fixture bytes changed")
			}
			if e = os.WriteFile(filepath.Join(destination, a.SHA256+".deb"), b, 0600); e != nil {
				t.Fatal(e)
			}
		}
		if e = os.WriteFile(filepath.Join(destination, "catalog.json"), catalog, 0600); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(m.repositoryDirectory(), "authorization.json"), raw, 0600); e != nil {
			t.Fatal(e)
		}
		t.Logf("Reused unchanged authenticated repository evidence from %s", filepath.Dir(path))
		return
	}
	t.Fatal("Preserved authenticated Sury repository evidence does not match the VM's current policy")
}

func vmRetentionInstallMiddleRevision(t *testing.T, m *Manager) []Package {
	t.Helper()
	inventories, err := bridge(context.Background(), "", map[string]any{"action": "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	var original []Package
	for _, inventory := range inventories {
		if inventory.Version == "8.4" {
			original = inventory.Packages
		}
	}
	if len(original) == 0 {
		t.Fatal("missing installed PHP 8.4 fixture")
	}
	var old, restore []string
	for _, p := range original {
		if p.Source != "php8.4" {
			continue
		}
		if p.Held || !p.Configured {
			t.Fatal("damaged or held fixture package")
		}
		files, e := filepath.Glob(filepath.Join("/var/lib/fluxo-php-recovery-fixture-repo", p.Name+"_*_amd64.deb"))
		if e != nil || len(files) != 1 {
			t.Fatalf("valid vendor fixture for %s: %v %v", p.Name, files, e)
		}
		version := strings.TrimSpace(vmRun(t, "/usr/bin/dpkg-deb", "-f", files[0], "Version"))
		old = append(old, p.Name+"="+version)
		restore = append(restore, p.Name+"="+p.Installed)
	}
	if len(old) < 3 {
		t.Fatal("incomplete middle-revision fixture")
	}
	t.Cleanup(func() { vmApt(t, restore); vmRestoreMarks(t, original) })
	vmApt(t, old)
	vmRestoreMarks(t, original)
	return original
}

func vmRetentionPrepareNextRevision(t *testing.T, m *Manager, generation string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	output, err := syscmd.RunEnv(ctx, 5*time.Minute, []string{"FLUXO_PHP_UPDATE_VM_TEST=1"}, "/usr/bin/python3", "-I", "-c", archiveRetentionVMFixture, "prepare", m.DataDir, generation)
	// Register restoration even if fixture generation stopped partway through.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		out, e := syscmd.RunEnv(ctx, 5*time.Minute, []string{"FLUXO_PHP_UPDATE_VM_TEST=1"}, "/usr/bin/python3", "-I", "-c", archiveRetentionVMFixture, "restore", m.DataDir, generation)
		if e != nil {
			t.Errorf("restore fixture indexes: %v %s", e, out)
		}
	})
	if err != nil {
		t.Fatalf("prepare signed next revision: %v %s", err, output)
	}
	t.Log(strings.TrimSpace(output))
}
