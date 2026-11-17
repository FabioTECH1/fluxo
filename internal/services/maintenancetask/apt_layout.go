package maintenancetask

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"fluxo/internal/syscmd"
)

// The path policy is also a filesystem policy: a standard textual archive path
// must not resolve through a symlink into an unrelated deletion target.
func qualifyAPTLayout(p *profile) error {
	for _, path := range []string{"/etc/apt", "/etc/apt/apt.conf.d", "/var/lib/apt", "/var/lib/apt/lists", "/var/cache/apt", "/var/cache/apt/archives"} {
		if err := trustedDatabaseDirectory(path); err != nil {
			return fmt.Errorf("APT layout %s: %w", path, err)
		}
		if err := recordAPTDirectory(p, path); err != nil {
			return err
		}
	}
	if err := qualifyAPTLogging(p); err != nil {
		return err
	}
	apt, err := syscmd.ResolveCredential("_apt")
	if err != nil || apt.Uid == 0 {
		return errors.New("APT sandbox identity needs maintenance review")
	}
	for _, path := range []string{"/var/lib/apt/lists/partial", "/var/cache/apt/archives/partial"} {
		if err := trustedDatabaseDirectory(filepath.Dir(path)); err != nil {
			return err
		}
		// _apt may own the private leaf, but cannot replace its root-owned
		// parent entry. No other unprivileged directory owner is accepted.
		if err := recordAPTPartialDirectory(p, path, apt.Uid); err != nil {
			return err
		}
	}
	return nil
}

func recordAPTPartialDirectory(p *profile, path string, aptUID uint32) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("APT partial directory %s: %w", path, err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || aptUID == 0 || !info.IsDir() || info.Mode().Perm() != 0700 || st.Gid != 0 || st.Uid != 0 && st.Uid != aptUID || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return fmt.Errorf("unsafe APT partial directory: %s", path)
	}
	// APT's SetupAPTPartialDirectory legitimately changes a root-owned private
	// leaf to _apt. Both observations satisfy this exact data-role contract;
	// preserving actual UID in the fingerprint would reject that safe effect.
	p.External["apt-directory:"+path] = fmt.Sprintf("root-or-_apt:%d:0:700:%d:%d", aptUID, st.Dev, st.Ino)
	return nil
}

func recordAPTDirectory(p *profile, path string) error {
	i, err := os.Lstat(path)
	if err != nil {
		return err
	}
	s, ok := i.Sys().(*syscall.Stat_t)
	if !ok || !i.IsDir() {
		return fmt.Errorf("APT layout changed: %s", path)
	}
	// Contents are expected cleanup data; membership/mtime is deliberately not
	// executable evidence. Bind the protected directory itself instead.
	p.External["apt-directory:"+path] = fmt.Sprintf("%d:%d:%o:%d:%d", s.Uid, s.Gid, i.Mode().Perm(), s.Dev, s.Ino)
	return nil
}
