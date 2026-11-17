package bridge

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// Copy DB and WAL from one stable filesystem observation. SQLite opens only
// the private copy, where rebuilding SHM is safe. If a live checkpoint/writer
// changes either source, fail rather than reading a stale immutable main DB.
type snapshotSource struct {
	path string
	file *os.File
	info os.FileInfo
}

func snapshot(ctx context.Context, dataDir string) (string, func(), error) {
	if os.Geteuid() != 0 {
		return "", nil, errors.New("bridge inspection requires root")
	}
	if err := trustedDirectory(dataDir); err != nil {
		return "", nil, err
	}
	var sources []snapshotSource
	closeSources := func() {
		for _, s := range sources {
			s.file.Close()
		}
	}
	defer closeSources()
	for _, name := range []string{"fluxo.db", "fluxo.db-wal"} {
		path := filepath.Join(dataDir, name)
		info, err := protectedInfo(path)
		if os.IsNotExist(err) && name != "fluxo.db" {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
		if err != nil {
			return "", nil, err
		}
		f := os.NewFile(uintptr(fd), path)
		sources = append(sources, snapshotSource{path, f, info})
		opened, err := f.Stat()
		if err != nil || !sameObservation(info, opened) {
			return "", nil, errors.New("SQLite source changed before snapshot")
		}
	}
	return copySnapshot(ctx, dataDir, sources)
}

func copySnapshot(ctx context.Context, dataDir string, sources []snapshotSource) (string, func(), error) {
	dir, err := os.MkdirTemp("", "fluxo-bridge-inspect-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	ok := false
	defer func() {
		if !ok {
			cleanup()
		}
	}()
	for _, s := range sources {
		if err = ctx.Err(); err != nil {
			return "", nil, err
		}
		f, err := os.OpenFile(filepath.Join(dir, filepath.Base(s.path)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return "", nil, err
		}
		n, copyErr := io.Copy(f, io.LimitReader(contextReader{ctx, s.file}, (2<<30)+1))
		closeErr := f.Close()
		if copyErr != nil {
			return "", nil, copyErr
		}
		if closeErr != nil {
			return "", nil, closeErr
		}
		if n > 2<<30 {
			return "", nil, errors.New("SQLite snapshot exceeds the supported size bound")
		}
	}
	for _, s := range sources {
		after, err := s.file.Stat()
		if err != nil {
			return "", nil, err
		}
		current, err := os.Lstat(s.path)
		if err != nil || !sameObservation(s.info, after) || !sameObservation(s.info, current) {
			return "", nil, errors.New("SQLite changed during inspection; retry after activity settles")
		}
	}
	if len(sources) == 1 {
		if _, err = os.Lstat(filepath.Join(dataDir, "fluxo.db-wal")); !os.IsNotExist(err) {
			return "", nil, errors.New("SQLite WAL appeared during inspection; retry")
		}
	}
	ok = true
	return filepath.Join(dir, "fluxo.db"), cleanup, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}

func sameObservation(a, b os.FileInfo) bool {
	if a == nil || b == nil || !os.SameFile(a, b) || a.Size() != b.Size() || a.Mode() != b.Mode() || a.ModTime() != b.ModTime() {
		return false
	}
	x, ok := a.Sys().(*syscall.Stat_t)
	y, other := b.Sys().(*syscall.Stat_t)
	return ok && other && x.Ctim == y.Ctim && x.Uid == y.Uid && x.Gid == y.Gid && x.Nlink == y.Nlink
}
