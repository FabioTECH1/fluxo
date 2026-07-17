// Package phpmyadmin manages Fluxo's optional, loopback-only phpMyAdmin installation.
package phpmyadmin

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"fluxo/internal/services/nginx"
)

const (
	Version        = "5.2.3"
	archiveSHA256  = "12ba1c425fa4071abbd4e7668c9ebdeac0b0755a467a6d6d5026122bb47c102b"
	archiveURL     = "https://files.phpmyadmin.net/phpMyAdmin/5.2.3/phpMyAdmin-5.2.3-all-languages.tar.gz"
	baseDir        = "/opt/fluxo/tools/phpmyadmin"
	releasesDir    = baseDir + "/releases"
	currentPath    = baseDir + "/current"
	tempPath       = baseDir + "/tmp"
	sessionPath    = baseDir + "/sessions"
	metadataPath   = baseDir + "/metadata.json"
	nginxAvailable = "/etc/nginx/sites-available/fluxo-phpmyadmin"
	nginxEnabled   = "/etc/nginx/sites-enabled/fluxo-phpmyadmin"
	listenAddress  = "127.0.0.1:9091"
)

var (
	operationMu sync.Mutex
	ErrBusy     = errors.New("another phpMyAdmin operation is already running")
)

type metadata struct {
	Version    string `json:"version"`
	PHPVersion string `json:"php_version"`
}

// Status describes the installed tool and its dependencies.
type Status struct {
	Installed      bool   `json:"installed"`
	Enabled        bool   `json:"enabled"`
	Version        string `json:"version"`
	PHPVersion     string `json:"php_version"`
	MySQLAvailable bool   `json:"mysql_available"`
	AccessPath     string `json:"access_path"`
}

// GetStatus reads the installation state from Fluxo-owned files.
func GetStatus() Status {
	status := Status{
		MySQLAvailable: commandExists("/usr/bin/mysql") || commandExists("/usr/bin/mariadb"),
		AccessPath:     "/phpmyadmin/",
	}
	if info, err := os.Stat(currentPath); err == nil && info.IsDir() {
		status.Installed = true
	}
	if info, err := os.Lstat(nginxEnabled); err == nil && info.Mode()&os.ModeSymlink != 0 {
		status.Enabled = true
	}
	if data, err := os.ReadFile(metadataPath); err == nil {
		var meta metadata
		if json.Unmarshal(data, &meta) == nil {
			status.Version = meta.Version
			status.PHPVersion = meta.PHPVersion
		}
	}
	return status
}

// Install downloads a pinned release, verifies it, configures PHP/Nginx, and enables access.
func Install(ctx context.Context, phpVersion string) error {
	if !operationMu.TryLock() {
		return ErrBusy
	}
	defer operationMu.Unlock()

	if GetStatus().Installed {
		return errors.New("phpMyAdmin is already installed")
	}
	if !GetStatus().MySQLAvailable {
		return errors.New("MySQL or MariaDB must be installed first")
	}
	phpSocket := fmt.Sprintf("/run/php/php%s-fpm.sock", phpVersion)
	if _, err := os.Stat(phpSocket); err != nil {
		return fmt.Errorf("PHP %s FPM is not available", phpVersion)
	}
	if _, err := os.Stat("/usr/sbin/nginx"); err != nil {
		return errors.New("Nginx must be installed first")
	}

	if err := os.MkdirAll(releasesDir, 0755); err != nil {
		return fmt.Errorf("create phpMyAdmin directory: %w", err)
	}
	workDir, err := os.MkdirTemp(baseDir, ".install-")
	if err != nil {
		return fmt.Errorf("create installation workspace: %w", err)
	}
	defer os.RemoveAll(workDir)

	archivePath := filepath.Join(workDir, "phpmyadmin.tar.gz")
	if err := download(ctx, archiveURL, archivePath); err != nil {
		return err
	}
	if err := verifyArchive(archivePath); err != nil {
		return err
	}

	stagingDir := filepath.Join(workDir, "release")
	if err := extractArchive(archivePath, stagingDir); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(stagingDir, "setup")); err != nil {
		return fmt.Errorf("remove phpMyAdmin setup utility: %w", err)
	}
	if err := writeConfig(stagingDir); err != nil {
		return err
	}
	if err := os.MkdirAll(tempPath, 0770); err != nil {
		return fmt.Errorf("create phpMyAdmin temp directory: %w", err)
	}
	setWebGroup(tempPath, 0770)
	if err := os.MkdirAll(sessionPath, 0770); err != nil {
		return fmt.Errorf("create phpMyAdmin session directory: %w", err)
	}
	setWebGroup(sessionPath, 0770)

	releaseDir := filepath.Join(releasesDir, Version)
	if err := os.Rename(stagingDir, releaseDir); err != nil {
		return fmt.Errorf("activate phpMyAdmin release: %w", err)
	}
	cleanupRelease := true
	defer func() {
		if cleanupRelease {
			os.RemoveAll(releaseDir)
		}
	}()

	if err := replaceSymlink(releaseDir, currentPath); err != nil {
		return err
	}
	if err := writeMetadata(metadata{Version: Version, PHPVersion: phpVersion}); err != nil {
		os.Remove(currentPath)
		return err
	}
	if err := os.WriteFile(nginxAvailable, []byte(renderNginxConfig(phpSocket)), 0644); err != nil {
		os.Remove(currentPath)
		return fmt.Errorf("write phpMyAdmin Nginx configuration: %w", err)
	}
	if err := replaceSymlink(nginxAvailable, nginxEnabled); err != nil {
		os.Remove(currentPath)
		return err
	}
	if err := nginx.Reload(ctx); err != nil {
		os.Remove(nginxEnabled)
		os.Remove(currentPath)
		return err
	}

	cleanupRelease = false
	return nil
}

// Enable exposes the installed tool through its loopback-only Nginx listener.
func Enable(ctx context.Context) error {
	if !operationMu.TryLock() {
		return ErrBusy
	}
	defer operationMu.Unlock()
	if !GetStatus().Installed {
		return errors.New("phpMyAdmin is not installed")
	}
	if _, err := os.Stat(nginxAvailable); err != nil {
		return errors.New("phpMyAdmin Nginx configuration is missing; reinstall phpMyAdmin")
	}
	if err := replaceSymlink(nginxAvailable, nginxEnabled); err != nil {
		return err
	}
	if err := nginx.Reload(ctx); err != nil {
		os.Remove(nginxEnabled)
		return err
	}
	return nil
}

// Disable removes the active Nginx link while preserving the installation.
func Disable(ctx context.Context) error {
	if !operationMu.TryLock() {
		return ErrBusy
	}
	defer operationMu.Unlock()
	if err := os.Remove(nginxEnabled); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("disable phpMyAdmin: %w", err)
	}
	return nginx.Reload(ctx)
}

// Remove disables phpMyAdmin and deletes only Fluxo-owned tool files.
func Remove(ctx context.Context) error {
	if !operationMu.TryLock() {
		return ErrBusy
	}
	defer operationMu.Unlock()
	if err := os.Remove(nginxEnabled); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("disable phpMyAdmin: %w", err)
	}
	if err := os.Remove(nginxAvailable); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove phpMyAdmin Nginx configuration: %w", err)
	}
	if err := nginx.Reload(ctx); err != nil {
		return err
	}
	if err := os.RemoveAll(baseDir); err != nil {
		return fmt.Errorf("remove phpMyAdmin files: %w", err)
	}
	return nil
}

func download(ctx context.Context, sourceURL, destination string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return fmt.Errorf("create phpMyAdmin download request: %w", err)
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download phpMyAdmin: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download phpMyAdmin: unexpected HTTP status %s", resp.Status)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("create phpMyAdmin archive: %w", err)
	}
	defer file.Close()
	if _, err := io.Copy(file, io.LimitReader(resp.Body, 64<<20)); err != nil {
		return fmt.Errorf("save phpMyAdmin archive: %w", err)
	}
	return nil
}

func verifyArchive(archivePath string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open phpMyAdmin archive: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("hash phpMyAdmin archive: %w", err)
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); actual != archiveSHA256 {
		return errors.New("phpMyAdmin archive checksum verification failed")
	}
	return nil
}

func extractArchive(archivePath, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("read phpMyAdmin archive: %w", err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	prefix := "phpMyAdmin-" + Version + "-all-languages/"
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("extract phpMyAdmin archive: %w", err)
		}
		if !strings.HasPrefix(header.Name, prefix) {
			continue
		}
		relative := strings.TrimPrefix(header.Name, prefix)
		if relative == "" {
			continue
		}
		target := filepath.Join(destination, filepath.FromSlash(relative))
		cleanDestination := filepath.Clean(destination) + string(os.PathSeparator)
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), cleanDestination) {
			return errors.New("phpMyAdmin archive contains an unsafe path")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, reader)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
	}
	return nil
}

func writeConfig(releaseDir string) error {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return fmt.Errorf("generate phpMyAdmin cookie secret: %w", err)
	}
	config := fmt.Sprintf(`<?php
declare(strict_types=1);
$cfg['blowfish_secret'] = sodium_hex2bin('%s');
$i = 0;
$i++;
$cfg['Servers'][$i]['auth_type'] = 'cookie';
$cfg['Servers'][$i]['host'] = '127.0.0.1';
$cfg['Servers'][$i]['AllowNoPassword'] = false;
$cfg['Servers'][$i]['AllowRoot'] = false;
$cfg['AllowArbitraryServer'] = false;
$cfg['LoginCookieValidity'] = 28800;
$cfg['LoginCookieStore'] = 0;
$cfg['SessionSavePath'] = '%s';
$cfg['TempDir'] = '%s';
$cfg['UploadDir'] = '';
$cfg['SaveDir'] = '';
`, hex.EncodeToString(secret), sessionPath, tempPath)
	path := filepath.Join(releaseDir, "config.inc.php")
	if err := os.WriteFile(path, []byte(config), 0640); err != nil {
		return fmt.Errorf("write phpMyAdmin configuration: %w", err)
	}
	setWebGroup(path, 0640)
	return nil
}

func renderNginxConfig(phpSocket string) string {
	return fmt.Sprintf(`server {
    listen %s;
    server_name localhost;
    root %s;
    index index.php;
    client_max_body_size 64m;

    location /phpmyadmin/ {
        alias %s/;
        index index.php;
    }

    location ~ ^/phpmyadmin/(.+\.php)$ {
        alias %s/$1;
        include fastcgi_params;
        fastcgi_param SCRIPT_FILENAME %s/$1;
        fastcgi_param SCRIPT_NAME /phpmyadmin/$1;
        fastcgi_param HTTP_HOST $http_x_forwarded_host;
        fastcgi_param SERVER_NAME $http_x_forwarded_host;
        fastcgi_param HTTPS $http_x_forwarded_https;
        fastcgi_param PHP_VALUE "session.gc_maxlifetime=28800";
        fastcgi_pass unix:%s;
        fastcgi_read_timeout 300;
    }

    location ~ /\. {
        deny all;
    }
}
`, listenAddress, currentPath, currentPath, currentPath, currentPath, phpSocket)
}

func writeMetadata(meta metadata) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("write phpMyAdmin metadata: %w", err)
	}
	return nil
}

func replaceSymlink(target, link string) error {
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		return err
	}
	temporary := link + ".tmp"
	os.Remove(temporary)
	if err := os.Symlink(target, temporary); err != nil {
		return fmt.Errorf("create phpMyAdmin link: %w", err)
	}
	if err := os.Rename(temporary, link); err != nil {
		os.Remove(temporary)
		return fmt.Errorf("activate phpMyAdmin link: %w", err)
	}
	return nil
}

func setWebGroup(path string, mode os.FileMode) {
	wwwData, err := user.Lookup("www-data")
	if err == nil {
		if gid, parseErr := strconv.Atoi(wwwData.Gid); parseErr == nil {
			_ = os.Chown(path, 0, gid)
		}
	}
	_ = os.Chmod(path, mode)
}

func commandExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0111 != 0
}
