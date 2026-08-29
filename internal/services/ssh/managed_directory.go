package ssh

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

var ErrManagedSSHDirectoryChanged = errors.New("Fluxo SSH directory changed during the operation")

// ManagedSSHDirectory pins a no-follow descriptor for a user's .ssh directory.
// Privileged callers must perform every filesystem mutation through this store
// so a process that owns the home directory cannot redirect the operation.
type ManagedSSHDirectory struct {
	home      *os.File
	directory *os.File
	homePath  string
	uid       int
	gid       int
}

func OpenManagedSSHDirectory(homePath string, create bool, uid, gid int) (*ManagedSSHDirectory, error) {
	homeFD, err := unix.Open(homePath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open Fluxo home directory: %w", err)
	}
	home := os.NewFile(uintptr(homeFD), homePath)
	if home == nil {
		unix.Close(homeFD)
		return nil, errors.New("open Fluxo home directory")
	}
	if create {
		if err := unix.Mkdirat(homeFD, ".ssh", 0700); err != nil && !errors.Is(err, unix.EEXIST) {
			_ = home.Close()
			return nil, fmt.Errorf("create Fluxo SSH directory: %w", err)
		}
	}
	directoryFD, err := unix.Openat(homeFD, ".ssh", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		_ = home.Close()
		return nil, fmt.Errorf("open Fluxo SSH directory: %w", err)
	}
	directory := os.NewFile(uintptr(directoryFD), filepath.Join(homePath, ".ssh"))
	if directory == nil {
		unix.Close(directoryFD)
		_ = home.Close()
		return nil, errors.New("open Fluxo SSH directory")
	}
	store := &ManagedSSHDirectory{home: home, directory: directory, homePath: homePath, uid: uid, gid: gid}
	if create {
		if err := unix.Fchmod(directoryFD, 0700); err != nil {
			store.Close()
			return nil, err
		}
		if uid >= 0 {
			if err := unix.Fchown(directoryFD, uid, gid); err != nil {
				store.Close()
				return nil, err
			}
		}
	}
	if err := store.ensureCurrent(); err != nil {
		store.Close()
		return nil, err
	}
	return store, nil
}

func (s *ManagedSSHDirectory) Close() {
	if s == nil {
		return
	}
	if s.directory != nil {
		_ = s.directory.Close()
		s.directory = nil
	}
	if s.home != nil {
		_ = s.home.Close()
		s.home = nil
	}
}

func (s *ManagedSSHDirectory) Path(name string) (string, error) {
	if err := validateManagedSSHFilename(name); err != nil {
		return "", err
	}
	return filepath.Join(s.homePath, ".ssh", name), nil
}

func validateManagedSSHFilename(name string) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return errors.New("invalid managed SSH filename")
	}
	return nil
}

func (s *ManagedSSHDirectory) ensureCurrent() error {
	if s == nil || s.home == nil || s.directory == nil {
		return errors.New("Fluxo SSH directory is closed")
	}
	var opened, current unix.Stat_t
	if err := unix.Fstat(int(s.directory.Fd()), &opened); err != nil {
		return err
	}
	if err := unix.Fstatat(int(s.home.Fd()), ".ssh", &current, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return ErrManagedSSHDirectoryChanged
	}
	if current.Mode&unix.S_IFMT != unix.S_IFDIR || opened.Dev != current.Dev || opened.Ino != current.Ino {
		return ErrManagedSSHDirectoryChanged
	}
	return nil
}

func (s *ManagedSSHDirectory) ReadFile(name string) ([]byte, *unix.Stat_t, error) {
	if err := validateManagedSSHFilename(name); err != nil {
		return nil, nil, err
	}
	if err := s.ensureCurrent(); err != nil {
		return nil, nil, err
	}
	fd, err := unix.Openat(int(s.directory.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		unix.Close(fd)
		return nil, nil, errors.New("open managed SSH file")
	}
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nil, nil, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, nil, fmt.Errorf("%s is not a regular file", name)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, nil, err
	}
	if err := s.ensureCurrent(); err != nil {
		return nil, nil, err
	}
	return content, &stat, nil
}

func managedSSHTemporaryName(name string) (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "." + name + "-" + hex.EncodeToString(random), nil
}

func (s *ManagedSSHDirectory) WriteFileAtomic(name string, content []byte, mode os.FileMode) (resultErr error) {
	if err := validateManagedSSHFilename(name); err != nil {
		return err
	}
	if err := s.ensureCurrent(); err != nil {
		return err
	}
	var temporaryName string
	var fd int
	for attempts := 0; attempts < 8; attempts++ {
		candidate, err := managedSSHTemporaryName(name)
		if err != nil {
			return err
		}
		fd, err = unix.Openat(int(s.directory.Fd()), candidate,
			unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode.Perm()))
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return err
		}
		temporaryName = candidate
		break
	}
	if temporaryName == "" {
		return errors.New("unable to allocate a temporary managed SSH file")
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = unix.Unlinkat(int(s.directory.Fd()), temporaryName, 0)
		}
	}()
	if err := unix.Fchmod(fd, uint32(mode.Perm())); err != nil {
		unix.Close(fd)
		return err
	}
	if s.uid >= 0 {
		if err := unix.Fchown(fd, s.uid, s.gid); err != nil {
			unix.Close(fd)
			return err
		}
	}
	temporary := os.NewFile(uintptr(fd), temporaryName)
	if temporary == nil {
		unix.Close(fd)
		return errors.New("open temporary managed SSH file")
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := s.ensureCurrent(); err != nil {
		return err
	}
	if err := unix.Renameat(int(s.directory.Fd()), temporaryName, int(s.directory.Fd()), name); err != nil {
		return err
	}
	cleanup = false
	if err := unix.Fsync(int(s.directory.Fd())); err != nil {
		return err
	}
	return s.ensureCurrent()
}

func (s *ManagedSSHDirectory) RemoveFile(name string) error {
	if err := validateManagedSSHFilename(name); err != nil {
		return err
	}
	if err := s.ensureCurrent(); err != nil {
		return err
	}
	if err := unix.Unlinkat(int(s.directory.Fd()), name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return err
	}
	if err := unix.Fsync(int(s.directory.Fd())); err != nil {
		return err
	}
	return s.ensureCurrent()
}
