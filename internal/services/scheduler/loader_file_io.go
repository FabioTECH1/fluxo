package scheduler

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const loaderFileBudget = 64 << 20

// Production always uses protected root-owned paths and real writer admission.
// A package-private policy seam lets filesystem tests exercise the exported I/O
// on isolated user-owned fixtures without fabricating a global execution floor.
type loaderFilePolicy interface {
	ownerUID() uint32
	trustFile(string) error
	trustDirectory(string) error
	admitWriter(context.Context) error
}
type rootLoaderFilePolicy struct{}

func (rootLoaderFilePolicy) ownerUID() uint32            { return 0 }
func (rootLoaderFilePolicy) trustFile(path string) error { return trustedFile(path) }
func (rootLoaderFilePolicy) trustDirectory(path string) error {
	for dir := path; ; dir = filepath.Dir(dir) {
		if err := owned(dir, true); err != nil {
			return err
		}
		if dir == "/" {
			return nil
		}
	}
}
func (rootLoaderFilePolicy) admitWriter(ctx context.Context) error { return requireLoaderWriter(ctx) }

var loaderFiles loaderFilePolicy = rootLoaderFilePolicy{}

func loaderSameFileStat(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

func readLoaderProtectedFile(path string, limit int64) ([]byte, unix.Stat_t, error) {
	var before unix.Stat_t
	if err := unix.Lstat(path, &before); err != nil {
		return nil, before, err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Uid != loaderFiles.ownerUID() || before.Mode&0022 != 0 || before.Nlink != 1 || before.Size < 0 || before.Size > limit {
		return nil, before, errors.New("unsafe or oversized loader regular file")
	}
	if err := loaderFiles.trustDirectory(filepath.Dir(path)); err != nil {
		return nil, before, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, before, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var opened unix.Stat_t
	if err = unix.Fstat(fd, &opened); err != nil {
		return nil, before, err
	}
	if !loaderSameFileStat(before, opened) {
		return nil, before, errors.New("loader file changed before bounded read")
	}
	if err = loaderFiles.trustDirectory(filepath.Dir(path)); err != nil {
		return nil, before, err
	}
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, before, err
	}
	if int64(len(raw)) > limit {
		return nil, before, errors.New("loader file exceeded read budget")
	}
	var after, pathAfter unix.Stat_t
	if err = unix.Fstat(fd, &after); err != nil {
		return nil, before, err
	}
	if err = unix.Lstat(path, &pathAfter); err != nil {
		return nil, before, err
	}
	if !loaderSameFileStat(before, after) || !loaderSameFileStat(before, pathAfter) || int64(len(raw)) != before.Size {
		return nil, before, errors.New("loader file changed during bounded read")
	}
	return raw, before, nil
}

func loaderPathAbsent(path string) (bool, error) {
	var st unix.Stat_t
	err := unix.Lstat(path, &st)
	if err == unix.ENOENT {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

// No replacement is ever authorized for an immutable archive, even if its
// existing name is a dangling link. A complete fsynced temporary image is
// published using the kernel NOREPLACE primitive, then the directory is fsynced.
func createLoaderArchiveNoReplace(path string, raw []byte) error {
	if len(raw) == 0 || len(raw) > loaderFileBudget {
		return errors.New("loader archive exceeds write budget")
	}
	if err := loaderFiles.trustDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	absent, err := loaderPathAbsent(path)
	if err != nil {
		return err
	}
	if !absent {
		return verifyExistingLoaderArchiveBytes(path, raw)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".loader-archive-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = loaderFiles.trustDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	if err = unix.Renameat2(unix.AT_FDCWD, name, unix.AT_FDCWD, path, unix.RENAME_NOREPLACE); err != nil {
		if err == unix.EEXIST {
			return verifyExistingLoaderArchiveBytes(path, raw)
		}
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func verifyExistingLoaderArchiveBytes(path string, want []byte) error {
	actual, _, err := readLoaderProtectedFile(path, loaderFileBudget)
	if err != nil {
		return err
	}
	if string(actual) != string(want) {
		return errors.New("existing immutable loader archive conflicts with recorded intent")
	}
	return nil
}
