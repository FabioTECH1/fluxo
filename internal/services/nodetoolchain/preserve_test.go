package nodetoolchain

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreservationAuthenticationAndReuse(t *testing.T) {
	state := managedState{OfficialNode: true, Corepack: true, Bun: true}
	trees := map[string]string{"node": "authenticated-node", "managers": "authenticated-managers"}
	auth, executions, writes := 0, 0, 0
	ops := preservationOps{
		trees:        func() (map[string]string, error) { return trees, nil },
		authenticate: func() error { auth++; return nil },
		executables:  func() (Status, error) { executions++; return Status{ToolchainReady: true}, nil },
		save:         func(s managedState) error { writes++; state = s; return nil },
	}
	if _, err := verifyOrEnroll(state, "pins", ops); err != nil {
		t.Fatal(err)
	}
	if auth != 1 || executions != 1 || writes != 1 {
		t.Fatalf("first assessment: %d/%d/%d", auth, executions, writes)
	}
	// No registry/package preparation/authentication operation is reachable on reuse.
	ops.authenticate = func() error { t.Fatal("reuse performed network/package authentication"); return nil }
	if _, err := verifyOrEnroll(state, "pins", ops); err != nil {
		t.Fatal(err)
	}
	if executions != 2 || writes != 1 {
		t.Fatalf("reuse: executions=%d writes=%d", executions, writes)
	}
}

func TestPreservationRefusesBeforeExecutableOrReceiptMutation(t *testing.T) {
	for _, test := range []struct {
		name             string
		proof            *toolchainIntegrity
		treeErr, authErr error
		trees            map[string]string
	}{
		{name: "changed payload", proof: &toolchainIntegrity{Schema: 1, Profile: "pins", Trees: map[string]string{"node": "original"}}, trees: map[string]string{"node": "tampered"}},
		{name: "missing payload", treeErr: os.ErrNotExist},
		{name: "unsafe ownership", treeErr: errors.New("unexpected owner")},
		{name: "wrong links", treeErr: errors.New("shadowed command")},
		{name: "unknown proof", proof: &toolchainIntegrity{Schema: 99}},
		{name: "absent proof untrusted payload", authErr: errors.New("checksum mismatch")},
		{name: "changed pins untrusted payload", proof: &toolchainIntegrity{Schema: 1, Profile: "old"}, authErr: errors.New("unsupported manager selection")},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := managedState{OfficialNode: true, Corepack: true, Bun: true, Integrity: test.proof}
			if state.Integrity != nil {
				state.Integrity.Selection = integritySelection(state)
			}
			_, err := verifyOrEnroll(state, "pins", preservationOps{
				trees:        func() (map[string]string, error) { return test.trees, test.treeErr },
				authenticate: func() error { return test.authErr },
				executables:  func() (Status, error) { t.Fatal("executed unverified payload"); return Status{}, nil },
				save:         func(managedState) error { t.Fatal("saved unverified proof"); return nil },
			})
			if err == nil {
				t.Fatal("unsafe state accepted")
			}
		})
	}
}

func TestPreservationReauthenticatesChangedSelectionAndPins(t *testing.T) {
	for _, change := range []string{"pins", "selection"} {
		t.Run(change, func(t *testing.T) {
			state := managedState{OfficialNode: true, Corepack: true, Bun: true}
			state.Integrity = &toolchainIntegrity{Schema: 1, Profile: "pins", Selection: integritySelection(state)}
			profile := "pins"
			if change == "pins" {
				profile = "new-pins"
			} else {
				state.ApprovedNode = &Release{Version: "24.20.0"}
			}
			auth := false
			_, err := verifyOrEnroll(state, profile, preservationOps{
				trees:        func() (map[string]string, error) { return map[string]string{"node": "new-authenticated"}, nil },
				authenticate: func() error { auth = true; return nil },
				executables: func() (Status, error) {
					if !auth {
						t.Fatal("skipped authentication")
					}
					return Status{}, nil
				},
				save: func(s managedState) error {
					if !auth || s.Integrity.Profile != profile || s.Integrity.Selection != integritySelection(state) {
						t.Fatal("wrong enrollment")
					}
					return nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPreservationDetectsConcurrentPayloadMutation(t *testing.T) {
	state := managedState{OfficialNode: true, Corepack: true, Bun: true}
	reads := 0
	_, err := verifyOrEnroll(state, "pins", preservationOps{
		trees: func() (map[string]string, error) {
			reads++
			if reads == 1 {
				return map[string]string{"node": "before"}, nil
			}
			return map[string]string{"node": "after"}, nil
		},
		authenticate: func() error { return nil },
		executables:  func() (Status, error) { return Status{}, nil },
		save:         func(managedState) error { t.Fatal("enrolled unstable payload"); return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "changed during") {
		t.Fatalf("got %v", err)
	}
}

func TestPendingNodeInstallIsReadOnlyRefusal(t *testing.T) {
	dir := t.TempDir()
	if err := pendingNodeInstall(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "install-interrupted")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := pendingNodeInstall(dir); err == nil {
		t.Fatal("accepted interrupted installation")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("inspection removed recovery evidence")
	}
}

func TestIntegritySelectionDoesNotGrantAuthority(t *testing.T) {
	state := managedState{OfficialNode: true, Corepack: true, Bun: true}
	before := integritySelection(state)
	state.Integrity = &toolchainIntegrity{Schema: 1, Profile: "anything", Trees: map[string]string{"node": "anything"}}
	if integritySelection(state) != before {
		t.Fatal("cache changed authority identity")
	}
	state.ApprovedNode = &Release{Version: "24.20.0", TreeSHA256: "approved"}
	if integritySelection(state) == before {
		t.Fatal("approval selection is not bound")
	}
}

func TestManagerLinksMustSelectAuthenticatedEntrypoints(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0755); err != nil {
		t.Fatal(err)
	}
	dist := filepath.Join(root, "lib/node_modules/corepack/dist")
	if err := os.MkdirAll(dist, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"corepack", "pnpm", "pnpx", "yarn", "yarnpkg"} {
		if err := os.WriteFile(filepath.Join(dist, name+".js"), []byte("authenticated"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../lib/node_modules/corepack/dist/"+name+".js", filepath.Join(root, "bin", name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateManagerCommandLinks(root); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "bin/pnpm")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin/unverified"), []byte("matching version"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("unverified", link); err != nil {
		t.Fatal(err)
	}
	if err := validateManagerCommandLinks(root); err == nil {
		t.Fatal("accepted unrelated matching-version entrypoint")
	}
}
