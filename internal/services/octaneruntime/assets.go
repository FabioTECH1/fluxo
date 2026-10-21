// Package octaneruntime owns the fixed non-root Octane maintenance integration.
package octaneruntime

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const Directory = "/usr/local/lib/fluxo/octane/v1"
const Launcher = Directory + "/start.php"
const MigrationMessage = "This Octane instance predates Fluxo's graceful-maintenance integration. Install the integration and complete a controlled full Octane restart after draining traffic. Worker reload is insufficient. No PHP packages have been changed."

//go:embed *.php compatibility.json THIRD_PARTY_LICENSE.txt
var assets embed.FS

func Command(branch string, port int) string {
	return fmt.Sprintf("php%s %s --host=127.0.0.1 --port=%d", branch, Launcher, port)
}

func protected(path string, directory bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != 0 || info.Mode().Perm()&0022 != 0 || (directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular()) {
		return errors.New("Octane integration path is not protected by root ownership")
	}
	return nil
}

func Verify() error {
	for dir := Directory; dir != "/"; dir = filepath.Dir(dir) {
		if err := protected(dir, true); err != nil {
			return err
		}
	}
	files, _ := assets.ReadDir(".")
	for _, file := range files {
		path := filepath.Join(Directory, file.Name())
		if err := protected(path, false); err != nil {
			return err
		}
		want, _ := assets.ReadFile(file.Name())
		got, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(want, got) {
			return errors.New("Installed Octane integration differs from this Fluxo release")
		}
	}
	return nil
}

// Install immutable versioned assets. Never overwrite a different installation
// or traverse a symlink; upgrading the integration requires a new version path.
func Install() error {
	if os.Geteuid() != 0 {
		return errors.New("Installing the Octane integration requires root")
	}
	for _, dir := range []string{"/usr/local", "/usr/local/lib", "/usr/local/lib/fluxo", "/usr/local/lib/fluxo/octane", Directory} {
		if err := os.Mkdir(dir, 0755); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		if err := protected(dir, true); err != nil {
			return err
		}
	}
	files, _ := assets.ReadDir(".")
	for _, file := range files {
		path := filepath.Join(Directory, file.Name())
		data, _ := assets.ReadFile(file.Name())
		if existing, err := os.ReadFile(path); err == nil {
			if err = protected(path, false); err != nil {
				return err
			}
			if !bytes.Equal(data, existing) {
				return errors.New("Refusing to replace a different Octane integration")
			}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		f, err := os.CreateTemp(Directory, ".install-")
		if err != nil {
			return err
		}
		tmp := f.Name()
		if err = f.Chmod(0644); err == nil {
			_, err = f.Write(data)
		}
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Link(tmp, path)
		}
		_ = os.Remove(tmp)
		if err != nil {
			return err
		}
	}
	dir, err := os.Open(Directory)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return err
	}
	return Verify()
}
