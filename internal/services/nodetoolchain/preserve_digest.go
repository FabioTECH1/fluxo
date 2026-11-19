package nodetoolchain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"golang.org/x/sys/unix"
)

// preservationDigest is local change-detection evidence, not runtime authority
// or a release digest. The caller must protect the root and its parent paths;
// stability checks do not make a concurrently writable tree a snapshot.
func preservationDigest(root string) (string, error) {
	h := sha256.New()
	enc := json.NewEncoder(h)
	if err := enc.Encode("fluxo-preservation-digest-v1"); err != nil {
		return "", err
	}
	if err := preservationDigestEntry(enc, unix.AT_FDCWD, filepath.Clean(root), "."); err != nil {
		return "", fmt.Errorf("preservation digest %s: %w", root, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// JSON frames each record, including its newline. Byte slices preserve even
// non-UTF-8 path/target bytes, which JSON strings would otherwise replace.
type preservationDigestRecord struct {
	Path   []byte `json:"path"`
	Type   string `json:"type"`
	Mode   uint32 `json:"mode"`
	Target []byte `json:"target"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func preservationDigestEntry(enc *json.Encoder, parent int, name, rel string) error {
	var before unix.Stat_t
	if err := unix.Fstatat(parent, name, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	record := preservationDigestRecord{Path: []byte(rel), Mode: before.Mode}
	kind := before.Mode & unix.S_IFMT
	var f *os.File
	if kind == unix.S_IFDIR || kind == unix.S_IFREG {
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if kind == unix.S_IFDIR {
			flags |= unix.O_DIRECTORY
		}
		fd, err := unix.Openat(parent, name, flags, 0)
		if err != nil {
			return err
		}
		f = os.NewFile(uintptr(fd), name)
		defer f.Close()
		var opened unix.Stat_t
		if err := unix.Fstat(fd, &opened); err != nil {
			return err
		}
		if !preservationDigestSameEntry(&before, &opened) {
			return fmt.Errorf("%q changed while opening", rel)
		}
	}

	switch kind {
	case unix.S_IFDIR:
		record.Type = "directory"
		if err := enc.Encode(record); err != nil {
			return err
		}
		entries, err := f.ReadDir(-1)
		if err != nil {
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if err := preservationDigestEntry(enc, int(f.Fd()), entry.Name(), filepath.ToSlash(filepath.Join(rel, entry.Name()))); err != nil {
				return err
			}
		}
	case unix.S_IFREG:
		if before.Size < 0 {
			return fmt.Errorf("%q has a negative size", rel)
		}
		record.Type, record.Size = "file", before.Size
		content := sha256.New()
		// Bound the read to the observed size plus one EOF probe. Large files
		// are streamed without imposing a limit on the total tree size.
		if _, err := io.CopyN(content, f, before.Size); err != nil {
			return fmt.Errorf("read %q: %w", rel, err)
		}
		var extra [1]byte
		if n, err := f.Read(extra[:]); n != 0 || err != io.EOF {
			return fmt.Errorf("%q changed length while reading (error %v)", rel, err)
		}
		record.SHA256 = hex.EncodeToString(content.Sum(nil))
		if err := enc.Encode(record); err != nil {
			return err
		}
	case unix.S_IFLNK:
		record.Type = "symlink"
		// Linux symlink targets are small; reject unusual metadata rather
		// than allocate an unbounded buffer or follow the link.
		if before.Size < 0 || before.Size > 1<<20 {
			return fmt.Errorf("%q has an unsupported symlink size", rel)
		}
		target := make([]byte, int(before.Size)+1)
		n, err := unix.Readlinkat(parent, name, target)
		if err != nil {
			return err
		}
		if int64(n) != before.Size {
			return fmt.Errorf("%q changed while reading its symlink target", rel)
		}
		record.Target = target[:n]
		if err := enc.Encode(record); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported special file %q", rel)
	}

	var after unix.Stat_t
	if f != nil {
		if err := unix.Fstat(int(f.Fd()), &after); err != nil {
			return err
		}
		if !preservationDigestSameEntry(&before, &after) {
			return fmt.Errorf("%q changed while reading", rel)
		}
	}
	if err := unix.Fstatat(parent, name, &after, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if !preservationDigestSameEntry(&before, &after) {
		return fmt.Errorf("%q was replaced or changed while reading", rel)
	}
	return nil
}

func preservationDigestSameEntry(a, b *unix.Stat_t) bool {
	// Ignore atime: our own reads may update it. Ctime also detects in-place
	// changes whose mtime was restored, and identity detects replacements.
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode &&
		a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}
