package nodeupdate

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// removeStartPermit only revokes this generation's exact regular-file permit.
// Its caller validates the unit/generation and any persistent barrier ownership.
func removeStartPermit(u Unit, generation string) error {
	p := permitPath(u, generation)
	// /run or the generation directory can be absent after reboot. Validate the
	// nearest existing ancestor even when the permit itself is already absent;
	// a dangling symlink or unsafe directory must not count as clean release.
	for dir := filepath.Dir(p); ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(dir); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		if err := trustedPath(dir); err != nil {
			return err
		}
		break
	}
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.Mode().IsRegular() || owner.Uid != 0 || owner.Nlink != 1 || st.Mode().Perm()&0022 != 0 {
		return errors.New("Unsafe Node.js start permit")
	}
	expected := generation + "\n"
	content, err := io.ReadAll(io.LimitReader(f, int64(len(expected)+1)))
	if err != nil {
		return err
	}
	if string(content) != expected {
		return errors.New("Node.js start permit belongs to another generation or has changed")
	}
	now, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if !os.SameFile(st, now) || now.Mode() != st.Mode() || now.Size() != st.Size() || !now.ModTime().Equal(st.ModTime()) {
		return errors.New("Node.js start permit changed during cleanup")
	}
	if err = os.Remove(p); err != nil {
		return err
	}
	return syncDir(filepath.Dir(p))
}
