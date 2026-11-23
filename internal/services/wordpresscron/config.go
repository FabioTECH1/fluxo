// Package wordpresscron owns the optional, per-site system trigger for WP-Cron.
// Execution is an ordinary guarded application job; runtime maintenance keeps
// its existing launch, credential, acknowledgement and drainage contracts.
package wordpresscron

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const Block = "\n// BEGIN FLUXO WORDPRESS CRON\ndefine('DISABLE_WP_CRON', true);\n// END FLUXO WORDPRESS CRON\n"

// Config preserves every byte outside the exact owned block. Complex/custom
// scheduling policies are left to their owner instead of rewriting PHP code.
func Config(raw []byte, enabled bool) ([]byte, error) {
	if !bytes.HasPrefix(raw, []byte("<?php")) || len(raw) > 1<<20 {
		return nil, errors.New("WordPress Cron needs a standard wp-config.php smaller than 1 MB")
	}
	base := bytes.Replace(raw, []byte(Block), nil, 1)
	upper := strings.ToUpper(string(base))
	if strings.Contains(upper, "DISABLE_WP_CRON") || strings.Contains(upper, "FLUXO WORDPRESS CRON") || strings.Contains(upper, "MULTISITE") || strings.Contains(upper, "ALTERNATE_WP_CRON") {
		return nil, errors.New("Custom or multisite WordPress cron configuration needs manual review; it was preserved")
	}
	if !enabled {
		return base, nil
	}
	return append(append(append([]byte{}, base[:5]...), []byte(Block)...), base[5:]...), nil
}

func regular(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || st.Nlink != 1 || info.Size() > 1<<20 {
		return nil, errors.New("WordPress cron files must be regular, single-link files smaller than 1 MB")
	}
	return info, nil
}

func readConfig(path string) ([]byte, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	if resolved != path {
		return nil, errors.New("WordPress Cron does not modify symlinked configuration")
	}
	if _, err = regular(path); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if len(raw) > 1<<20 {
		return nil, errors.New("WordPress configuration exceeds 1 MB")
	}
	return raw, err
}

func writeConfig(path string, previous, next []byte, lint func(string) error) error {
	info, err := regular(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".fluxo-wp-cron-*.php")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err = tmp.Write(next); err != nil {
		return err
	}
	st := info.Sys().(*syscall.Stat_t)
	if err = tmp.Chown(int(st.Uid), int(st.Gid)); err != nil {
		return err
	}
	if err = tmp.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = lint(tmp.Name()); err != nil {
		return err
	}
	// Detect concurrent editor/application changes immediately before rename.
	current, err := readConfig(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, previous) {
		return errors.New("WordPress configuration changed; try again after reviewing it")
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
