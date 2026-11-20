package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fluxo/internal/services/nginx"
	"fluxo/internal/services/traffic"
	"golang.org/x/sys/unix"
)

const (
	trafficRotateBytes    = 4 << 20
	trafficSiteLogBudget  = 16 << 20
	trafficTotalLogBudget = 128 << 20
	trafficMinimumFree    = 128 << 20
)

// Walk with O_NOFOLLOW relative to verified directory descriptors. A sticky
// root-owned temporary directory is allowed for isolated development fixtures.
func trafficControlDirectory(name string, create bool) (*os.File, error) {
	if !filepath.IsAbs(name) || filepath.Clean(name) != name {
		return nil, errors.New("traffic directory must be an absolute clean path")
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(name, "/"), "/") {
		if part == "" {
			continue
		}
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(e, unix.ENOENT) && create {
			e = unix.Mkdirat(fd, part, 0700)
			if e == nil || errors.Is(e, unix.EEXIST) {
				next, e = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			}
		}
		unix.Close(fd)
		if e != nil {
			return nil, e
		}
		fd = next
		var st unix.Stat_t
		if e = unix.Fstat(fd, &st); e != nil {
			unix.Close(fd)
			return nil, e
		}
		stickyRoot := st.Uid == 0 && st.Mode&unix.S_ISVTX != 0
		if (st.Uid != 0 && int(st.Uid) != os.Geteuid()) || st.Mode&0022 != 0 && !stickyRoot {
			unix.Close(fd)
			return nil, errors.New("traffic directory ancestry is writable or has an untrusted owner")
		}
	}
	return os.NewFile(uintptr(fd), name), nil
}

func (m *trafficControl) removeRetiredTrafficLog(src traffic.Source) error {
	if src.Path != m.sourcePath(src.SiteID, src.Generation) || !validTrafficGeneration(src.Generation) {
		return errors.New("invalid retired analytics log")
	}
	dir, err := trafficControlDirectory(filepath.Dir(src.Path), false)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer dir.Close()
	for _, name := range []string{filepath.Base(src.Path), filepath.Base(src.Path) + ".1"} {
		var st unix.Stat_t
		err := unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil {
			return err
		}
		if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1 || st.Mode&0022 != 0 {
			return errors.New("retired analytics log is not a protected regular file")
		}
		if err := unix.Unlinkat(int(dir.Fd()), name, 0); err != nil {
			return err
		}
	}
	return dir.Sync()
}

// Supervision performs metadata checks only. It never parses a request or
// touches an operational access log. Limits are soft: an in-flight worker can
// write until graceful retirement, so pressure pauses remain visibly pending.
func (m *trafficControl) superviseLocked(ctx context.Context) error {
	sources := trafficSources(m.state.trafficSelection)
	if len(sources) == 0 {
		return nil
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(filepath.Dir(m.path), &fs); err != nil {
		return err
	}
	if fs.Bavail*uint64(fs.Bsize) < trafficMinimumFree {
		return m.pauseTrafficLocked(ctx, 0, "Collection paused because available disk space is low. Free space, then enable collection again.")
	}
	for _, src := range sources {
		var total int64
		var currentSize int64
		for i, name := range []string{src.Path, src.Path + ".1"} {
			st, err := os.Lstat(name)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return err
			}
			if !st.Mode().IsRegular() || st.Mode().Perm()&0022 != 0 {
				return m.pauseTrafficLocked(ctx, src.SiteID, "Collection paused because an analytics log is unsafe. Review its permissions before enabling collection again.")
			}
			total += st.Size()
			if i == 0 {
				currentSize = st.Size()
			}
		}
		if total >= trafficSiteLogBudget {
			return m.pauseTrafficLocked(ctx, src.SiteID, "Collection paused because a site's analytics log exceeded its processing budget. Traffic history may be incomplete. Enable collection again after reviewing the load.")
		}
		stats := m.collector.Stats(src.SiteID)
		if trafficStorageExhausted(stats.Error) {
			return m.pauseTrafficLocked(ctx, 0, "Collection paused because analytics storage reached its budget. Delete analytics history or free disk space before enabling collection again.")
		}
		if currentSize >= trafficRotateBytes && m.collector.RotationReady(src) {
			retired, err := m.retired(ctx, src.Path+".1")
			if os.IsNotExist(err) {
				retired, err = true, nil
			}
			if err != nil {
				return err
			}
			if !retired {
				continue
			}
			release, err := m.admit(ctx)
			if err != nil {
				return err
			}
			rotationErr := m.collector.RotateSource(ctx, src, func(work context.Context) error {
				dir, err := trafficControlDirectory(filepath.Dir(src.Path), false)
				if err != nil {
					return err
				}
				defer dir.Close()
				base := filepath.Base(src.Path)
				if err = unix.Renameat(int(dir.Fd()), base, int(dir.Fd()), base+".1"); err != nil {
					return err
				}
				fd, err := unix.Openat(int(dir.Fd()), base, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
				if err != nil {
					return err
				}
				unix.Close(fd)
				if err = dir.Sync(); err != nil {
					return err
				}
				// New root-created workers inherit the reopened descriptor. USR1
				// alone cannot reopen through private data-directory ancestors as
				// an unprivileged worker. The collector retains .1 until every old
				// Nginx writer is independently observed to have closed it.
				sites, err := m.listSites(work)
				if err != nil {
					return err
				}
				index := make(map[int64]trafficSiteInfo, len(sites))
				for _, site := range sites {
					index[site.ID] = site
				}
				var producers []nginx.TrafficSite
				for _, source := range sources {
					site, ok := index[source.SiteID]
					if !ok {
						return errTrafficSiteMissing
					}
					producers = append(producers, nginx.TrafficSite{ID: source.SiteID, ConfigName: site.ConfigName, Domain: site.Domain, Generation: source.Generation, LogPath: source.Path})
				}
				_, err = m.apply(work, producers)
				return err
			})
			release()
			if rotationErr != nil {
				return rotationErr
			}
		}
	}
	if m.lastSweep.IsZero() || m.now().Sub(m.lastSweep) >= 10*time.Second {
		m.lastSweep = m.now()
		var total int64
		count := 0
		err := filepath.WalkDir(filepath.Join(m.dataDir, "traffic", "logs"), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			count++
			if count > 4096 {
				return errors.New("analytics log inventory exceeded its bounded capacity")
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return errors.New("analytics log inventory contains an unsafe link")
			}
			if !entry.IsDir() {
				st, err := entry.Info()
				if err != nil {
					return err
				}
				if !st.Mode().IsRegular() {
					return errors.New("analytics inventory contains a nonregular file")
				}
				total += st.Size()
			}
			return nil
		})
		if err != nil {
			return m.pauseTrafficLocked(ctx, 0, "Collection paused because its log inventory could not be safely bounded. Review analytics storage before enabling collection again.")
		}
		if total >= trafficTotalLogBudget {
			return m.pauseTrafficLocked(ctx, 0, "Collection paused because analytics logs reached the server storage budget. Enable collection again after reviewing storage and load.")
		}
	}
	return nil
}

// A busy WAL checkpoint is temporary backpressure: collection must retry when
// readers release their snapshots, without requiring the user to enable it again.
func trafficStorageExhausted(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "database or disk is full") ||
		strings.Contains(message, "traffic collection paused: analytics storage capacity reached")
}

func (m *trafficControl) pauseTrafficLocked(ctx context.Context, id int64, reason string) error {
	target := cloneTrafficSelection(m.state.trafficSelection)
	if id == 0 {
		target.MasterEnabled = false
	}
	for site, p := range target.Sites {
		if site == id {
			p.Enabled = false
		}
		if !target.MasterEnabled || site == id {
			p.Source = nil
		}
		target.Sites[site] = p
	}
	if err := m.beginTargetLocked(target, reason); err != nil {
		return err
	}
	sites, err := m.listSites(ctx)
	if err != nil {
		return err
	}
	if err = m.applyLocked(ctx, sites); err != nil {
		return fmt.Errorf("pause traffic collection: %w", err)
	}
	return nil
}
