package scheduler

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// CronBackendRetirement is installer conversion evidence, never launch authority.
// The original identity remains in the immutable activation source archive.
type CronBackendRetirement struct {
	Saved   CronBackendIdentity `json:"saved"`
	Current CronBackendIdentity `json:"current"`
}

// ValidateInstallerHistoricalRetirement deliberately covers only the historical
// Noble Cronie producer. Other provider/format changes retain their old refusal.
func ValidateInstallerHistoricalRetirement(r CronBackendRetirement) error {
	a, b := r.Saved, r.Current
	for _, start := range []string{a.Start, b.Start} {
		if n, err := strconv.ParseUint(start, 10, 64); err != nil || n == 0 {
			return errors.New("invalid historical daemon start identity")
		}
	}
	if ValidateBackendIdentity(a) != nil || ValidateBackendIdentity(b) != nil ||
		a.Backend != "cronie-noble-1" || a.Backend != b.Backend || a.Boot != b.Boot ||
		a.Group != b.Group || a.Configuration != b.Configuration || a.GroupInode == b.GroupInode ||
		a.PID == b.PID && a.Start == b.Start {
		return errors.New("historical installer retirement requires a replaced same-boot Cronie generation under its unchanged contract")
	}
	return nil
}

// VerifyInstallerHistoricalRetirement is read-only. Callers retain exclusive
// lifecycle admission and separately prove every recorded invocation settled.
// A new MainPID, empty replacement cgroup or absent old PID alone is insufficient:
// the old cgroup inode must be absent from the entire visible unified hierarchy.
func VerifyInstallerHistoricalRetirement(ctx context.Context, r CronBackendRetirement) error {
	if err := ValidateInstallerHistoricalRetirement(r); err != nil {
		return err
	}
	return verifyInstallerHistoricalRetirement(r,
		func() (legacyHost, error) { return RequalifyCronBackend(ctx, r.Saved) },
		func() error { return VerifyCronBackendEmpty(ctx, r.Current) },
		startKey, func(inode uint64) error { return retiredUnifiedHierarchy(ctx, inode) })
}

func verifyInstallerHistoricalRetirement(r CronBackendRetirement, qualify func() (legacyHost, error), empty func() error, process func(int) (string, error), retired func(uint64) error) error {
	if err := ValidateInstallerHistoricalRetirement(r); err != nil {
		return err
	}
	check := func() error {
		now, err := qualify()
		if err != nil {
			return err
		}
		if now != r.Current {
			return errors.New("Cronie generation changed during historical retirement")
		}
		key, err := process(r.Saved.PID)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && key == r.Saved.Start {
			return errors.New("historical Cronie daemon still owns submission authority")
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	if err := retired(r.Saved.GroupInode); err != nil {
		return err
	}
	if err := empty(); err != nil {
		return err
	}
	return check()
}

func retiredUnifiedHierarchy(ctx context.Context, inode uint64) error {
	const root = "/sys/fs/cgroup"
	selfNS, err := os.Stat("/proc/self/ns/cgroup")
	if err != nil {
		return err
	}
	initNS, err := os.Stat("/proc/1/ns/cgroup")
	if err != nil {
		return err
	}
	if !os.SameFile(selfNS, initNS) {
		return errors.New("historical retirement requires the service manager cgroup namespace")
	}
	before, err := os.Stat(root)
	if err != nil {
		return err
	}
	var st unix.Statfs_t
	if err := unix.Statfs(root, &st); err != nil {
		return err
	}
	if uint64(st.Type) != uint64(unix.CGROUP2_SUPER_MAGIC) {
		return errors.New("historical retirement requires unified cgroup2")
	}
	// Reject a subtree mount: absence there says nothing about old siblings.
	raw, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	full := false
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 6 && strings.HasPrefix(fields[4], root+"/") {
			return errors.New("historical retirement refuses hidden cgroup submounts")
		}
		if len(fields) > 6 && fields[4] == root && fields[3] == "/" && strings.Contains(line, " - cgroup2 ") {
			full = true
		}
	}
	if !full {
		return errors.New("historical retirement cannot inspect the complete cgroup hierarchy")
	}
	if err = scanRetiredHierarchy(ctx, root, inode); err != nil {
		return err
	}
	after, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !os.SameFile(before, after) {
		return errors.New("cgroup hierarchy changed during retirement")
	}
	return nil
}

func scanRetiredHierarchy(ctx context.Context, root string, inode uint64) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return errors.New("unknown cgroup identity")
		}
		if st.Ino == inode {
			return fmt.Errorf("historical Cronie execution hierarchy still exists; wait for natural retirement")
		}
		return nil
	})
}
