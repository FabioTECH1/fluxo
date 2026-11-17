package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"golang.org/x/sys/unix"
)

// Archived pairs are immutable. The active ledger retains their cryptographic
// digest and protected filesystem identity, rather than rewriting every old
// core snapshot for each current-generation acknowledgement.
type loaderArchiveRef struct {
	PublicationID string `json:"publication_id"`
	Predecessor   string `json:"predecessor"`
	SHA256        string `json:"sha256"`
	Device        uint64 `json:"device"`
	Inode         uint64 `json:"inode"`
	Size          int64  `json:"size"`
	ModifiedNS    int64  `json:"modified_ns"`
	ChangedNS     int64  `json:"changed_ns"`
}
type loaderArchivedPair struct {
	Format      int                   `json:"format"`
	Epoch       LoaderEpoch           `json:"epoch"`
	Publication LoaderPublicationPlan `json:"publication"`
}

func decodeLoaderArchivePair(raw []byte, id string, ref loaderArchiveRef, all map[string]loaderArchiveRef) (loaderArchivedPair, error) {
	var pair loaderArchivedPair
	if len(raw) == 0 || len(raw) > loaderFileBudget || int64(len(raw)) != ref.Size || hashBytes(raw) != ref.SHA256 {
		return pair, errors.New("loader archive content differs from its bounded checksum reference")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&pair); err != nil {
		return pair, err
	}
	if d.Decode(new(any)) != io.EOF {
		return pair, errors.New("trailing loader archive data")
	}
	if pair.Format != 1 || pair.Epoch.ID != id || pair.Publication.ID != ref.PublicationID || pair.Publication.EpochID != id || pair.Epoch.Predecessor != ref.Predecessor || pair.Publication.Epoch.Predecessor != ref.Predecessor || pair.Epoch.Phase != "published" || pair.Publication.Phase != "committed" {
		return pair, errors.New("loader archived pair does not match its exact epoch/publication/lineage reference")
	}
	ancestors := map[string]loaderArchiveRef{}
	seen := map[string]bool{id: true}
	previous := ref.Predecessor
	for previous != "" {
		r, ok := all[previous]
		if !ok || seen[previous] {
			return pair, errors.New("loader archive predecessor is missing or cyclic")
		}
		seen[previous] = true
		ancestors[previous] = r
		previous = r.Predecessor
	}
	s := emptyLoaderStore()
	s.Current = id
	s.Epochs[id] = pair.Epoch
	s.Publications[pair.Publication.ID] = pair.Publication
	s.Archives = ancestors
	encoded, err := json.Marshal(s)
	if err != nil {
		return pair, err
	}
	if _, err = decodeLoaderStore(encoded); err != nil {
		return pair, err
	}
	return pair, nil
}

func loaderArchiveDirectory() string     { return filepath.Join(stateDir, "loader-history") }
func loaderArchivePath(id string) string { return filepath.Join(loaderArchiveDirectory(), id+".json") }
func loaderArchiveRefValid(r loaderArchiveRef) bool {
	return loaderUUID(r.PublicationID) && loaderDigestValid(r.SHA256) && r.Inode != 0 && r.Size > 0 && r.Size <= 64<<20 && r.ModifiedNS > 0 && r.ChangedNS > 0 && (r.Predecessor == "" || loaderUUID(r.Predecessor))
}

func loaderArchiveIdentity(path string) (loaderArchiveRef, error) {
	if err := loaderFiles.trustFile(path); err != nil {
		return loaderArchiveRef{}, err
	}
	return loaderArchiveIdentityInProtectedDirectory(path)
}

func loaderArchiveIdentityInProtectedDirectory(path string) (loaderArchiveRef, error) {
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return loaderArchiveRef{}, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0777 != 0600 || st.Nlink != 1 || st.Uid != loaderFiles.ownerUID() || st.Size <= 0 || st.Size > loaderFileBudget {
		return loaderArchiveRef{}, errors.New("unprotected loader archive")
	}
	return loaderArchiveRef{Device: uint64(st.Dev), Inode: st.Ino, Size: st.Size, ModifiedNS: st.Mtim.Nano(), ChangedNS: st.Ctim.Nano()}, nil
}
func loaderArchiveIdentityEqual(a, b loaderArchiveRef) bool {
	return a.Device == b.Device && a.Inode == b.Inode && a.Size == b.Size && a.ModifiedNS == b.ModifiedNS && a.ChangedNS == b.ChangedNS
}

func verifyLoaderArchiveReferences(s loaderEpochStore) error {
	if len(s.Archives) == 0 {
		return nil
	}
	// Validate the shared protected traversal once, then each exact direct
	// child inode. Rewalking every common ancestor for every reference would
	// make the per-invocation history check unnecessarily expensive.
	if err := loaderFiles.trustDirectory(loaderArchiveDirectory()); err != nil {
		return err
	}
	for id, ref := range s.Archives {
		actual, err := loaderArchiveIdentityInProtectedDirectory(loaderArchivePath(id))
		if err != nil {
			return err
		}
		if !loaderArchiveIdentityEqual(actual, ref) {
			return errors.New("loader archive changed or was replaced; retain and reconcile history")
		}
	}
	return nil
}

// AuditLoaderArchiveHashesLocked is the full-content bridge/backup audit. Normal
// admission checks the exact protected inode/size/mtime/ctime identities; it
// never rereads all historical core bytes for each scheduled invocation.
func AuditLoaderArchiveHashesLocked() error {
	s, err := readLoaderStore()
	if err != nil {
		return err
	}
	for id, ref := range s.Archives {
		raw, _, e := readLoaderProtectedFile(loaderArchivePath(id), loaderFileBudget)
		if e != nil {
			return e
		}
		if _, e = decodeLoaderArchivePair(raw, id, ref, s.Archives); e != nil {
			return e
		}
	}
	return verifyLoaderArchiveReferences(s)
}

// RebindRestoredLoaderArchivesLocked is an explicit compatible-restore step,
// never an ordinary admission fallback. The installer owns exclusive lifecycle
// admission and the Scheduler gate and supplies its authenticated snapshot
// ledger bytes. Physical copy/restore changes inode/ctime; only exact logical
// snapshot equality and every retained content checksum authorize rebinding.
func RebindRestoredLoaderArchivesLocked(ctx context.Context, snapshot []byte) error {
	if len(snapshot) == 0 || len(snapshot) > loaderFileBudget {
		return errors.New("restored loader snapshot exceeds decode budget")
	}
	if err := loaderFiles.admitWriter(ctx); err != nil {
		return err
	}
	if absent, err := loaderPathAbsent(loaderInitialPath()); err != nil {
		return err
	} else if !absent {
		return errors.New("loader initialization must finish before compatible restore revalidation")
	}
	original, err := decodeLoaderStore(snapshot)
	if err != nil {
		return err
	}
	anchor, _, err := readLoaderProtectedFile(loaderAnchorPath(), 16)
	if err != nil || string(anchor) != "1\n" {
		return errors.New("restored loader initialization anchor is invalid")
	}
	raw, _, err := readLoaderProtectedFile(loaderStorePath(), loaderFileBudget)
	if err != nil {
		return err
	}
	current, err := decodeLoaderStore(raw)
	if err != nil {
		return err
	}
	if current.Pending != "" || original.Pending != "" {
		return errors.New("finish exact loader publication recovery before compatible restore revalidation")
	}
	if !reflect.DeepEqual(loaderWithoutArchiveIdentities(current), loaderWithoutArchiveIdentities(original)) {
		return errors.New("restored loader ledger differs from the authenticated logical snapshot")
	}
	changed := false
	for id, ref := range current.Archives {
		path := loaderArchivePath(id)
		identity, err := loaderArchiveIdentity(path)
		if err != nil {
			return err
		}
		content, _, err := readLoaderProtectedFile(path, loaderFileBudget)
		if err != nil {
			return err
		}
		if _, err = decodeLoaderArchivePair(content, id, ref, current.Archives); err != nil {
			return err
		}
		if !loaderArchiveIdentityEqual(identity, ref) {
			identity.PublicationID, identity.Predecessor, identity.SHA256 = ref.PublicationID, ref.Predecessor, ref.SHA256
			current.Archives[id] = identity
			changed = true
		}
	}
	if !changed {
		return verifyLoaderArchiveReferences(current)
	}
	updated, err := json.Marshal(current)
	if err != nil {
		return err
	}
	if _, err = decodeLoaderStore(updated); err != nil {
		return err
	}
	if actual, _, err := readLoaderProtectedFile(loaderStorePath(), loaderFileBudget); err != nil || !bytes.Equal(actual, raw) {
		return errors.New("restored loader ledger changed during revalidation")
	}
	if err = atomicFile(loaderStorePath(), updated, 0600); err != nil {
		return err
	}
	return verifyLoaderArchiveReferences(current)
}

func loaderWithoutArchiveIdentities(s loaderEpochStore) loaderEpochStore {
	refs := make(map[string]loaderArchiveRef, len(s.Archives))
	for id, r := range s.Archives {
		r.Device, r.Inode, r.ModifiedNS, r.ChangedNS = 0, 0, 0, 0
		refs[id] = r
	}
	s.Archives = refs
	return s
}

// The new publication intent already contains the complete predecessor pair.
// Ordinary receipts are fenced while Pending is set. A crash therefore resumes
// the same immutable snapshot, including final predecessor received bits.
func archiveSupersededLoaderPairs(s *loaderEpochStore) error {
	changed := false
	for id, epoch := range s.Epochs {
		if id == s.Current {
			continue
		}
		if s.Pending == "" || s.Epochs[s.Current].Predecessor != id || epoch.Phase != "published" {
			return errors.New("unrecorded superseded loader epoch")
		}
		var plan LoaderPublicationPlan
		for _, p := range s.Publications {
			if p.EpochID == id {
				plan = p
			}
		}
		if plan.ID == "" || plan.Phase != "committed" {
			return errors.New("superseded loader publication is incomplete")
		}
		raw, err := json.Marshal(loaderArchivedPair{Format: 1, Epoch: epoch, Publication: plan})
		if err != nil {
			return err
		}
		if len(raw) > 64<<20 {
			return errors.New("loader archive exceeds preservation budget")
		}
		if err = os.Mkdir(loaderArchiveDirectory(), 0700); err != nil && !os.IsExist(err) {
			return err
		}
		if err = loaderFiles.trustDirectory(loaderArchiveDirectory()); err != nil {
			return err
		}
		path := loaderArchivePath(id)
		if err = createLoaderArchiveNoReplace(path, raw); err != nil {
			return err
		}
		ref, err := loaderArchiveIdentity(path)
		if err != nil {
			return err
		}
		actual, _, err := readLoaderProtectedFile(path, loaderFileBudget)
		if err != nil || !bytes.Equal(actual, raw) {
			return errors.New("loader archive readback differs")
		}
		ref.PublicationID, ref.Predecessor, ref.SHA256 = plan.ID, epoch.Predecessor, hashBytes(raw)
		if old, ok := s.Archives[id]; ok && old != ref {
			return errors.New("loader archive reference was already bound differently")
		}
		if err = recordLoaderArchiveReference(s, id, ref); err != nil {
			return err
		}
		changed = true
	}
	if changed {
		return saveLoaderStore(*s)
	}
	return nil
}

func recordLoaderArchiveReference(s *loaderEpochStore, id string, ref loaderArchiveRef) error {
	e, ok := s.Epochs[id]
	p, found := s.Publications[ref.PublicationID]
	if id == s.Current || !ok || !found || s.Pending == "" || s.Epochs[s.Current].Predecessor != id || e.Phase != "published" || p.Phase != "committed" || p.EpochID != id || ref.Predecessor != e.Predecessor || !loaderArchiveRefValid(ref) {
		return errors.New("loader archive reference does not match exact pending predecessor")
	}
	s.Archives[id] = ref
	delete(s.Epochs, id)
	delete(s.Publications, p.ID)
	return nil
}
