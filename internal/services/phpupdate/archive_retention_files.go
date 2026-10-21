package phpupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

type archiveRetentionStore struct {
	root  *os.File
	owner uint32
}
type archiveRetentionFile struct {
	Parent   *os.File
	Name     string
	Identity unix.Stat_t
}
type archiveRetentionDirectory struct {
	File, Parent *os.File
	Name         string
	Identity     unix.Stat_t
}
type archiveRetentionSet struct {
	Files       []archiveRetentionFile
	Directories []archiveRetentionDirectory
	Manifest    archiveRetentionFile
}

func openArchiveRetentionStore(path string, owner uint32) (*archiveRetentionStore, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, abs, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), abs)
	if _, err = retentionFileIdentity(f, owner, true); err != nil {
		f.Close()
		return nil, err
	}
	return &archiveRetentionStore{root: f, owner: owner}, nil
}
func (s *archiveRetentionStore) Close() { s.root.Close() }

func retentionOpen(parent *os.File, name string, directory bool, owner uint32) (*os.File, unix.Stat_t, error) {
	var st unix.Stat_t
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return nil, st, errors.New("invalid archive path component")
	}
	flags := uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK)
	if directory {
		flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Openat2(int(parent.Fd()), name, &unix.OpenHow{Flags: flags, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return nil, st, err
	}
	f := os.NewFile(uintptr(fd), name)
	st, err = retentionFileIdentity(f, owner, directory)
	if err != nil {
		f.Close()
		return nil, st, err
	}
	return f, st, nil
}
func retentionFileIdentity(f *os.File, owner uint32, directory bool) (unix.Stat_t, error) {
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return st, err
	}
	want := uint32(unix.S_IFREG)
	if directory {
		want = unix.S_IFDIR
	}
	if st.Mode&unix.S_IFMT != want || st.Uid != owner || st.Mode&0022 != 0 || (!directory && st.Nlink != 1) {
		return st, fmt.Errorf("unsafe PHP archive ownership, type, mode or links: %s (owner %d, expected %d, mode %#o, links %d)", f.Name(), st.Uid, owner, st.Mode, st.Nlink)
	}
	return st, nil
}
func sameRetentionIdentity(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}
func retentionNames(f *os.File) ([]string, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	names, err := f.Readdirnames(2049)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(names) > 2048 {
		return nil, errors.New("PHP archive directory exceeds cleanup limit")
	}
	sort.Strings(names)
	return names, nil
}

func archiveMetadataForRetention(g recoveryGeneration) (map[string]PackageArchive, error) {
	m := g.Archives
	if !canonicalGenerationID(g.ID) || m.Schema != 1 || m.Version != g.Version || len(m.Packages) == 0 || len(m.Packages) > 256 || len(m.After) != len(m.Packages) || (m.RollbackAvailable && len(m.Before) != len(m.Packages)) {
		return nil, errors.New("incomplete PHP archive retention manifest")
	}
	result := map[string]PackageArchive{}
	var size int64
	for _, a := range append(append([]PackageArchive{}, m.Before...), m.After...) {
		if !validArchiveMetadata(a) || strings.ToLower(a.SHA256) != a.SHA256 || a.Size > 2<<30 {
			return nil, errors.New("invalid retained package identity")
		}
		if old, ok := result[a.SHA256]; ok {
			if !reflect.DeepEqual(old, a) {
				return nil, errors.New("conflicting retained package identity")
			}
			continue
		}
		result[a.SHA256] = a
		size += a.Size
	}
	if size > 2<<30 {
		return nil, errors.New("PHP archive set exceeds cleanup byte limit")
	}
	return result, nil
}

type retentionContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r retentionContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func inspectRetainedPackage(ctx context.Context, parent *os.File, name string, a PackageArchive, owner uint32) (archiveRetentionFile, error) {
	item := archiveRetentionFile{Parent: parent, Name: name}
	f, st, err := retentionOpen(parent, name, false, owner)
	if err != nil {
		return item, err
	}
	defer f.Close()
	if st.Size != a.Size {
		return item, errors.New("retained package size changed")
	}
	h := sha256.New()
	n, err := io.Copy(h, retentionContextReader{ctx: ctx, r: io.LimitReader(f, a.Size+1)})
	if err != nil {
		return item, err
	}
	if n != a.Size || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
		return item, errors.New("retained package checksum changed")
	}
	after, err := retentionFileIdentity(f, owner, false)
	if err != nil {
		return item, err
	}
	if !sameRetentionIdentity(st, after) {
		return item, errors.New("retained package changed during inspection")
	}
	item.Identity = after
	return item, nil
}

func (s *archiveRetentionStore) inspectGeneration(ctx context.Context, g recoveryGeneration, complete bool) (_ *archiveRetentionSet, resultErr error) {
	metadata, err := archiveMetadataForRetention(g)
	if err != nil {
		return nil, err
	}
	set := &archiveRetentionSet{}
	defer func() {
		if resultErr != nil {
			set.Close()
		}
	}()
	parent := s.root
	for _, name := range []string{"php-updates", g.ID, "archives"} {
		f, st, e := retentionOpen(parent, name, true, s.owner)
		if e != nil {
			return nil, e
		}
		set.Directories = append(set.Directories, archiveRetentionDirectory{File: f, Parent: parent, Name: name, Identity: st})
		parent = f
	}
	archives := parent
	f, st, err := retentionOpen(archives, "manifest.json", false, s.owner)
	if err != nil {
		return nil, err
	}
	if st.Size > 4<<20 {
		f.Close()
		return nil, errors.New("oversized PHP archive manifest")
	}
	var manifest archiveManifest
	d := json.NewDecoder(io.LimitReader(f, 4<<20))
	d.DisallowUnknownFields()
	err = d.Decode(&manifest)
	if err == nil {
		var trailing any
		if e := d.Decode(&trailing); !errors.Is(e, io.EOF) {
			err = errors.New("trailing PHP archive manifest data")
		}
	}
	after, e := retentionFileIdentity(f, s.owner, false)
	f.Close()
	if err != nil {
		return nil, err
	}
	if e != nil {
		return nil, e
	}
	if !sameRetentionIdentity(st, after) || !reflect.DeepEqual(manifest, g.Archives) {
		return nil, errors.New("PHP archive manifest differs from durable generation")
	}
	set.Manifest = archiveRetentionFile{Parent: archives, Name: "manifest.json", Identity: after}
	names, err := retentionNames(archives)
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for _, name := range names {
		if name == "manifest.json" {
			continue
		}
		if name == "recovery-before" || name == "recovery-after" {
			if err = set.inspectRecoveryDirectory(ctx, s.owner, archives, name, g.Archives); err != nil {
				return nil, err
			}
			continue
		}
		a, ok := metadata[strings.TrimSuffix(name, ".deb")]
		if !ok || name != a.SHA256+".deb" {
			return nil, fmt.Errorf("unrecorded PHP archive entry: %s", name)
		}
		item, e := inspectRetainedPackage(ctx, archives, name, a, s.owner)
		if e != nil {
			return nil, e
		}
		set.Files = append(set.Files, item)
		present[a.SHA256] = true
	}
	if complete && len(present) != len(metadata) {
		return nil, errors.New("successor PHP recovery files are incomplete; retaining older archives")
	}
	for i := range set.Directories {
		st, e := retentionFileIdentity(set.Directories[i].File, s.owner, true)
		if e != nil {
			return nil, e
		}
		set.Directories[i].Identity = st
	}
	return set, nil
}

func (set *archiveRetentionSet) inspectRecoveryDirectory(ctx context.Context, owner uint32, parent *os.File, name string, m archiveManifest) error {
	f, st, err := retentionOpen(parent, name, true, owner)
	if err != nil {
		return err
	}
	set.Directories = append(set.Directories, archiveRetentionDirectory{File: f, Parent: parent, Name: name, Identity: st})
	wanted := m.After
	if name == "recovery-before" {
		wanted = m.Before
	}
	allowed := map[string]PackageArchive{}
	for _, a := range wanted {
		allowed[a.SHA256+".deb"] = a
	}
	names, err := retentionNames(f)
	if err != nil {
		return err
	}
	for _, child := range names {
		if child == "lists" || child == "cache" {
			// Retain APT metadata/cache trees entirely; never traverse or delete
			// arbitrary descendants. Verify only the immediate boundary.
			d, _, e := retentionOpen(f, child, true, owner)
			if e != nil {
				return e
			}
			d.Close()
			continue
		}
		if child == "Packages" || child == "sources.list" {
			meta, _, e := retentionOpen(f, child, false, owner)
			if e != nil {
				return e
			}
			meta.Close()
			continue
		}
		a, ok := allowed[child]
		if !ok {
			return fmt.Errorf("unrecorded PHP recovery entry: %s", child)
		}
		item, e := inspectRetainedPackage(ctx, f, child, a, owner)
		if e != nil {
			return e
		}
		set.Files = append(set.Files, item)
	}
	return nil
}

func (set *archiveRetentionSet) Close() {
	for i := len(set.Directories) - 1; i >= 0; i-- {
		set.Directories[i].File.Close()
	}
}
func (set *archiveRetentionSet) Unchanged() error {
	for _, d := range set.Directories {
		f, st, err := retentionOpen(d.Parent, d.Name, true, d.Identity.Uid)
		if err != nil {
			return err
		}
		f.Close()
		if !sameRetentionIdentity(st, d.Identity) {
			return errors.New("PHP archive directory changed before cleanup")
		}
	}
	f, st, err := retentionOpen(set.Manifest.Parent, set.Manifest.Name, false, set.Manifest.Identity.Uid)
	if err != nil {
		return err
	}
	f.Close()
	if !sameRetentionIdentity(st, set.Manifest.Identity) {
		return errors.New("PHP archive manifest changed before cleanup")
	}
	for _, item := range set.Files {
		f, st, err := retentionOpen(item.Parent, item.Name, false, item.Identity.Uid)
		if err != nil {
			return err
		}
		f.Close()
		if !sameRetentionIdentity(st, item.Identity) {
			return errors.New("PHP package archive changed before cleanup")
		}
	}
	return nil
}
func (set *archiveRetentionSet) UnlinkBatch(ctx context.Context, limit int) (count int, bytes int64, resultErr error) {
	changed := map[*os.File]bool{}
	defer func() {
		for d := range changed {
			resultErr = errors.Join(resultErr, d.Sync())
		}
	}()
	for _, item := range set.Files {
		if count >= limit {
			break
		}
		if err := ctx.Err(); err != nil {
			return count, bytes, err
		}
		f, st, err := retentionOpen(item.Parent, item.Name, false, item.Identity.Uid)
		if err != nil {
			return count, bytes, err
		}
		if !sameRetentionIdentity(st, item.Identity) {
			f.Close()
			return count, bytes, errors.New("PHP archive changed before unlink")
		}
		err = unix.Unlinkat(int(item.Parent.Fd()), item.Name, 0)
		f.Close()
		if err != nil {
			return count, bytes, err
		}
		changed[item.Parent] = true
		count++
		bytes += st.Size
	}
	return count, bytes, nil
}
