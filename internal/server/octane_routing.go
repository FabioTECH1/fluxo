package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/syscmd"
)

// Reload only the current configuration: unlike nginx.Reload, this does not
// provision a default server or write a previously absent site configuration.
var reloadExistingOctaneRouting = func(ctx context.Context) error {
	if _, err := syscmd.Run(ctx, 10*time.Second, "/usr/sbin/nginx", "-t"); err != nil {
		return err
	}
	_, err := syscmd.Run(ctx, 10*time.Second, "/usr/bin/systemctl", "reload", "nginx")
	return err
}

var syncOctaneRoutingConfig = syncProtectedOctaneRoutingConfig

var readOctaneRoutingConfig = readProtectedOctaneRoutingConfig
var renderOctaneEnableVhost = func(id int) (renderedSiteVhost, error) { return renderManagedNginxForSiteWithOctane(id, true) }

// Read only the expected root-controlled generated vhost; never overwrite an
// administrator's alternate file or a changed enabled-site link during repair.
func readProtectedOctaneRoutingConfig(name string) (string, error) {
	if name == "." || name == "" || filepath.Base(name) != name {
		return "", errors.New("Invalid Octane vhost name")
	}
	path := filepath.Join("/etc/nginx/sites-available", name)
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || info.Mode().Perm()&0022 != 0 || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("Octane vhost ownership or path changed")
		}
		if current == path && !info.Mode().IsRegular() {
			return "", errors.New("Octane vhost is not a regular file")
		}
		if current == "/" {
			break
		}
	}
	enabled := filepath.Join("/etc/nginx/sites-enabled", name)
	if _, err := os.Lstat(enabled); err == nil {
		target, err := filepath.EvalSymlinks(enabled)
		if err != nil || target != path {
			return "", errors.New("Octane enabled vhost target changed")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	data, err := os.ReadFile(path)
	return string(data), err
}

// Called before any backend shutdown. Publish intent remains durable until a
// matching generated proxy has been replaced and Nginx reload has succeeded.
func restorePendingOctaneRouting(siteID, daemonID int, phase string) error {
	siteNginxMutationMu.Lock()
	defer siteNginxMutationMu.Unlock()
	override, err := database.GetSiteVhostOverride(siteID)
	if err != nil {
		return err
	}
	if override != nil {
		return errors.New("Custom Nginx configuration requires operator review before cleaning up pending Octane routing")
	}
	proxy, err := renderOctaneEnableVhost(siteID)
	if err != nil {
		return err
	}
	fallback, err := renderRegeneratedSiteVhost(siteID)
	if err != nil {
		return err
	}
	if proxy.ConfigName != fallback.ConfigName || proxy.Content == fallback.Content {
		return errors.New("Unable to distinguish pending Octane routing from FPM fallback")
	}
	actual, err := readOctaneRoutingConfig(proxy.ConfigName)
	if errors.Is(err, os.ErrNotExist) && phase == "enable_publish_pending" {
		// Absence on disk is not evidence that live workers stopped proxying.
		// Preserve the absent file, but validate/reload before backend removal.
		if err := removeOctaneReloadFromDeployScript(siteID); err != nil {
			return err
		}
		if err := reloadExistingOctaneRouting(context.Background()); err != nil {
			return fmt.Errorf("Confirm absent Octane routing before cleanup: %w", err)
		}
		if _, err := readOctaneRoutingConfig(proxy.ConfigName); !errors.Is(err, os.ErrNotExist) {
			return errors.New("Absent Octane routing changed during reload")
		}
		return syncOctaneRoutingConfig(proxy.ConfigName, "", true)
	}
	if err != nil {
		return err
	}
	if actual != proxy.Content && actual != fallback.Content {
		return errors.New("Nginx configuration changed since Octane enablement; backend retained for operator review")
	}
	if err := removeOctaneReloadFromDeployScript(siteID); err != nil {
		return err
	}
	// Matching bytes on disk do not prove the running Nginx workers adopted
	// them. Persist restoration intent and validate/reload before any drain.
	if phase != "enable_fallback_pending" {
		if _, err := database.DB.Exec("UPDATE daemons SET status='enable_fallback_pending' WHERE id=? AND site_id=? AND status='enable_publish_pending'", daemonID, siteID); err != nil {
			return err
		}
	}
	if err := installRegeneratedSiteVhost(context.Background(), fallback.ConfigName, fallback.Content); err != nil {
		return fmt.Errorf("Restore FPM routing before Octane cleanup: %w", err)
	}
	actual, err = readOctaneRoutingConfig(fallback.ConfigName)
	if err != nil || actual != fallback.Content {
		return fmt.Errorf("FPM fallback configuration did not verify: %v", err)
	}
	return syncOctaneRoutingConfig(fallback.ConfigName, fallback.Content, false)
}

// Persist both file contents and namespace changes before cleanup can advance
// its SQLite phase or remove the backend. Nginx's successful reload alone does
// not make an atomic rename durable across power loss.
func syncProtectedOctaneRoutingConfig(name, expected string, absent bool) error {
	if name == "." || name == "" || filepath.Base(name) != name {
		return errors.New("Invalid Octane vhost name")
	}
	available := "/etc/nginx/sites-available"
	enabled := "/etc/nginx/sites-enabled"
	for _, dir := range []string{available, enabled} {
		for current := dir; ; current = filepath.Dir(current) {
			info, err := os.Lstat(current)
			if err != nil {
				return err
			}
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok || st.Uid != 0 || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
				return errors.New("Unsafe Octane routing directory")
			}
			if current == "/" {
				break
			}
		}
	}
	path := filepath.Join(available, name)
	if absent {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return errors.New("Absent Octane vhost changed before durable confirmation")
		}
	} else {
		file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Size() != int64(len(expected)) {
			return errors.New("Unsafe or changed FPM fallback file")
		}
		data, err := io.ReadAll(file)
		if err != nil {
			return err
		}
		if string(data) != expected {
			return errors.New("FPM fallback changed before durable confirmation")
		}
		if err = file.Sync(); err != nil {
			return fmt.Errorf("Persist FPM fallback file: %w", err)
		}
	}
	link := filepath.Join(enabled, name)
	if info, err := os.Lstat(link); err == nil {
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || info.Mode()&os.ModeSymlink == 0 {
			return errors.New("Unsafe Octane enabled-site link")
		}
		target, err := os.Readlink(link)
		if err != nil {
			return err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(enabled, target)
		}
		if filepath.Clean(target) != path {
			return errors.New("Octane enabled-site link changed before durable confirmation")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, path := range []string{available, enabled} {
		dir, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_DIRECTORY, 0)
		if err != nil {
			return err
		}
		err = dir.Sync()
		closeErr := dir.Close()
		if err != nil {
			return fmt.Errorf("Persist Octane routing directory: %w", err)
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
