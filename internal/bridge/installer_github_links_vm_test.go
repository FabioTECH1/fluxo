package bridge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Uses private SQLite/key/association fixtures only. GitHub verification is
// simulated; real request transport has its separate HTTP tests. Live panel,
// Scheduler, task journals, SSH keys and applications are never written here.
func TestVMInstallerGitHubLinksPrivateRecovery(t *testing.T) {
	host, _ := os.Hostname()
	if host != "fluxo-php-patch-test" || os.Geteuid() != 0 || os.Getenv("FLUXO_PHP_UPDATE_VM_TEST") != "1" {
		t.Skip("explicit root opt-in on the named Ubuntu acceptance VM only")
	}
	root, err := os.MkdirTemp("/root", "fluxo-github-link-fixture-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	t.Setenv("TMPDIR", root)
	f := newLinkRecoveryFixture(t)
	f.account(t, 4, "synthetic-native-fixture-token")
	for _, id := range []int{14, 16, 19, 21, 22, 23} {
		f.site(t, id, 0, "company/project")
	}
	f.site(t, 90, 8, "untouched/project")
	f.deps.backup = func(plans []githubLinkPlan) (string, error) { return preserveInstallerGitHubLinkPlan(root, plans) }
	r, err := restoreGitHubLinks(context.Background(), f.db, f.deps)
	if err != nil || len(r.Restored) != 6 {
		t.Fatal(r, err)
	}
	info, err := protectedInfo(r.Backup)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("backup not private/protected", err)
	}
	folder, err := os.Stat(filepath.Dir(r.Backup))
	if err != nil || folder.Mode().Perm() != 0700 {
		t.Fatal("backup directory not private", err)
	}
	raw, err := os.ReadFile(r.Backup)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Format int
		Links  []githubLinkPlan
	}
	if err = json.Unmarshal(raw, &saved); err != nil || saved.Format != 1 || len(saved.Links) != 6 {
		t.Fatal("missing original association record", err)
	}
	for _, link := range saved.Links {
		if link.PreviousAccountID != 0 || link.AccountID != 4 || link.Repository != "company/project" || link.Branch != "main" {
			t.Fatal("original association projection changed")
		}
	}
	if f.links(t)[90] != 8 {
		t.Fatal("unrelated association changed")
	}
	r, err = restoreGitHubLinks(context.Background(), f.db, f.deps)
	if err != nil || r.Affected != 0 || len(r.Restored) != 0 {
		t.Fatal("completed recovery replayed", r, err)
	}
	t.Log("six private native SQLite associations restored with synced root-private originals; retry is a no-op; live installation untouched; GitHub verification simulated")
}
