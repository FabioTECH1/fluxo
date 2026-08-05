package processlog

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

// Prepare creates a process log that its service user can append to while
// keeping the shared log directory protected from file replacement attacks.
func Prepare(path, serviceUser string) error {
	logDir := filepath.Dir(path)
	if err := secureDirectory(logDir); err != nil {
		return err
	}

	uid, gid, err := serviceUserIDs(serviceUser)
	if err != nil {
		return err
	}
	return prepareFile(path, uid, gid)
}

// Repair removes an unsafe legacy log entry after securing its parent
// directory, then creates a fresh regular file for the service user.
func Repair(path, serviceUser string) error {
	logDir := filepath.Dir(path)
	if err := secureDirectory(logDir); err != nil {
		return err
	}
	uid, gid, err := serviceUserIDs(serviceUser)
	if err != nil {
		return err
	}
	return repairFile(path, uid, gid)
}

// IsSafe reports whether a process log is absent or is a single-link regular
// file. Symlinks and hardlinks must be repaired while their job is disabled.
func IsSafe(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return info.Mode().IsRegular() && ok && stat.Nlink == 1, nil
}

func secureDirectory(logDir string) error {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("create process log directory: %w", err)
	}
	dirInfo, err := os.Lstat(logDir)
	if err != nil {
		return fmt.Errorf("inspect process log directory: %w", err)
	}
	if !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("process log directory is not a real directory")
	}
	if err := os.Chown(logDir, 0, 0); err != nil {
		return fmt.Errorf("secure process log directory ownership: %w", err)
	}
	if err := os.Chmod(logDir, 0755); err != nil {
		return fmt.Errorf("secure process log directory mode: %w", err)
	}
	return nil
}

func serviceUserIDs(serviceUser string) (int, int, error) {
	u, err := user.Lookup(serviceUser)
	if err != nil {
		return 0, 0, fmt.Errorf("look up process user %s: %w", serviceUser, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse process user uid: %w", err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse process user gid: %w", err)
	}
	return uid, gid, nil
}

func repairFile(path string, uid, gid int) error {
	safe, err := IsSafe(path)
	if err != nil {
		return fmt.Errorf("inspect legacy process log: %w", err)
	}
	if !safe {
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove unsafe legacy process log: %w", err)
		}
	}
	return prepareFile(path, uid, gid)
}

func prepareFile(path string, uid, gid int) error {
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_APPEND|syscall.O_CREAT|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0640)
	if err != nil {
		return fmt.Errorf("open process log safely: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect process log: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Nlink != 1 {
		return fmt.Errorf("process log must be a regular file with one link")
	}
	if err := file.Chown(uid, gid); err != nil {
		return fmt.Errorf("set process log ownership: %w", err)
	}
	if err := file.Chmod(0640); err != nil {
		return fmt.Errorf("set process log mode: %w", err)
	}
	return nil
}
