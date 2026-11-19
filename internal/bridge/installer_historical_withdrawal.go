package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"fluxo/internal/services/executiondomain"
	"golang.org/x/sys/unix"
)

const historicalWithdrawal = "withdraw-installer-historical"

// A published archive can precede all live writes. In this exact two-existing-
// core transition, the writer first creates directories, then writes images in
// plan order. A distinct, still-original first target excludes even a rewrite
// of an identical image. A staged archive precedes the whole transition.
func historicalFirstWriteDistinct(p executionActivation) bool {
	for _, f := range p.Files {
		if f.Lock || len(f.Fence) != 0 {
			return false
		}
	}
	for _, f := range p.Files {
		if f.Directory {
			return f.Before == "absent"
		}
	}
	for _, f := range p.Files {
		if f.Image {
			return f.Before != p.BinarySHA256
		}
	}
	return false
}

func historicalWithdrawalTarget(f executiondomain.ActivationFile, before, raw []byte, info os.FileInfo) error {
	if info.Mode().Perm() != os.FileMode(f.Mode) || f.Cron && info.ModTime().Unix() == f.CronMTime {
		return errors.New("historical target metadata differs from source")
	}
	switch f.Path {
	case "/var/lib/fluxo-system-tasks/state.json":
		if bytes.Equal(raw, before) || bytes.Equal(raw, f.Data) {
			return errors.New("historical task target lacks a distinct old-writer projection")
		}
		return nil // The caller separately proves exact historical compaction under both gates.
	case "/var/lib/fluxo-scheduler/state.json":
		var header struct{ Schema int }
		if json.Unmarshal(before, &header) != nil || header.Schema != 2 {
			return errors.New("historical withdrawal requires an existing schema2 Scheduler")
		}
	}
	if digest(raw) != f.Before {
		return fmt.Errorf("historical target differs from original: %s", f.Path)
	}
	return nil
}

// The floorless reader treats the directory itself as authority. Never remove
// another operation; rmdir also checks emptiness atomically after the move.
func checkHistoricalArchiveNamespace(parent, archive string) error {
	info, err := os.Lstat(parent)
	if os.IsNotExist(err) && filepath.Dir(archive) != parent {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("invalid historical activation namespace")
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return err
	}
	if filepath.Dir(archive) == parent && len(entries) == 1 && entries[0].Name() == filepath.Base(archive) && entries[0].IsDir() {
		return nil
	}
	if filepath.Dir(archive) != parent && len(entries) == 0 {
		return nil
	}
	return errors.New("unrelated floorless activation archive")
}

func finishHistoricalArchiveMove(parent, retired string) error {
	if err := checkHistoricalArchiveNamespace(parent, retired); err != nil {
		return err
	}
	if err := unix.Rmdir(parent); err != nil && !os.IsNotExist(err) {
		return err
	}
	return syncActivationDirectory(filepath.Dir(parent))
}
