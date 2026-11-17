package nginx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func crashHTTPDefaults(t *testing.T, run func()) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != "caller disappeared" {
			t.Fatalf("crash point not reached: %v", recovered)
		}
	}()
	run()
}

func duplicateHTTPFixture(t *testing.T, env *httpDefaultsEnvironment) string {
	t.Helper()
	operator := filepath.Join(filepath.Dir(env.path), "nginx.conf")
	contents := "http {\n server_names_hash_bucket_size 64;\n}\n"
	if err := os.WriteFile(operator, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	env.validate = func(context.Context) error {
		if _, err := os.Lstat(env.path); err == nil {
			return fmt.Errorf("nginx: [emerg] \"server_names_hash_bucket_size\" directive is duplicate in %s:2", env.path)
		}
		return nil
	}
	return operator
}

func TestHTTPDefaultsRecoversCrashAfterPublicationBeforeValidation(t *testing.T) {
	env := httpDefaultsFixture(t)
	operator := duplicateHTTPFixture(t, &env)
	original, _ := os.ReadFile(operator)
	env.checkpoint = func(phase string) {
		if phase == "after-publish" {
			panic("caller disappeared")
		}
	}
	crashHTTPDefaults(t, func() { _ = ensureHTTPDefaults(context.Background(), env) })
	if raw, _ := os.ReadFile(env.path); string(raw) != httpDefaultsConfig {
		t.Fatal("publication crash was not exercised")
	}
	env.checkpoint = nil
	if err := ensureHTTPDefaults(context.Background(), env); err != nil {
		t.Fatal("owned duplicate did not recover", err)
	}
	if _, err := os.Lstat(env.path); !os.IsNotExist(err) {
		t.Fatal("recovered duplicate remains", err)
	}
	if raw, _ := os.ReadFile(operator); string(raw) != string(original) {
		t.Fatal("operator file changed")
	}
}

func TestHTTPDefaultsPostRenameDirectorySyncFailureRollsBack(t *testing.T) {
	env := httpDefaultsFixture(t)
	renamed, failed := false, false
	env.checkpoint = func(phase string) {
		if phase == "after-publish-rename" {
			renamed = true
		}
	}
	env.syncDirectory = func(path string) error {
		if renamed && !failed && path == filepath.Dir(env.path) {
			failed = true
			return syscall.EIO
		}
		return syncHTTPDefaultsDirectory(path)
	}
	if err := ensureHTTPDefaults(context.Background(), env); !errors.Is(err, syscall.EIO) {
		t.Fatal("directory durability failure disappeared", err)
	}
	if !renamed || !failed {
		t.Fatal("post-rename failure was not exercised")
	}
	if _, err := os.Lstat(env.path); !os.IsNotExist(err) {
		t.Fatal("unvalidated published file remains", err)
	}
	for _, name := range []string{"held", "intent.json"} {
		if _, err := os.Lstat(filepath.Join(filepath.Dir(env.path), httpDefaultsTransactionName, name)); !os.IsNotExist(err) {
			t.Fatal("successful rollback left active recovery state", name, err)
		}
	}
}

func replaceHTTPPublic(t *testing.T, path, contents string) httpDefaultsIdentity {
	t.Helper()
	temporary := path + ".operator"
	if err := os.WriteFile(temporary, []byte(contents), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	id, err := httpIdentity(info)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestHTTPDefaultsQuarantinePreservesOperatorReplacementRace(t *testing.T) {
	for _, crash := range []bool{false, true} {
		t.Run(fmt.Sprint(crash), func(t *testing.T) {
			env := httpDefaultsFixture(t)
			calls := 0
			env.validate = func(context.Context) error {
				calls++
				if calls == 2 {
					return errors.New("candidate validation failure")
				}
				return nil
			}
			custom := "# operator replacement\nserver_names_hash_bucket_size 128;\n"
			var identity httpDefaultsIdentity
			env.checkpoint = func(phase string) {
				if phase == "before-quarantine" {
					identity = replaceHTTPPublic(t, env.path, custom)
				}
				if crash && phase == "after-quarantine" {
					panic("caller disappeared")
				}
			}
			if crash {
				crashHTTPDefaults(t, func() { _ = ensureHTTPDefaults(context.Background(), env) })
				if _, err := os.Lstat(env.path); !os.IsNotExist(err) {
					t.Fatal("quarantine crash not exercised", err)
				}
				env.checkpoint = nil
			}
			err := ensureHTTPDefaults(context.Background(), env)
			if err == nil || !strings.Contains(err.Error(), "operator replacement") {
				t.Fatal("replacement not reported", err)
			}
			if raw, _ := os.ReadFile(env.path); string(raw) != custom {
				t.Fatal("operator replacement was deleted or changed")
			}
			info, _ := os.Lstat(env.path)
			actual, _ := httpIdentity(info)
			if actual != identity {
				t.Fatal("replacement metadata/inode was not preserved")
			}
		})
	}
}

func TestHTTPDefaultsUnknownSymlinkAndHardlinkAreRestoredWithoutFollowing(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			env := httpDefaultsFixture(t)
			calls := 0
			env.validate = func(context.Context) error {
				calls++
				if calls == 2 {
					return errors.New("candidate failed")
				}
				return nil
			}
			target := filepath.Join(filepath.Dir(env.path), "operator-data")
			if err := os.WriteFile(target, []byte("unchanged operator data"), 0640); err != nil {
				t.Fatal(err)
			}
			env.checkpoint = func(phase string) {
				if phase == "before-quarantine" {
					if err := os.Remove(env.path); err != nil {
						t.Fatal(err)
					}
					var err error
					if kind == "symlink" {
						err = os.Symlink(target, env.path)
					} else {
						err = os.Link(target, env.path)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := ensureHTTPDefaults(context.Background(), env); err == nil {
				t.Fatal("unknown linked replacement was adopted")
			}
			if raw, _ := os.ReadFile(target); string(raw) != "unchanged operator data" {
				t.Fatal("operator link target was changed")
			}
			if kind == "symlink" {
				if got, err := os.Readlink(env.path); err != nil || got != target {
					t.Fatal("symlink was not restored", err)
				}
			} else {
				a, _ := os.Stat(target)
				b, err := os.Stat(env.path)
				if err != nil || !os.SameFile(a, b) {
					t.Fatal("hardlink identity lost", err)
				}
			}
		})
	}
}

func TestHTTPDefaultsCrashAfterRestoringUnknownFileRetainsItOnRetry(t *testing.T) {
	env := httpDefaultsFixture(t)
	calls := 0
	env.validate = func(context.Context) error {
		calls++
		if calls == 2 {
			return errors.New("candidate failed")
		}
		return nil
	}
	custom := "# custom replacement survives restoration crash\n"
	env.checkpoint = func(phase string) {
		if phase == "before-quarantine" {
			replaceHTTPPublic(t, env.path, custom)
		}
		if phase == "after-restore" {
			panic("caller disappeared")
		}
	}
	crashHTTPDefaults(t, func() { _ = ensureHTTPDefaults(context.Background(), env) })
	env.checkpoint = nil
	if err := ensureHTTPDefaults(context.Background(), env); err == nil {
		t.Fatal("restored operator file was silently adopted")
	}
	if raw, _ := os.ReadFile(env.path); string(raw) != custom {
		t.Fatal("restored operator file lost on retry")
	}
}

func TestHTTPDefaultsCrashAfterDiscardRecoversBeforePublication(t *testing.T) {
	env := httpDefaultsFixture(t)
	operator := duplicateHTTPFixture(t, &env)
	original, _ := os.ReadFile(operator)
	env.checkpoint = func(phase string) {
		if phase == "after-discard" {
			panic("caller disappeared")
		}
	}
	crashHTTPDefaults(t, func() { _ = ensureHTTPDefaults(context.Background(), env) })
	env.checkpoint = nil
	if err := ensureHTTPDefaults(context.Background(), env); err != nil {
		t.Fatal("discard completion did not recover", err)
	}
	if raw, _ := os.ReadFile(operator); string(raw) != string(original) {
		t.Fatal("operator setting changed")
	}
	if _, err := os.Lstat(env.path); !os.IsNotExist(err) {
		t.Fatal("duplicate remains", err)
	}
}

func TestHTTPDefaultsIncompletePreMutationFilesArePreservedAsEvidence(t *testing.T) {
	env := httpDefaultsFixture(t)
	tx, err := openHTTPDefaultsTransaction(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(tx.path("intent.json"), []byte("{incomplete"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(tx.path("staged"), []byte("# partial staged write"), 0600); err != nil {
		t.Fatal(err)
	}
	tx.close()
	if err = ensureHTTPDefaults(context.Background(), env); err != nil {
		t.Fatal("pre-mutation interruption did not recover", err)
	}
	evidence := filepath.Join(filepath.Dir(env.path), httpDefaultsTransactionName, "evidence")
	entries, err := os.ReadDir(evidence)
	if err != nil || len(entries) != 2 {
		t.Fatal("incomplete bytes not retained", err, len(entries))
	}
	contents := map[string]bool{}
	for _, entry := range entries {
		raw, err := os.ReadFile(filepath.Join(evidence, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		contents[string(raw)] = true
	}
	if !contents["{incomplete"] || !contents["# partial staged write"] {
		t.Fatal("pre-mutation evidence was discarded")
	}
}

func TestHTTPDefaultsReadOnlyAdmissionRejectsPendingQuarantine(t *testing.T) {
	env := httpDefaultsFixture(t)
	directory := filepath.Join(filepath.Dir(env.path), httpDefaultsTransactionName)
	if err := inspectHTTPDefaultsRecovery(directory, uint32(os.Getuid())); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(directory); !os.IsNotExist(err) {
		t.Fatal("read-only admission initialized recovery state")
	}
	tx, err := openHTTPDefaultsTransaction(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	tx.close()
	if err = inspectHTTPDefaultsRecovery(directory, uint32(os.Getuid())); err != nil {
		t.Fatal("idle lock was not ready", err)
	}
	for _, name := range []string{"held", "staged", "intent.json", "unknown"} {
		path := filepath.Join(directory, name)
		if err = os.WriteFile(path, []byte("preserved"), 0600); err != nil {
			t.Fatal(err)
		}
		if err = inspectHTTPDefaultsRecovery(directory, uint32(os.Getuid())); err == nil {
			t.Fatal("pending recovery admitted", name)
		}
		if raw, _ := os.ReadFile(path); string(raw) != "preserved" {
			t.Fatal("inspection modified recovery evidence")
		}
		if err = os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHTTPDefaultsRollbackDestinationAppearancePreservesBothFiles(t *testing.T) {
	env := httpDefaultsFixture(t)
	calls := 0
	env.validate = func(context.Context) error {
		calls++
		if calls == 2 {
			return errors.New("candidate validation failure")
		}
		return nil
	}
	first, second := "# first operator replacement\n", "# second public replacement\n"
	env.checkpoint = func(phase string) {
		if phase == "before-quarantine" {
			replaceHTTPPublic(t, env.path, first)
		}
		if phase == "before-restore" {
			replaceHTTPPublic(t, env.path, second)
		}
	}
	if err := ensureHTTPDefaults(context.Background(), env); err == nil || !strings.Contains(err.Error(), "retained") {
		t.Fatal("restore conflict was masked", err)
	}
	held := filepath.Join(filepath.Dir(env.path), httpDefaultsTransactionName, "held")
	if raw, _ := os.ReadFile(held); string(raw) != first {
		t.Fatal("quarantined replacement lost")
	}
	if raw, _ := os.ReadFile(env.path); string(raw) != second {
		t.Fatal("new public replacement overwritten")
	}
	env.checkpoint = nil
	if err := ensureHTTPDefaults(context.Background(), env); err == nil {
		t.Fatal("conflicting recovery was ignored")
	}
	if raw, _ := os.ReadFile(held); string(raw) != first {
		t.Fatal("retry lost quarantined replacement")
	}
	if raw, _ := os.ReadFile(env.path); string(raw) != second {
		t.Fatal("retry overwrote public replacement")
	}
}

func TestHTTPDefaultsPublicationNeverReplacesAppearingPublicFile(t *testing.T) {
	env := httpDefaultsFixture(t)
	custom := "# operator won publication race\n"
	env.checkpoint = func(phase string) {
		if phase == "before-publish" {
			replaceHTTPPublic(t, env.path, custom)
		}
	}
	if err := ensureHTTPDefaults(context.Background(), env); err == nil {
		t.Fatal("publication replaced operator file")
	}
	env.checkpoint = nil
	if err := ensureHTTPDefaults(context.Background(), env); err == nil {
		t.Fatal("custom file silently adopted on retry")
	}
	if raw, _ := os.ReadFile(env.path); string(raw) != custom {
		t.Fatal("operator destination changed")
	}
}

func TestHTTPDefaultsRestoresCandidateIfBaselineAlsoFails(t *testing.T) {
	env := httpDefaultsFixture(t)
	calls := 0
	env.validate = func(context.Context) error {
		calls++
		switch calls {
		case 2:
			return errors.New("candidate failed")
		case 3:
			return errors.New("baseline also failed")
		}
		return nil
	}
	err := ensureHTTPDefaults(context.Background(), env)
	if err == nil || !strings.Contains(err.Error(), "candidate failed") || !strings.Contains(err.Error(), "baseline also failed") {
		t.Fatal("double error lost", err)
	}
	if raw, _ := os.ReadFile(env.path); string(raw) != httpDefaultsConfig {
		t.Fatal("original candidate discarded despite invalid baseline")
	}
}

func TestHTTPDefaultsRollbackSyncFailureRetainsRecoverableEvidence(t *testing.T) {
	env := httpDefaultsFixture(t)
	calls, quarantined := 0, false
	env.validate = func(context.Context) error {
		calls++
		if calls == 2 {
			return errors.New("candidate failed")
		}
		return nil
	}
	env.checkpoint = func(phase string) {
		if phase == "after-quarantine-rename" {
			quarantined = true
		}
	}
	env.syncDirectory = func(path string) error {
		if quarantined && path == filepath.Dir(env.path) {
			return syscall.EIO
		}
		return syncHTTPDefaultsDirectory(path)
	}
	err := ensureHTTPDefaults(context.Background(), env)
	if err == nil || !errors.Is(err, syscall.EIO) || !strings.Contains(err.Error(), "candidate failed") {
		t.Fatal("rollback durability error masked", err)
	}
	held := filepath.Join(filepath.Dir(env.path), httpDefaultsTransactionName, "held")
	if raw, _ := os.ReadFile(held); string(raw) != httpDefaultsConfig {
		t.Fatal("failed rollback lost original evidence")
	}
	env.syncDirectory = nil
	env.checkpoint = nil
	env.validate = func(context.Context) error { return nil }
	if err = ensureHTTPDefaults(context.Background(), env); err != nil {
		t.Fatal("durability recovery failed", err)
	}
	if raw, _ := os.ReadFile(env.path); string(raw) != httpDefaultsConfig {
		t.Fatal("fresh validated default missing after recovery")
	}
}
