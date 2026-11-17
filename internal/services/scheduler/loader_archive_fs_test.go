package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

// These storage tests use actual files/syscalls with a private user-owned
// fixture policy. They do not assert real root/floor admission or create any
// global receipt. Production policy remains root-owned + real writer guards.
type loaderFixtureFiles struct {
	root           string
	directoryCalls int
	onDirectory    func(int)
	admissions     int
}

func (p *loaderFixtureFiles) ownerUID() uint32                  { return uint32(os.Geteuid()) }
func (p *loaderFixtureFiles) admitWriter(context.Context) error { p.admissions++; return nil }
func (p *loaderFixtureFiles) within(path string) error {
	rel, err := filepath.Rel(p.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("outside private test fixture")
	}
	return nil
}
func (p *loaderFixtureFiles) trustFile(path string) error {
	if err := p.within(path); err != nil {
		return err
	}
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != p.ownerUID() || st.Mode&0022 != 0 {
		return errors.New("unprotected test file")
	}
	return p.trustDirectory(filepath.Dir(path))
}
func (p *loaderFixtureFiles) trustDirectory(path string) error {
	if err := p.within(path); err != nil {
		return err
	}
	p.directoryCalls++
	if p.onDirectory != nil {
		p.onDirectory(p.directoryCalls)
	}
	for {
		var st unix.Stat_t
		if err := unix.Lstat(path, &st); err != nil {
			return err
		}
		if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != p.ownerUID() || st.Mode&0022 != 0 {
			return fmt.Errorf("unprotected test directory %s uid=%d expected=%d mode=%o", path, st.Uid, p.ownerUID(), st.Mode)
		}
		if path == p.root {
			return nil
		}
		path = filepath.Dir(path)
	}
}

func loaderFSFixture(t *testing.T) (*loaderFixtureFiles, loaderEpochStore, []byte, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	oldState, oldPolicy := stateDir, loaderFiles
	stateDir = root
	policy := &loaderFixtureFiles{root: root}
	loaderFiles = policy
	t.Cleanup(func() { stateDir, loaderFiles = oldState, oldPolicy })
	if err := os.Mkdir(loaderArchiveDirectory(), 0700); err != nil {
		t.Fatal(err)
	}
	s, j, e := loaderStoreFixture()
	s = loaderFixturePublished(s)
	oldID := s.Current
	old := s.Epochs[oldID]
	var oldPlan LoaderPublicationPlan
	for _, p := range s.Publications {
		oldPlan = p
	}
	archive, err := json.Marshal(loaderArchivedPair{Format: 1, Epoch: old, Publication: oldPlan})
	if err != nil {
		t.Fatal(err)
	}
	if err = createLoaderArchiveNoReplace(loaderArchivePath(oldID), archive); err != nil {
		t.Fatal(err)
	}
	ref, err := loaderArchiveIdentity(loaderArchivePath(oldID))
	if err != nil {
		t.Fatal(err)
	}
	ref.PublicationID, ref.Predecessor, ref.SHA256 = oldPlan.ID, old.Predecessor, hashBytes(archive)
	before, _ := json.Marshal(j)
	e.LoaderNonce = uuid.NewString()
	j.Entries = cloneLoaderEntries(j.Entries)
	j.Entries[e.ID] = e
	after, _ := json.Marshal(j)
	next := LoaderEpoch{ID: uuid.NewString(), Provider: old.Provider, Predecessor: oldID, Targets: map[int]LoaderTarget{e.ID: {Binding: e.Identity, Nonce: e.LoaderNonce, TriggerHash: hashBytes([]byte(CronContent(e)))}}, Phase: "prepared"}
	plan := LoaderPublicationPlan{ID: uuid.NewString(), EpochID: next.ID, Epoch: next, OriginalCore: before, IntendedCore: after, Triggers: map[int]loaderTriggerPublication{e.ID: {Original: oldPlan.Triggers[e.ID].Intended, Intended: []byte(CronContent(e))}}, Phase: "intent"}
	s.Current = next.ID
	s.Pending = plan.ID
	s.Epochs[next.ID] = next
	s.Publications[plan.ID] = plan
	if err = recordLoaderArchiveReference(&s, oldID, ref); err != nil {
		t.Fatal(err)
	}
	s = loaderFixturePublished(s)
	snapshot, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeLoaderStore(snapshot); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(loaderStorePath(), snapshot, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(loaderAnchorPath(), []byte("1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return policy, s, snapshot, oldID
}

func loaderMustReturnErrorPromptly(t *testing.T, call func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- call() }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("unsafe filesystem input accepted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("filesystem operation blocked on hostile input")
	}
}

func TestLoaderArchiveExportedAuditAndCompatibleRestoreUseRealFiles(t *testing.T) {
	_, s, snapshot, id := loaderFSFixture(t)
	if err := AuditLoaderArchiveHashesLocked(); err != nil {
		t.Fatal(err)
	}
	path := loaderArchivePath(id)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path, path+".original"); err != nil {
		t.Fatal(err)
	}
	if err = createLoaderArchiveNoReplace(path, raw); err != nil {
		t.Fatal(err)
	}
	if err = AuditLoaderArchiveHashesLocked(); err == nil {
		t.Fatal("physical replacement silently accepted as original identity")
	}
	if err = RebindRestoredLoaderArchivesLocked(context.Background(), snapshot); err != nil {
		t.Fatal("exact compatible restore refused", err)
	}
	if err = AuditLoaderArchiveHashesLocked(); err != nil {
		t.Fatal(err)
	}
	actual, err := readLoaderStore()
	if err != nil {
		t.Fatal(err)
	}
	if actual.Archives[id].Inode == s.Archives[id].Inode {
		t.Fatal("restored inode was not rebound")
	}
	if !bytes.Equal(mustLoaderJSON(t, loaderWithoutArchiveIdentities(actual)), mustLoaderJSON(t, loaderWithoutArchiveIdentities(s))) {
		t.Fatal("restore changed logical epoch, receipt or publication history")
	}
	if err = RebindRestoredLoaderArchivesLocked(context.Background(), snapshot); err != nil {
		t.Fatal("restore resume was not idempotent", err)
	}
}
func mustLoaderJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestLoaderArchiveCollisionNeverFollowsOrReplacesLinks(t *testing.T) {
	for _, race := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing", true: "appeared-before-publish"}[race], func(t *testing.T) {
			policy, _, _, _ := loaderFSFixture(t)
			path := filepath.Join(loaderArchiveDirectory(), uuid.NewString()+".json")
			target := filepath.Join(stateDir, "missing-target")
			if race {
				policy.directoryCalls = 0
				policy.onDirectory = func(n int) {
					if n == 2 {
						if err := os.Symlink(target, path); err != nil {
							t.Fatal(err)
						}
					}
				}
			} else if err := os.Symlink(target, path); err != nil {
				t.Fatal(err)
			}
			if err := createLoaderArchiveNoReplace(path, []byte("complete intended archive")); err == nil {
				t.Fatal("dangling archive link accepted")
			}
			info, err := os.Lstat(path)
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatal("archive collision replaced the symlink", err)
			}
			if _, err = os.Lstat(target); !os.IsNotExist(err) {
				t.Fatal("archive collision wrote the link target", err)
			}
		})
	}
}

func TestLoaderArchiveExportedAPIsRejectFIFOAndOversizedFiles(t *testing.T) {
	for _, which := range []string{"archive", "ledger", "anchor"} {
		for _, kind := range []string{"fifo", "oversized"} {
			t.Run(which+"/"+kind, func(t *testing.T) {
				_, _, snapshot, id := loaderFSFixture(t)
				path := loaderArchivePath(id)
				if which == "ledger" {
					path = loaderStorePath()
				} else if which == "anchor" {
					path = loaderAnchorPath()
				}
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if kind == "fifo" {
					if err := unix.Mkfifo(path, 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
					if err != nil {
						t.Fatal(err)
					}
					if err = f.Truncate(loaderFileBudget + 1); err != nil {
						t.Fatal(err)
					}
					f.Close()
				}
				loaderMustReturnErrorPromptly(t, AuditLoaderArchiveHashesLocked)
				loaderMustReturnErrorPromptly(t, func() error { return RebindRestoredLoaderArchivesLocked(context.Background(), snapshot) })
			})
		}
	}
}

func TestLoaderArchiveRestoreRefusesInitializationAndOversizedSnapshot(t *testing.T) {
	for _, kind := range []string{"regular", "dangling-link", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			_, _, snapshot, _ := loaderFSFixture(t)
			switch kind {
			case "regular":
				if err := os.WriteFile(loaderInitialPath(), []byte("pending"), 0600); err != nil {
					t.Fatal(err)
				}
			case "dangling-link":
				if err := os.Symlink(filepath.Join(stateDir, "missing"), loaderInitialPath()); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := unix.Mkfifo(loaderInitialPath(), 0600); err != nil {
					t.Fatal(err)
				}
			}
			loaderMustReturnErrorPromptly(t, func() error { return RebindRestoredLoaderArchivesLocked(context.Background(), snapshot) })
			actual, err := os.ReadFile(loaderStorePath())
			if err != nil || !bytes.Equal(actual, snapshot) {
				t.Fatal("initializing restore mutated live ledger", err)
			}
		})
	}
	policy, _, snapshot, _ := loaderFSFixture(t)
	policy.admissions = 0
	if err := RebindRestoredLoaderArchivesLocked(context.Background(), make([]byte, loaderFileBudget+1)); err == nil || policy.admissions != 0 {
		t.Fatal("oversized snapshot reached admission/decode")
	}
	if actual, _ := os.ReadFile(loaderStorePath()); !bytes.Equal(actual, snapshot) {
		t.Fatal("oversized snapshot changed live ledger")
	}
}

func TestLoaderBoundedReaderRejectsPathAndTypeReplacement(t *testing.T) {
	for _, kind := range []string{"fifo-before-open", "link-before-open", "regular-after-open"} {
		t.Run(kind, func(t *testing.T) {
			policy, _, _, _ := loaderFSFixture(t)
			path := filepath.Join(stateDir, "replace-me")
			if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			policy.directoryCalls = 0
			policy.onDirectory = func(n int) {
				when := 1
				if kind == "regular-after-open" {
					when = 2
				}
				if n != when {
					return
				}
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "fifo-before-open":
					if err := unix.Mkfifo(path, 0600); err != nil {
						t.Fatal(err)
					}
				case "link-before-open":
					if err := os.Symlink(path+".original", path); err != nil {
						t.Fatal(err)
					}
				case "regular-after-open":
					if err := os.WriteFile(path, []byte("replaced"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			loaderMustReturnErrorPromptly(t, func() error { _, _, err := readLoaderProtectedFile(path, 1024); return err })
		})
	}
}

func TestLoaderArchiveAuditAndRestoreVerifyFullPairIdentity(t *testing.T) {
	for _, field := range []string{"epoch", "publication", "predecessor"} {
		t.Run(field, func(t *testing.T) {
			_, s, _, id := loaderFSFixture(t)
			path := loaderArchivePath(id)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var pair loaderArchivedPair
			if err = json.Unmarshal(raw, &pair); err != nil {
				t.Fatal(err)
			}
			switch field {
			case "epoch":
				pair.Epoch.ID = uuid.NewString()
			case "publication":
				pair.Publication.ID = uuid.NewString()
			case "predecessor":
				pair.Epoch.Predecessor = uuid.NewString()
			}
			if err = os.Rename(path, path+".original"); err != nil {
				t.Fatal(err)
			}
			forged := mustLoaderJSON(t, pair)
			if err = createLoaderArchiveNoReplace(path, forged); err != nil {
				t.Fatal(err)
			}
			identity, err := loaderArchiveIdentity(path)
			if err != nil {
				t.Fatal(err)
			}
			old := s.Archives[id]
			identity.PublicationID, identity.Predecessor, identity.SHA256 = old.PublicationID, old.Predecessor, hashBytes(forged)
			s.Archives[id] = identity
			ledger := mustLoaderJSON(t, s)
			if err = os.WriteFile(loaderStorePath(), ledger, 0600); err != nil {
				t.Fatal(err)
			}
			if err = AuditLoaderArchiveHashesLocked(); err == nil {
				t.Fatal("checksum-consistent wrong archive pair accepted")
			}
			if err = RebindRestoredLoaderArchivesLocked(context.Background(), ledger); err == nil {
				t.Fatal("restore accepted checksum-consistent wrong pair")
			}
		})
	}
}

func TestLoaderArchiveMissingAndInterruptedStorageNeverLooksFresh(t *testing.T) {
	for _, missing := range []string{"archive", "ledger", "anchor"} {
		t.Run(missing, func(t *testing.T) {
			_, _, snapshot, id := loaderFSFixture(t)
			path := map[string]string{"archive": loaderArchivePath(id), "ledger": loaderStorePath(), "anchor": loaderAnchorPath()}[missing]
			if err := os.Rename(path, path+".retained"); err != nil {
				t.Fatal(err)
			}
			if err := AuditLoaderArchiveHashesLocked(); err == nil {
				t.Fatal("lost established storage was accepted as fresh")
			}
			if err := RebindRestoredLoaderArchivesLocked(context.Background(), snapshot); err == nil {
				t.Fatal("restore invented missing evidence")
			}
			if err := os.Rename(path+".retained", path); err != nil {
				t.Fatal(err)
			}
			// Renaming retained evidence may change ctime. Only the explicit
			// compatible-restore API may bind that physical change.
			if err := RebindRestoredLoaderArchivesLocked(context.Background(), snapshot); err != nil {
				t.Fatal("complete exact evidence could not resume explicit restore", err)
			}
			// Unpublished temporary bytes left by a crash convey no authority.
			if err := os.WriteFile(filepath.Join(loaderArchiveDirectory(), ".interrupted-temp"), []byte("incomplete"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := AuditLoaderArchiveHashesLocked(); err != nil {
				t.Fatal("unreferenced temporary file displaced committed evidence", err)
			}
		})
	}
}

func TestLoaderArchiveDuplicatePublicationIDsRefusedByExportedAPIs(t *testing.T) {
	_, s, _, id := loaderFSFixture(t)
	ref := s.Archives[id]
	previous := uuid.NewString()
	// Keep a linear, reachable ancestry, but deliberately reuse the immutable
	// publication identity. Decoder must reject before any archive path read.
	s.Archives[previous] = ref
	ref.Predecessor = previous
	s.Archives[id] = ref
	raw := mustLoaderJSON(t, s)
	if err := os.WriteFile(loaderStorePath(), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := AuditLoaderArchiveHashesLocked(); err == nil {
		t.Fatal("duplicate archived publication identity accepted")
	}
	if err := RebindRestoredLoaderArchivesLocked(context.Background(), raw); err == nil {
		t.Fatal("restore accepted duplicate publication identity")
	}
	actual, err := os.ReadFile(loaderStorePath())
	if err != nil || !bytes.Equal(raw, actual) {
		t.Fatal("refusal modified ledger", err)
	}
}

func TestLoaderInitializationRecoveryPreservesConflictingEvidence(t *testing.T) {
	for _, ledgerAbsent := range []bool{false, true} {
		t.Run(fmt.Sprintf("ledger-absent=%v", ledgerAbsent), func(t *testing.T) {
			_, _, snapshot, _ := loaderFSFixture(t)
			if ledgerAbsent {
				if err := os.Remove(loaderStorePath()); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(loaderInitialPath(), snapshot, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(loaderAnchorPath(), []byte("conflict\n"), 0600); err != nil {
				t.Fatal(err)
			}
			type observed struct {
				present bool
				stat    unix.Stat_t
				data    []byte
			}
			observe := func(path string) observed {
				var st unix.Stat_t
				err := unix.Lstat(path, &st)
				if os.IsNotExist(err) {
					return observed{}
				}
				if err != nil {
					t.Fatal(err)
				}
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				return observed{true, st, raw}
			}
			paths := []string{loaderInitialPath(), loaderStorePath(), loaderAnchorPath()}
			before := map[string]observed{}
			for _, p := range paths {
				before[p] = observe(p)
			}
			if err := recoverLoaderInitializationLocked(); err == nil {
				t.Fatal("conflicting anchor was silently repaired")
			}
			for _, p := range paths {
				a, b := before[p], observe(p)
				if a.present != b.present || !bytes.Equal(a.data, b.data) || a.present && !loaderSameFileStat(a.stat, b.stat) {
					t.Fatal("recovery changed retained evidence before refusing", p)
				}
			}
		})
	}
}

func TestLoaderInitializationRecoveryResumesLegitimateStages(t *testing.T) {
	for _, stage := range []string{"intent-only", "ledger-published", "anchor-published"} {
		t.Run(stage, func(t *testing.T) {
			_, _, snapshot, _ := loaderFSFixture(t)
			if err := os.WriteFile(loaderInitialPath(), snapshot, 0600); err != nil {
				t.Fatal(err)
			}
			if stage == "intent-only" {
				if err := os.Remove(loaderStorePath()); err != nil {
					t.Fatal(err)
				}
			}
			if stage != "anchor-published" {
				if err := os.Remove(loaderAnchorPath()); err != nil {
					t.Fatal(err)
				}
			}
			if err := recoverLoaderInitializationLocked(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(loaderInitialPath()); !os.IsNotExist(err) {
				t.Fatal("completed initialization retained an unsealed intent", err)
			}
			actual, err := os.ReadFile(loaderStorePath())
			if err != nil || !bytes.Equal(actual, snapshot) {
				t.Fatal("initialization did not publish the exact ledger", err)
			}
			anchor, err := os.ReadFile(loaderAnchorPath())
			if err != nil || string(anchor) != "1\n" {
				t.Fatal("initialization marker differs", err)
			}
			if _, err = readLoaderStore(); err != nil {
				t.Fatal("recovered store cannot be read", err)
			}
		})
	}
}
