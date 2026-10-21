package phpupdate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	"golang.org/x/sys/unix"
)

var archiveSnapshotName = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-[0-9]+$`)

type archiveSnapshotFile struct {
	Name     string
	Identity unix.Stat_t
}
type archiveSnapshot struct {
	Name     string
	Identity unix.Stat_t
	Files    []archiveSnapshotFile
	Absent   bool
}
type archiveSnapshotListing struct {
	Exists    bool
	Identity  unix.Stat_t
	Snapshots []archiveSnapshot
}

func (s *archiveRetentionStore) listSnapshots(ctx context.Context) (archiveSnapshotListing, error) {
	var result archiveSnapshotListing
	dir, st, err := retentionOpen(s.root, "upgrades", true, s.owner)
	if errors.Is(err, unix.ENOENT) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer dir.Close()
	result.Exists = true
	result.Identity = st
	names, err := retentionNames(dir)
	if err != nil {
		return result, err
	}
	if len(names) > 64 {
		return result, errors.New("too many installer snapshots to inspect safely")
	}
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		if !archiveSnapshotName.MatchString(name) {
			return result, errors.New("unrecognized installer snapshot; retaining PHP archives")
		}
		d, identity, e := retentionOpen(dir, name, true, s.owner)
		if e != nil {
			return result, e
		}
		snapshot := archiveSnapshot{Name: name, Identity: identity}
		present, absent := false, false
		for _, file := range []string{"database-present", "database-absent", "fluxo.db", "fluxo.db-wal", "fluxo.db-shm"} {
			f, metadata, e := retentionOpen(d, file, false, s.owner)
			if errors.Is(e, unix.ENOENT) {
				continue
			}
			if e != nil {
				d.Close()
				return result, e
			}
			f.Close()
			if metadata.Size > 128<<20 {
				d.Close()
				return result, errors.New("installer database snapshot exceeds PHP cleanup inspection limit")
			}
			snapshot.Files = append(snapshot.Files, archiveSnapshotFile{Name: file, Identity: metadata})
			if file == "database-present" {
				present = true
			}
			if file == "database-absent" {
				absent = true
			}
		}
		d.Close()
		hasDB := false
		for _, f := range snapshot.Files {
			if f.Name == "fluxo.db" {
				hasDB = true
			}
		}
		if present == absent || present != hasDB || (absent && len(snapshot.Files) != 1) {
			return result, errors.New("incomplete installer database snapshot; retaining PHP archives")
		}
		snapshot.Absent = absent
		result.Snapshots = append(result.Snapshots, snapshot)
	}
	return result, nil
}

func snapshotListingDigest(list archiveSnapshotListing) string {
	// atime can change simply by reading; ctime/mtime and inode identity cannot.
	list.Identity.Atim = unix.Timespec{}
	for i := range list.Snapshots {
		list.Snapshots[i].Identity.Atim = unix.Timespec{}
		for j := range list.Snapshots[i].Files {
			list.Snapshots[i].Files[j].Identity.Atim = unix.Timespec{}
		}
	}
	return digest(list)
}
func (s *archiveRetentionStore) snapshotFootprint(ctx context.Context) (string, error) {
	list, err := s.listSnapshots(ctx)
	if err != nil {
		return "", err
	}
	return snapshotListingDigest(list), nil
}

func (s *archiveRetentionStore) snapshotPins(ctx context.Context) (map[string]bool, string, error) {
	list, err := s.listSnapshots(ctx)
	if err != nil {
		return nil, "", err
	}
	protected := map[string]bool{}
	var total int64
	for _, snapshot := range list.Snapshots {
		if snapshot.Absent {
			continue
		}
		for _, f := range snapshot.Files {
			total += f.Identity.Size
		}
		if total > 512<<20 {
			return nil, "", errors.New("installer snapshot inspection exceeds PHP cleanup byte limit")
		}
		items, e := s.readSnapshotGenerations(ctx, snapshot)
		if e != nil {
			return nil, "", e
		}
		latest, forward, fallback := archiveRetentionHeads(items)
		for _, item := range items {
			g := item.Value
			if !item.Settled {
				protected[g.ID] = true
			}
		}
		for _, group := range []map[string]archiveGeneration{latest, forward, fallback} {
			for _, item := range group {
				protected[item.Value.ID] = true
			}
		}
	}
	return protected, snapshotListingDigest(list), nil
}

// Installer snapshots contain a stopped SQLite database and optional WAL/SHM.
// Copy all three to a private scratch directory before opening SQLite: neither
// recovery nor SHM creation may write into the original recovery snapshot.
func (s *archiveRetentionStore) readSnapshotGenerations(ctx context.Context, snapshot archiveSnapshot) ([]archiveGeneration, error) {
	upgrades, _, err := retentionOpen(s.root, "upgrades", true, s.owner)
	if err != nil {
		return nil, err
	}
	defer upgrades.Close()
	dir, identity, err := retentionOpen(upgrades, snapshot.Name, true, s.owner)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	if !sameRetentionIdentity(identity, snapshot.Identity) {
		return nil, errors.New("installer snapshot changed during PHP cleanup")
	}
	temporary, err := os.MkdirTemp("", "fluxo-php-retention-snapshot-")
	if err != nil {
		return nil, err
	}
	defer func() {
		for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
			_ = os.Remove(filepath.Join(temporary, "fluxo.db"+suffix))
		}
		_ = os.Remove(temporary)
	}()
	for _, file := range snapshot.Files {
		if file.Name == "database-present" || file.Name == "database-absent" {
			continue
		}
		src, before, e := retentionOpen(dir, file.Name, false, s.owner)
		if e != nil {
			return nil, e
		}
		if !sameRetentionIdentity(before, file.Identity) {
			src.Close()
			return nil, errors.New("installer database snapshot changed")
		}
		dst, e := os.OpenFile(filepath.Join(temporary, file.Name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			src.Close()
			return nil, e
		}
		n, e := io.Copy(dst, retentionContextReader{ctx: ctx, r: io.LimitReader(src, before.Size+1)})
		closeErr := dst.Close()
		after, statErr := retentionFileIdentity(src, s.owner, false)
		src.Close()
		if e != nil {
			return nil, e
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if statErr != nil {
			return nil, statErr
		}
		if n != before.Size || !sameRetentionIdentity(before, after) {
			return nil, errors.New("installer database snapshot changed while copying")
		}
	}
	uri := url.URL{Scheme: "file", Path: filepath.Join(temporary, "fluxo.db")}
	query := uri.Query()
	query.Set("mode", "ro")
	query.Add("_pragma", "query_only(1)")
	uri.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('php_update_generations','php_update_operations')`).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("read installer recovery dependencies: %w", err)
	}
	if count == 0 {
		return nil, nil
	}
	if count != 2 {
		return nil, errors.New("incomplete installer PHP recovery schema")
	}
	items, err := readArchiveGenerations(ctx, db, false)
	if err != nil {
		return nil, err
	}
	// Pending repository bindings can name a historical generation that was
	// current when setup started. Keep that evidence too, even in old snapshots.
	var exists int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='php_repository_migrations'`).Scan(&exists); err != nil {
		return nil, err
	}
	if exists > 0 {
		rows, e := db.QueryContext(ctx, "SELECT bindings FROM php_repository_migrations WHERE state='pending'")
		if e != nil {
			return nil, e
		}
		known := map[string]int{}
		for i, item := range items {
			known[item.Value.ID] = i
		}
		for rows.Next() {
			var raw string
			if e = rows.Scan(&raw); e != nil {
				break
			}
			var bindings []repositoryRecoveryBinding
			if e = json.Unmarshal([]byte(raw), &bindings); e != nil {
				break
			}
			for _, binding := range bindings {
				i, ok := known[binding.GenerationID]
				if !ok || digest(items[i].Value.Archives) != binding.ManifestHash {
					e = errors.New("invalid installer repository recovery binding")
					break
				}
				items[i].Settled = false
			}
			if e != nil {
				break
			}
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return nil, e
		}
	}
	return items, nil
}
