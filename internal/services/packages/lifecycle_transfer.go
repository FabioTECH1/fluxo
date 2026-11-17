package packages

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// LifecycleTransfer identifies a designated descriptor of the ORIGINAL shared
// flock open-file description. It is durable bootstrap-seal data, not permission
// to acquire a new lease or a generic inherited-FD bypass.
type LifecycleTransfer struct {
	FD           int    `json:"fd"`
	Device       uint64 `json:"device"`
	Inode        uint64 `json:"inode"`
	OwnerPID     int    `json:"owner_pid"`
	MarkerOffset int64  `json:"marker_offset"`
}

// LifecycleLease owns a continuously held shared lease. Its inherited duplicate
// is raw rather than an os.File, so a Go finalizer cannot close it during exec.
// The owner must not Close between PrepareExecTransfer and successful exec.
type LifecycleLease struct {
	file      *os.File
	path      string
	duplicate int
	close     sync.Once
}

func AcquireLifecycleSharedLease(ctx context.Context) (*LifecycleLease, error) {
	return acquireLifecycleSharedLease(ctx, LifecycleLockPath)
}
func acquireLifecycleSharedLease(ctx context.Context, path string) (*LifecycleLease, error) {
	f, e := acquireFile(ctx, path, true)
	if e != nil {
		return nil, e
	}
	return &LifecycleLease{file: f, path: path, duplicate: -1}, nil
}
func (l *LifecycleLease) Close() {
	if l == nil {
		return
	}
	l.close.Do(func() {
		if l.file != nil {
			_ = unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
			_ = l.file.Close()
		}
		if l.duplicate >= 0 {
			_ = unix.Close(l.duplicate)
		}
	})
}

func (l *LifecycleLease) PrepareExecTransfer() (LifecycleTransfer, error) {
	var t LifecycleTransfer
	if l == nil || l.file == nil || l.duplicate >= 0 {
		return t, errors.New("lifecycle lease is absent or already prepared")
	}
	var st unix.Stat_t
	if e := unix.Fstat(int(l.file.Fd()), &st); e != nil {
		return t, e
	}
	// The byte-range marker is owned by this SAME open-file description.
	// It is independent of flock and changes no file contents. A fresh open
	// cannot masquerade as this description: its GETLK sees our marker.
	var position int64
	for tries := 0; tries < 16; tries++ {
		var nonce [8]byte
		if _, e := rand.Read(nonce[:]); e != nil {
			return t, e
		}
		position = int64(binary.LittleEndian.Uint64(nonce[:])&((1<<62)-1)) + 1
		marker := unix.Flock_t{Type: unix.F_WRLCK, Whence: 0, Start: position, Len: 1}
		e := unix.FcntlFlock(l.file.Fd(), unix.F_OFD_SETLK, &marker)
		if e == nil {
			break
		}
		if !errors.Is(e, unix.EAGAIN) && !errors.Is(e, unix.EACCES) {
			return t, e
		}
		position = 0
	}
	if position == 0 {
		return t, errors.New("could not reserve unique lifecycle OFD marker")
	}
	fd, e := unix.FcntlInt(l.file.Fd(), unix.F_DUPFD_CLOEXEC, 3)
	if e != nil {
		return t, e
	}
	t = LifecycleTransfer{FD: fd, Device: uint64(st.Dev), Inode: st.Ino, OwnerPID: os.Getpid(), MarkerOffset: position}
	if e = verifyLifecycleTransfer(fd, t, l.path); e != nil {
		unix.Close(fd)
		return LifecycleTransfer{}, e
	}
	l.duplicate = fd
	return t, nil
}

// ArmExecTransfer must run after CLOSE_RANGE_CLOEXEC. It changes only the
// designated descriptor; every other private FD remains close-on-exec.
func (l *LifecycleLease) ArmExecTransfer(t LifecycleTransfer) error {
	if l == nil || l.duplicate != t.FD {
		return errors.New("foreign lifecycle transfer descriptor")
	}
	if e := verifyLifecycleTransfer(t.FD, t, l.path); e != nil {
		return e
	}
	_, e := unix.FcntlInt(uintptr(t.FD), unix.F_SETFD, 0)
	return e
}

// AdoptInheritedLifecycleLease restores CLOEXEC BEFORE interpreting the lease.
// It never unlocks, reacquires or upgrades the inherited flock. The scheduler
// separately binds owner PID/starttime and this descriptor to its one-use seal.
func AdoptInheritedLifecycleLease(t LifecycleTransfer) (*LifecycleLease, error) {
	return adoptInheritedLifecycleLease(t, LifecycleLockPath)
}
func adoptInheritedLifecycleLease(t LifecycleTransfer, path string) (*LifecycleLease, error) {
	if t.FD < 3 {
		return nil, errors.New("lifecycle transfer cannot use a standard descriptor")
	}
	if _, e := unix.FcntlInt(uintptr(t.FD), unix.F_SETFD, unix.FD_CLOEXEC); e != nil {
		return nil, e
	}
	if e := verifyLifecycleTransfer(t.FD, t, path); e != nil {
		return nil, e
	}
	return &LifecycleLease{file: os.NewFile(uintptr(t.FD), "inherited lifecycle lease"), path: path, duplicate: -1}, nil
}
func verifyLifecycleTransfer(fd int, t LifecycleTransfer, path string) error {
	if fd < 3 || fd != t.FD || t.OwnerPID <= 1 || t.Inode == 0 || t.MarkerOffset <= 0 {
		return errors.New("invalid lifecycle transfer identity")
	}
	var st, named unix.Stat_t
	if e := unix.Fstat(fd, &st); e != nil {
		return e
	}
	if e := unix.Lstat(path, &named); e != nil {
		return e
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || named.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Geteuid()) || named.Uid != uint32(os.Geteuid()) || st.Nlink != 1 || named.Nlink != 1 || st.Mode&0022 != 0 || named.Mode&0022 != 0 || uint64(st.Dev) != t.Device || st.Ino != t.Inode || st.Dev != named.Dev || st.Ino != named.Ino {
		return errors.New("lifecycle lease named inode or ownership changed")
	}
	flags, e := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if e != nil {
		return e
	}
	if flags&unix.O_ACCMODE != unix.O_RDWR || flags&unix.O_PATH != 0 {
		return errors.New("lifecycle descriptor mode changed")
	}
	if e = verifyLifecycleOFDMarker(fd, t, path); e != nil {
		return e
	}
	raw, e := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", fd))
	if e != nil {
		return e
	}
	return validateLifecycleFDInfo(string(raw), t)
}
func verifyLifecycleOFDMarker(fd int, t LifecycleTransfer, path string) error {
	own := unix.Flock_t{Type: unix.F_WRLCK, Whence: 0, Start: t.MarkerOffset, Len: 1}
	if e := unix.FcntlFlock(uintptr(fd), unix.F_OFD_GETLK, &own); e != nil {
		return e
	}
	if own.Type != unix.F_UNLCK {
		return errors.New("claimed lifecycle descriptor does not own its OFD marker")
	}
	observer, e := unix.Open(path, unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return e
	}
	defer unix.Close(observer)
	var st unix.Stat_t
	if e = unix.Fstat(observer, &st); e != nil {
		return e
	}
	if uint64(st.Dev) != t.Device || st.Ino != t.Inode {
		return errors.New("lifecycle named inode changed during OFD proof")
	}
	other := unix.Flock_t{Type: unix.F_WRLCK, Whence: 0, Start: t.MarkerOffset, Len: 1}
	if e = unix.FcntlFlock(uintptr(observer), unix.F_OFD_GETLK, &other); e != nil {
		return e
	}
	if other.Type != unix.F_WRLCK || other.Start != t.MarkerOffset || other.Len != 1 || other.Pid != -1 {
		return errors.New("lifecycle OFD marker is absent or not the inherited write lock")
	}
	return nil
}

func validateLifecycleFDInfo(raw string, t LifecycleTransfer) error {
	matches := 0
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(line, "lock:") {
			continue
		}
		f := strings.Fields(strings.TrimPrefix(line, "lock:"))
		if len(f) != 8 || f[2] != "ADVISORY" {
			return errors.New("invalid lifecycle descriptor lock metadata")
		}
		parts := strings.Split(f[5], ":")
		if len(parts) != 3 {
			return errors.New("invalid lifecycle flock identity")
		}
		major, e1 := strconv.ParseUint(parts[0], 16, 32)
		minor, e2 := strconv.ParseUint(parts[1], 16, 32)
		ino, e3 := strconv.ParseUint(parts[2], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil || major != uint64(unix.Major(t.Device)) || minor != uint64(unix.Minor(t.Device)) || ino != t.Inode {
			return errors.New("lifecycle descriptor lock belongs to another inode")
		}
		if f[1] == "OFDLCK" {
			if f[3] != "WRITE" || f[4] != "-1" || f[6] != strconv.FormatInt(t.MarkerOffset, 10) || f[7] != f[6] {
				return errors.New("lifecycle OFD marker metadata differs")
			}
			continue
		}
		if f[1] != "FLOCK" || f[3] != "READ" || f[4] != strconv.Itoa(t.OwnerPID) || f[6] != "0" || f[7] != "EOF" {
			return errors.New("lifecycle descriptor lacks its exact inherited shared flock")
		}
		matches++
	}
	if matches != 1 {
		return errors.New("lifecycle descriptor has no unique owned shared flock")
	}
	return nil
}
