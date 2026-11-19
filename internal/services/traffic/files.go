package traffic

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Open every component relative to the already verified directory descriptor.
// Sticky root-owned /tmp is permitted as an ancestor for development/test data.
func protectedDir(name string, create bool) (*os.File, error) {
	name, err := filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Clean(name), "/"), "/") {
		if part == "" {
			continue
		}
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(e, unix.ENOENT) && create {
			e = unix.Mkdirat(fd, part, 0700)
			if e == nil || errors.Is(e, unix.EEXIST) {
				next, e = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			}
		}
		unix.Close(fd)
		if e != nil {
			return nil, e
		}
		fd = next
		var st unix.Stat_t
		if e = unix.Fstat(fd, &st); e != nil {
			unix.Close(fd)
			return nil, e
		}
		stickyRoot := st.Uid == 0 && st.Mode&unix.S_ISVTX != 0
		if (st.Uid != 0 && int(st.Uid) != os.Geteuid()) || st.Mode&0022 != 0 && !stickyRoot {
			unix.Close(fd)
			return nil, fmt.Errorf("traffic directory is not protected: %s (component %s mode %o uid %d)", name, part, st.Mode, st.Uid)
		}
	}
	return os.NewFile(uintptr(fd), name), nil
}

func openTrafficFile(name string) (*os.File, unix.Stat_t, error) {
	return openTrafficFileTrusted(name, nil)
}
func openTrafficFileTrusted(name string, trusted []uint32) (*os.File, unix.Stat_t, error) {
	var st unix.Stat_t
	if !filepath.IsAbs(name) || filepath.Clean(name) != name {
		return nil, st, errors.New("traffic source must be an absolute clean path")
	}
	dir, err := protectedDir(filepath.Dir(name), false)
	if err != nil {
		return nil, st, err
	}
	defer dir.Close()
	fd, err := unix.Openat(int(dir.Fd()), filepath.Base(name), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, st, err
	}
	f := os.NewFile(uintptr(fd), name)
	if err = unix.Fstat(fd, &st); err != nil {
		f.Close()
		return nil, st, err
	}
	ownerOK := st.Uid == 0 || int(st.Uid) == os.Geteuid()
	for _, uid := range trusted {
		if st.Uid == uid {
			ownerOK = true
		}
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 || !ownerOK || st.Mode&0022 != 0 {
		f.Close()
		return nil, st, errors.New("traffic source must be a protected single-link regular file")
	}
	return f, st, nil
}
