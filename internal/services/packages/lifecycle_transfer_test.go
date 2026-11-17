package packages

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func transferFixture(t *testing.T) (*LifecycleLease, LifecycleTransfer, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lifecycle.lock")
	l, e := acquireLifecycleSharedLease(context.Background(), path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(l.Close)
	tr, e := l.PrepareExecTransfer()
	if e != nil {
		t.Fatal(e)
	}
	return l, tr, path
}
func TestLifecycleTransferPreservesOriginalOFDAndExclusion(t *testing.T) {
	l, tr, path := transferFixture(t)
	if e := l.ArmExecTransfer(tr); e != nil {
		t.Fatal(e)
	}
	flags, e := unix.FcntlInt(uintptr(tr.FD), unix.F_GETFD, 0)
	if e != nil || flags&unix.FD_CLOEXEC != 0 {
		t.Fatal("designated FD not armed", e)
	}
	inherited, e := adoptInheritedLifecycleLease(tr, path)
	if e != nil {
		t.Fatal(e)
	}
	// Model exec closing the original CLOEXEC descriptor, without unlocking.
	// Only the inherited duplicate remains; its OFD must continuously hold both
	// the original shared flock and the independent byte-range marker.
	l.duplicate = -1
	if e = l.file.Close(); e != nil {
		t.Fatal(e)
	}
	l.file = nil
	defer inherited.Close()
	flags, e = unix.FcntlInt(uintptr(tr.FD), unix.F_GETFD, 0)
	if e != nil || flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("worker did not restore CLOEXEC", e)
	}
	observer, e := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer unix.Close(observer)
	if e = unix.Flock(observer, unix.LOCK_EX|unix.LOCK_NB); !errors.Is(e, unix.EWOULDBLOCK) {
		t.Fatal("exclusive contender crossed inherited OFD", e)
	}
	if e = verifyLifecycleTransfer(tr.FD, tr, path); e != nil {
		t.Fatal(e)
	}
}
func TestLifecycleTransferRejectsFreshSameInodeDescriptor(t *testing.T) {
	_, tr, path := transferFixture(t)
	fresh, e := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer unix.Close(fresh)
	// A fresh fd can itself acquire a shared flock while the old shared lease
	// exists. Device/inode/FLOCK metadata alone would therefore accept it.
	if e = unix.Flock(fresh, unix.LOCK_SH|unix.LOCK_NB); e != nil {
		t.Fatal(e)
	}
	wrong := tr
	wrong.FD = fresh
	if e = verifyLifecycleTransfer(fresh, wrong, path); e == nil {
		t.Fatal("fresh same-inode open masqueraded as inherited OFD")
	}
}
func TestLifecycleTransferRejectsLostMarkerAndReplacedPath(t *testing.T) {
	t.Run("marker", func(t *testing.T) {
		l, tr, path := transferFixture(t)
		marker := unix.Flock_t{Type: unix.F_UNLCK, Whence: 0, Start: tr.MarkerOffset, Len: 1}
		if e := unix.FcntlFlock(l.file.Fd(), unix.F_OFD_SETLK, &marker); e != nil {
			t.Fatal(e)
		}
		if e := verifyLifecycleTransfer(tr.FD, tr, path); e == nil {
			t.Fatal("missing OFD marker admitted")
		}
	})
	t.Run("named inode", func(t *testing.T) {
		_, tr, path := transferFixture(t)
		if e := os.Rename(path, path+".retained"); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(path, nil, 0600); e != nil {
			t.Fatal(e)
		}
		if e := verifyLifecycleTransfer(tr.FD, tr, path); e == nil {
			t.Fatal("named lifecycle inode replacement admitted")
		}
	})
	t.Run("flock", func(t *testing.T) {
		l, tr, path := transferFixture(t)
		if e := unix.Flock(int(l.file.Fd()), unix.LOCK_UN); e != nil {
			t.Fatal(e)
		}
		if e := verifyLifecycleTransfer(tr.FD, tr, path); e == nil {
			t.Fatal("OFD marker substituted for missing lifecycle flock")
		}
	})
}
