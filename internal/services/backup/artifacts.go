package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"fluxo/internal/database"
	"fluxo/internal/syscmd"

	"golang.org/x/sys/unix"
)

const backupFormatVersion = 1

const (
	minimumBackupFreeBytes = 512 << 20
	diskCheckInterval      = 16 << 20
)

var safeDatabaseName = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

type localArtifact struct {
	database.BackupArtifact
	Path string
}

type backupManifest struct {
	FormatVersion int                      `json:"format_version"`
	RunID         string                   `json:"run_id"`
	CreatedAt     time.Time                `json:"created_at"`
	Trigger       string                   `json:"trigger"`
	Site          backupManifestSite       `json:"site"`
	Artifacts     []backupManifestArtifact `json:"artifacts"`
}

type backupManifestSite struct {
	ID                 int    `json:"id"`
	Domain             string `json:"domain"`
	AppType            string `json:"app_type"`
	PHPVersion         string `json:"php_version"`
	Repository         string `json:"repository"`
	Branch             string `json:"branch"`
	WebRoot            string `json:"web_root"`
	DeploymentStrategy string `json:"deployment_strategy"`
}

type backupManifestArtifact struct {
	Kind         string `json:"kind"`
	DatabaseName string `json:"database_name,omitempty"`
	Engine       string `json:"engine,omitempty"`
	ObjectKey    string `json:"object_key"`
	Filename     string `json:"filename"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
}

func buildArtifacts(ctx context.Context, plan database.BackupPlan, site database.Site, runID, workDir string) ([]localArtifact, error) {
	artifacts := make([]localArtifact, 0, 1+len(plan.DatabaseIDs))
	if plan.IncludeFiles {
		filename := "site-files.tar.gz"
		path := filepath.Join(workDir, filename)
		if err := archiveSite(ctx, site, path); err != nil {
			return nil, fmt.Errorf("archive site files: %w", err)
		}
		artifact, err := describeLocalArtifact(ctx, runID, "files", 0, "", "", filename, path)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}

	for _, databaseID := range plan.DatabaseIDs {
		var item database.Database
		if err := database.DB.QueryRow(`SELECT id, site_id, engine, name, username, created_at
			FROM databases WHERE id = ?`, databaseID).Scan(&item.ID, &item.SiteID, &item.Engine, &item.Name, &item.Username, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("load database %d: %w", databaseID, err)
		}
		if item.SiteID != plan.SiteID {
			return nil, fmt.Errorf("database %s is not linked to site %s", item.Name, site.Domain)
		}
		artifact, err := dumpDatabase(ctx, runID, item, workDir)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, nil
}

func archiveSite(ctx context.Context, site database.Site, destination string) error {
	if site.Domain == "" || site.Domain == "." || site.Domain == ".." || filepath.Base(site.Domain) != site.Domain {
		return errors.New("site domain is not a safe managed directory name")
	}
	expectedPath := filepath.Join("/home/fluxo", site.Domain)
	if site.Path == "" || !filepath.IsAbs(site.Path) || filepath.Clean(site.Path) != expectedPath {
		return errors.New("site path is outside the managed site directory")
	}
	rootFD, err := unix.Open(site.Path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open site directory without following symlinks: %w", err)
	}
	root := os.NewFile(uintptr(rootFD), site.Path)
	if root == nil {
		unix.Close(rootFD)
		return errors.New("open site directory")
	}
	defer root.Close()

	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	removeOnError := true
	defer func() {
		file.Close()
		if removeOnError {
			os.Remove(destination)
		}
	}()
	guard := &diskReserveWriter{file: file, directory: filepath.Dir(destination), bytesUntilCheck: 0}
	gzipWriter, err := gzip.NewWriterLevel(guard, gzip.BestSpeed)
	if err != nil {
		return err
	}
	tarWriter := tar.NewWriter(gzipWriter)

	if site.DeploymentStrategy == "zero-downtime" {
		if err := archiveDirectory(ctx, root, tarWriter, "site", "", true); err != nil {
			return err
		}
		currentTarget, err := readlinkAt(rootFD, "current")
		if err != nil {
			return fmt.Errorf("resolve current release: %w", err)
		}
		if filepath.IsAbs(currentTarget) {
			currentTarget, err = filepath.Rel(site.Path, filepath.Clean(currentTarget))
			if err != nil {
				return err
			}
		} else {
			currentTarget = filepath.Clean(currentTarget)
		}
		if currentTarget == "." || currentTarget == ".." || strings.HasPrefix(currentTarget, ".."+string(os.PathSeparator)) {
			return errors.New("current release resolves outside the site directory")
		}
		current, err := openDirectoryBeneath(rootFD, currentTarget)
		if err != nil {
			return fmt.Errorf("open current release securely: %w", err)
		}
		defer current.Close()
		if err := archiveDirectory(ctx, current, tarWriter, "site/current", "", false); err != nil {
			return err
		}
	} else if err := archiveDirectory(ctx, root, tarWriter, "site", "", false); err != nil {
		return err
	}

	if err := tarWriter.Close(); err != nil {
		return err
	}
	if err := gzipWriter.Close(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	removeOnError = false
	return nil
}

func archiveDirectory(ctx context.Context, directory *os.File, writer *tar.Writer, archiveRoot, relativeRoot string, skipReleases bool) error {
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		relative := filepath.Join(relativeRoot, name)
		procPath := fmt.Sprintf("/proc/self/fd/%d/%s", directory.Fd(), name)
		info, err := os.Lstat(procPath)
		if err != nil {
			return err
		}
		if shouldSkipBackupPath(relative, info.IsDir(), skipReleases) {
			continue
		}
		archiveName := filepath.ToSlash(filepath.Join(archiveRoot, relative))
		if strings.HasPrefix(archiveName, "../") || filepath.IsAbs(archiveName) {
			return errors.New("unsafe archive path")
		}

		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(procPath)
			if err != nil {
				return err
			}
			header, err := tar.FileInfoHeader(info, target)
			if err != nil {
				return err
			}
			header.Name = archiveName
			if err := writer.WriteHeader(header); err != nil {
				return err
			}
			continue
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("site entry %s has unsupported file type %s", relative, info.Mode().Type())
		}

		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC
		if info.IsDir() {
			flags |= unix.O_DIRECTORY
		}
		fd, err := unix.Openat(int(directory.Fd()), name, flags, 0)
		if err != nil {
			return fmt.Errorf("securely open site entry %s: %w", relative, err)
		}
		opened := os.NewFile(uintptr(fd), procPath)
		if opened == nil {
			unix.Close(fd)
			return errors.New("open site entry")
		}
		openedInfo, err := opened.Stat()
		if err != nil || openedInfo.Mode().Type() != info.Mode().Type() {
			opened.Close()
			return errors.New("site entry changed while it was being archived")
		}
		header, err := tar.FileInfoHeader(openedInfo, "")
		if err != nil {
			opened.Close()
			return err
		}
		header.Name = archiveName
		if err := writer.WriteHeader(header); err != nil {
			opened.Close()
			return err
		}
		if openedInfo.IsDir() {
			err = archiveDirectory(ctx, opened, writer, archiveRoot, relative, skipReleases)
		} else if openedInfo.Mode().IsRegular() {
			_, err = io.Copy(writer, &contextReader{ctx: ctx, reader: opened})
		}
		closeErr := opened.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func readlinkAt(directoryFD int, name string) (string, error) {
	buffer := make([]byte, 4096)
	count, err := unix.Readlinkat(directoryFD, name, buffer)
	if err != nil {
		return "", err
	}
	if count == len(buffer) {
		return "", errors.New("symlink target is too long")
	}
	return string(buffer[:count]), nil
}

func openDirectoryBeneath(rootFD int, relative string) (*os.File, error) {
	fd, err := unix.Dup(rootFD)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(filepath.Clean(relative), string(os.PathSeparator)) {
		if part == "" || part == "." || part == ".." {
			unix.Close(fd)
			return nil, errors.New("unsafe directory path")
		}
		next, openErr := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if openErr != nil {
			return nil, openErr
		}
		fd = next
	}
	file := os.NewFile(uintptr(fd), relative)
	if file == nil {
		unix.Close(fd)
		return nil, errors.New("open directory")
	}
	return file, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

type diskReserveWriter struct {
	file            *os.File
	directory       string
	bytesUntilCheck int64
}

func (writer *diskReserveWriter) Write(data []byte) (int, error) {
	if writer.bytesUntilCheck <= 0 {
		if err := ensureBackupFreeSpace(writer.directory); err != nil {
			return 0, err
		}
		writer.bytesUntilCheck = diskCheckInterval
	}
	written, err := writer.file.Write(data)
	writer.bytesUntilCheck -= int64(written)
	return written, err
}

func ensureBackupFreeSpace(directory string) error {
	var stats unix.Statfs_t
	if err := unix.Statfs(directory, &stats); err != nil {
		return err
	}
	available := int64(stats.Bavail) * int64(stats.Bsize)
	if available < minimumBackupFreeBytes {
		return errors.New("backup stopped to preserve at least 512 MB of free disk space")
	}
	return nil
}

func shouldSkipBackupPath(relative string, isDir, skipReleases bool) bool {
	path := filepath.ToSlash(relative)
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if part == ".git" || part == "node_modules" {
			return true
		}
	}
	if skipReleases && (path == "releases" || strings.HasPrefix(path, "releases/") || path == "current") {
		return true
	}
	if isDir && (hasPathSequence(path, "storage/logs") ||
		hasPathSequence(path, "storage/framework/cache") ||
		hasPathSequence(path, "storage/framework/sessions") ||
		hasPathSequence(path, "storage/framework/views")) {
		return true
	}
	return false
}

func hasPathSequence(path, sequence string) bool {
	return path == sequence || strings.HasPrefix(path, sequence+"/") ||
		strings.HasSuffix(path, "/"+sequence) || strings.Contains(path, "/"+sequence+"/")
}

func dumpDatabase(ctx context.Context, runID string, item database.Database, workDir string) (localArtifact, error) {
	if !safeDatabaseName.MatchString(item.Name) {
		return localArtifact{}, errors.New("database name contains unsupported characters")
	}
	switch item.Engine {
	case "mysql":
		return dumpMySQL(ctx, runID, item, workDir)
	case "postgres":
		return dumpPostgres(ctx, runID, item, workDir)
	default:
		return localArtifact{}, fmt.Errorf("unsupported database engine %q", item.Engine)
	}
}

func dumpMySQL(ctx context.Context, runID string, item database.Database, workDir string) (localArtifact, error) {
	binary := "mariadb-dump"
	if _, err := exec.LookPath(binary); err != nil {
		binary = "mysqldump"
		if _, err := exec.LookPath(binary); err != nil {
			return localArtifact{}, errors.New("mariadb-dump or mysqldump is required")
		}
	}
	filename := "mysql-" + item.Name + ".sql.gz"
	path := filepath.Join(workDir, filename)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return localArtifact{}, err
	}
	guard := &diskReserveWriter{file: file, directory: workDir}
	gzipWriter, err := gzip.NewWriterLevel(guard, gzip.BestSpeed)
	if err != nil {
		file.Close()
		os.Remove(path)
		return localArtifact{}, err
	}
	err = syscmd.RunToWriter(ctx, 2*time.Hour, gzipWriter, binary,
		"--single-transaction", "--quick", "--routines", "--triggers", "--events", "--hex-blob", "--databases", item.Name)
	closeGzipErr := gzipWriter.Close()
	closeFileErr := file.Close()
	if err != nil || closeGzipErr != nil || closeFileErr != nil {
		os.Remove(path)
		if err != nil {
			return localArtifact{}, fmt.Errorf("dump MySQL database %s: %w", item.Name, err)
		}
		if closeGzipErr != nil {
			return localArtifact{}, closeGzipErr
		}
		return localArtifact{}, closeFileErr
	}
	return describeLocalArtifact(ctx, runID, "database", item.ID, item.Name, item.Engine, filename, path)
}

func dumpPostgres(ctx context.Context, runID string, item database.Database, workDir string) (localArtifact, error) {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		return localArtifact{}, errors.New("pg_dump is required")
	}
	filename := "postgres-" + item.Name + ".dump"
	path := filepath.Join(workDir, filename)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return localArtifact{}, err
	}
	guard := &diskReserveWriter{file: file, directory: workDir}
	err = syscmd.RunToWriter(ctx, 2*time.Hour, guard, "sudo", "-u", "postgres", "pg_dump",
		"--format=custom", "--no-owner", "--no-privileges", "--dbname", item.Name)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		os.Remove(path)
		if err != nil {
			return localArtifact{}, fmt.Errorf("dump PostgreSQL database %s: %w", item.Name, err)
		}
		return localArtifact{}, closeErr
	}
	return describeLocalArtifact(ctx, runID, "database", item.ID, item.Name, item.Engine, filename, path)
}

func describeLocalArtifact(ctx context.Context, runID, kind string, databaseID int, databaseName, engine, filename, path string) (localArtifact, error) {
	info, err := os.Stat(path)
	if err != nil {
		return localArtifact{}, err
	}
	checksum, err := fileSHA256(ctx, path)
	if err != nil {
		return localArtifact{}, err
	}
	return localArtifact{BackupArtifact: database.BackupArtifact{
		RunID: runID, Kind: kind, DatabaseID: databaseID, DatabaseName: databaseName,
		Engine: engine, Filename: filename, SizeBytes: info.Size(), SHA256: checksum,
	}, Path: path}, nil
}

func fileSHA256(ctx context.Context, path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, &contextReader{ctx: ctx, reader: file}); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func encodeManifest(run database.BackupRun, site database.Site, artifacts []localArtifact) ([]byte, string, error) {
	manifestArtifacts := make([]backupManifestArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		manifestArtifacts = append(manifestArtifacts, backupManifestArtifact{
			Kind: artifact.Kind, DatabaseName: artifact.DatabaseName, Engine: artifact.Engine,
			ObjectKey: artifact.ObjectKey, Filename: artifact.Filename,
			SizeBytes: artifact.SizeBytes, SHA256: artifact.SHA256,
		})
	}
	manifest := backupManifest{
		FormatVersion: backupFormatVersion,
		RunID:         run.ID,
		CreatedAt:     time.Now().UTC(),
		Trigger:       run.Trigger,
		Site: backupManifestSite{
			ID: site.ID, Domain: site.Domain, AppType: site.AppType, PHPVersion: site.PHPVersion,
			Repository: site.Repository, Branch: site.Branch, WebRoot: site.WebRoot,
			DeploymentStrategy: site.DeploymentStrategy,
		},
		Artifacts: manifestArtifacts,
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, "", err
	}
	hash := sha256.Sum256(data)
	return data, hex.EncodeToString(hash[:]), nil
}
