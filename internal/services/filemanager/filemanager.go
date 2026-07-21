// Package filemanager provides site-scoped file operations. All path resolution
// is performed relative to an open site-root descriptor so a request can never
// escape through .. components, symlinks, or concurrent path replacement.
package filemanager

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const (
	MaxTextBytes        = int64(1 << 20)
	MaxUploadBytes      = int64(100 << 20)
	DefaultPageSize     = 250
	MaxPageSize         = 500
	MaxDirectoryEntries = 10_000
)

var (
	ErrInvalidPath       = errors.New("invalid file path")
	ErrNotFound          = errors.New("file or directory not found")
	ErrConflict          = errors.New("file changed or destination already exists")
	ErrTooLarge          = errors.New("file is too large")
	ErrNotText           = errors.New("file is not valid UTF-8 text")
	ErrTooManyEntries    = errors.New("directory contains too many entries")
	ErrUnsafePath        = errors.New("path is an unsafe symlink")
	ErrDirectoryNotEmpty = errors.New("directory is not empty")
	ErrNotRegular        = errors.New("path is not a regular file")
	ErrNotDirectory      = errors.New("path is not a directory")
)

type Manager struct {
	root string
	uid  int
	gid  int
}

type fileAttributes struct {
	uid  int
	gid  int
	mode uint32
}

type Entry struct {
	Name          string    `json:"name"`
	Path          string    `json:"path"`
	Kind          string    `json:"kind"`
	Size          int64     `json:"size"`
	Permissions   string    `json:"permissions"`
	Modified      time.Time `json:"modified"`
	IsDirectory   bool      `json:"is_directory"`
	IsFile        bool      `json:"is_file"`
	IsSymlink     bool      `json:"is_symlink"`
	UnsafeSymlink bool      `json:"unsafe_symlink"`
	Editable      bool      `json:"editable"`
}

type Listing struct {
	Path    string  `json:"path"`
	Parent  string  `json:"parent"`
	Entries []Entry `json:"entries"`
	Total   int     `json:"total"`
	Offset  int     `json:"offset"`
	Limit   int     `json:"limit"`
}

type TextFile struct {
	Path     string    `json:"path"`
	Content  string    `json:"content"`
	SHA256   string    `json:"sha256"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
}

func New(root string) (*Manager, error) {
	account, err := user.Lookup("fluxo")
	if err != nil {
		return nil, fmt.Errorf("look up fluxo user: %w", err)
	}
	group, err := user.LookupGroup("www-data")
	if err != nil {
		return nil, fmt.Errorf("look up www-data group: %w", err)
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return nil, fmt.Errorf("parse fluxo uid: %w", err)
	}
	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return nil, fmt.Errorf("parse www-data gid: %w", err)
	}
	return newManager(root, uid, gid)
}

func newManager(root string, uid, gid int) (*Manager, error) {
	if root == "" || !pathIsAbsoluteClean(root) {
		return nil, ErrInvalidPath
	}
	m := &Manager{root: root, uid: uid, gid: gid}
	fd, err := m.openRoot()
	if err != nil {
		return nil, err
	}
	unix.Close(fd)
	return m, nil
}

func pathIsAbsoluteClean(value string) bool {
	return strings.HasPrefix(value, "/") && path.Clean(value) == value && !strings.ContainsRune(value, '\x00')
}

// NormalizePath validates a client-supplied site-relative path and returns its
// canonical slash-separated representation. The site root is represented by ".".
func NormalizePath(value string) (string, error) {
	if value == "" || value == "." || value == "/" {
		return ".", nil
	}
	if len(value) > 4096 || strings.HasPrefix(value, "/") || strings.Contains(value, `\`) || strings.ContainsRune(value, '\x00') {
		return "", ErrInvalidPath
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return "", ErrInvalidPath
		}
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrInvalidPath
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == "" || part == "." || part == ".." || len(part) > 255 {
			return "", ErrInvalidPath
		}
	}
	return cleaned, nil
}

func ValidateName(value string) error {
	if value == "" || value == "." || value == ".." || len(value) > 255 || strings.ContainsAny(value, "/\\") || strings.ContainsRune(value, '\x00') {
		return ErrInvalidPath
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return ErrInvalidPath
		}
	}
	return nil
}

func (m *Manager) openRoot() (int, error) {
	fd, err := unix.Open(m.root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, classify(err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		unix.Close(fd)
		return -1, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		unix.Close(fd)
		return -1, ErrNotDirectory
	}
	return fd, nil
}

func (m *Manager) openBeneath(rootFD int, relative string, flags int, mode uint32) (int, error) {
	fd, err := unix.Openat2(rootFD, relative, &unix.OpenHow{
		Flags:   uint64(flags | unix.O_CLOEXEC),
		Mode:    uint64(mode),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err == nil {
		return fd, nil
	}
	if !errors.Is(err, syscall.EXDEV) && !errors.Is(err, syscall.ELOOP) {
		return -1, classify(err)
	}

	// Zero-downtime deployments use absolute symlinks such as
	// /home/fluxo/example.com/current -> /home/fluxo/example.com/releases/....
	// Resolve those for compatibility, reject targets outside this site, then
	// reopen the resolved target beneath the already-open root descriptor. The
	// final openat2 call remains the security boundary if paths change mid-call.
	resolved, resolveErr := filepath.EvalSymlinks(filepath.Join(m.root, filepath.FromSlash(relative)))
	if resolveErr != nil {
		return -1, classify(resolveErr)
	}
	resolvedRelative, resolveErr := filepath.Rel(m.root, resolved)
	if resolveErr != nil || resolvedRelative == ".." || strings.HasPrefix(resolvedRelative, ".."+string(filepath.Separator)) || filepath.IsAbs(resolvedRelative) {
		return -1, ErrUnsafePath
	}
	fd, err = unix.Openat2(rootFD, filepath.ToSlash(resolvedRelative), &unix.OpenHow{
		Flags:   uint64(flags | unix.O_CLOEXEC),
		Mode:    uint64(mode),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return -1, classify(err)
	}
	return fd, nil
}

func (m *Manager) openParent(rootFD int, relative string) (int, string, error) {
	dir, base := path.Split(relative)
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" {
		dir = "."
	}
	fd, err := m.openBeneath(rootFD, dir, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return -1, "", err
	}
	return fd, base, nil
}

func (m *Manager) List(relative string, includeHidden bool, offset, limit int) (Listing, error) {
	relative, err := NormalizePath(relative)
	if err != nil {
		return Listing{}, err
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = DefaultPageSize
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}

	rootFD, err := m.openRoot()
	if err != nil {
		return Listing{}, err
	}
	defer unix.Close(rootFD)
	dirFD, err := m.openBeneath(rootFD, relative, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return Listing{}, err
	}
	dir := os.NewFile(uintptr(dirFD), relative)
	if dir == nil {
		unix.Close(dirFD)
		return Listing{}, errors.New("open directory stream")
	}
	defer dir.Close()

	entries := make([]Entry, 0)
	scanned := 0
	for {
		batch, readErr := dir.ReadDir(256)
		for _, item := range batch {
			scanned++
			if scanned > MaxDirectoryEntries {
				return Listing{}, ErrTooManyEntries
			}
			if !includeHidden && strings.HasPrefix(item.Name(), ".") {
				continue
			}
			entry, entryErr := m.entryFromDir(rootFD, int(dir.Fd()), relative, item.Name())
			if entryErr != nil {
				if errors.Is(entryErr, ErrNotFound) {
					continue // The entry disappeared while the directory was being read.
				}
				return Listing{}, entryErr
			}
			entries = append(entries, entry)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return Listing{}, readErr
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDirectory != entries[j].IsDirectory {
			return entries[i].IsDirectory
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	total := len(entries)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	parent := "."
	if relative != "." {
		parent = path.Dir(relative)
	}
	return Listing{Path: relative, Parent: parent, Entries: entries[offset:end], Total: total, Offset: offset, Limit: limit}, nil
}

func (m *Manager) entryFromDir(rootFD, directoryFD int, directory, name string) (Entry, error) {
	full := name
	if directory != "." {
		full = path.Join(directory, name)
	}
	var stat unix.Stat_t
	if err := unix.Fstatat(directoryFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return Entry{}, classify(err)
	}
	isSymlink := stat.Mode&unix.S_IFMT == unix.S_IFLNK
	targetStat := stat
	unsafe := false
	if isSymlink {
		fd, err := m.openBeneath(rootFD, full, unix.O_PATH, 0)
		if err != nil {
			unsafe = true
		} else {
			if err := unix.Fstat(fd, &targetStat); err != nil {
				unix.Close(fd)
				return Entry{}, err
			}
			unix.Close(fd)
		}
	}
	modeType := targetStat.Mode & unix.S_IFMT
	isDir := !unsafe && modeType == unix.S_IFDIR
	isFile := !unsafe && modeType == unix.S_IFREG
	kind := "other"
	if isSymlink {
		kind = "symlink"
	} else if isDir {
		kind = "directory"
	} else if isFile {
		kind = "file"
	}
	editable := isFile && !isSymlink && targetStat.Size <= MaxTextBytes
	if editable {
		switch strings.ToLower(path.Ext(name)) {
		case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".ico", ".pdf", ".zip", ".tar", ".gz", ".mp4", ".mp3", ".webm", ".ttf", ".woff", ".woff2", ".eot", ".exe", ".so", ".dll", ".bin", ".dmg":
			editable = false
		}
	}

	return Entry{
		Name:          name,
		Path:          full,
		Kind:          kind,
		Size:          targetStat.Size,
		Permissions:   fmt.Sprintf("%04o", stat.Mode&0777),
		Modified:      time.Unix(stat.Mtim.Sec, stat.Mtim.Nsec),
		IsDirectory:   isDir,
		IsFile:        isFile,
		IsSymlink:     isSymlink,
		UnsafeSymlink: unsafe,
		Editable:      editable,
	}, nil
}

func (m *Manager) ReadText(relative string) (TextFile, error) {
	relative, err := NormalizePath(relative)
	if err != nil || relative == "." {
		return TextFile{}, ErrInvalidPath
	}
	rootFD, err := m.openRoot()
	if err != nil {
		return TextFile{}, err
	}
	defer unix.Close(rootFD)
	fd, err := m.openBeneath(rootFD, relative, unix.O_RDONLY, 0)
	if err != nil {
		return TextFile{}, err
	}
	file := os.NewFile(uintptr(fd), relative)
	if file == nil {
		unix.Close(fd)
		return TextFile{}, errors.New("open file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return TextFile{}, err
	}
	if !info.Mode().IsRegular() {
		return TextFile{}, ErrNotRegular
	}
	if info.Size() > MaxTextBytes {
		return TextFile{}, ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxTextBytes+1))
	if err != nil {
		return TextFile{}, err
	}
	if int64(len(data)) > MaxTextBytes {
		return TextFile{}, ErrTooLarge
	}
	if !utf8.Valid(data) {
		return TextFile{}, ErrNotText
	}
	hash := sha256.Sum256(data)
	return TextFile{Path: relative, Content: string(data), SHA256: hex.EncodeToString(hash[:]), Size: int64(len(data)), Modified: info.ModTime()}, nil
}

func (m *Manager) OpenDownload(relative string) (*os.File, os.FileInfo, error) {
	relative, err := NormalizePath(relative)
	if err != nil || relative == "." {
		return nil, nil, ErrInvalidPath
	}
	rootFD, err := m.openRoot()
	if err != nil {
		return nil, nil, err
	}
	defer unix.Close(rootFD)
	fd, err := m.openBeneath(rootFD, relative, unix.O_RDONLY, 0)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), relative)
	if file == nil {
		unix.Close(fd)
		return nil, nil, errors.New("open file")
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, nil, ErrNotRegular
	}
	return file, info, nil
}

func (m *Manager) Create(relative, entryType string) error {
	relative, err := NormalizePath(relative)
	if err != nil || relative == "." {
		return ErrInvalidPath
	}
	unlock := lockSiteMutations(m.root)
	defer unlock()
	rootFD, err := m.openRoot()
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	parentFD, base, err := m.openParent(rootFD, relative)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)

	switch entryType {
	case "directory":
		if err := unix.Mkdirat(parentFD, base, 0755); err != nil {
			return classify(err)
		}
		directoryFD, err := unix.Openat(parentFD, base, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			unix.Unlinkat(parentFD, base, unix.AT_REMOVEDIR)
			return classify(err)
		}
		if err := unix.Fchown(directoryFD, m.uid, m.gid); err != nil {
			unix.Close(directoryFD)
			unix.Unlinkat(parentFD, base, unix.AT_REMOVEDIR)
			return err
		}
		if err := unix.Fchmod(directoryFD, 0755); err != nil {
			unix.Close(directoryFD)
			unix.Unlinkat(parentFD, base, unix.AT_REMOVEDIR)
			return err
		}
		if err := unix.Fsync(directoryFD); err != nil {
			unix.Close(directoryFD)
			unix.Unlinkat(parentFD, base, unix.AT_REMOVEDIR)
			return err
		}
		if err := unix.Close(directoryFD); err != nil {
			unix.Unlinkat(parentFD, base, unix.AT_REMOVEDIR)
			return err
		}
	case "file":
		fd, err := unix.Openat(parentFD, base, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(fileMode(base)))
		if err != nil {
			return classify(err)
		}
		if err := setOwnershipAndSync(fd, m.uid, m.gid, fileMode(base)); err != nil {
			unix.Close(fd)
			unix.Unlinkat(parentFD, base, 0)
			return err
		}
		if err := unix.Close(fd); err != nil {
			unix.Unlinkat(parentFD, base, 0)
			return err
		}
	default:
		return ErrInvalidPath
	}
	return syncDirectory(parentFD)
}

func (m *Manager) WriteText(relative, content, expectedSHA256 string) error {
	if int64(len(content)) > MaxTextBytes {
		return ErrTooLarge
	}
	if !utf8.ValidString(content) {
		return ErrNotText
	}
	relative, err := NormalizePath(relative)
	if err != nil || relative == "." || len(expectedSHA256) != sha256.Size*2 {
		return ErrInvalidPath
	}
	if _, err := hex.DecodeString(expectedSHA256); err != nil {
		return ErrInvalidPath
	}
	unlock := lockSiteMutations(m.root)
	defer unlock()
	rootFD, err := m.openRoot()
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	parentFD, base, err := m.openParent(rootFD, relative)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)

	currentFD, err := unix.Openat(parentFD, base, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return ErrUnsafePath
		}
		return classify(err)
	}
	current := os.NewFile(uintptr(currentFD), base)
	if current == nil {
		unix.Close(currentFD)
		return errors.New("open current file")
	}
	info, err := current.Stat()
	if err != nil {
		current.Close()
		return err
	}
	if !info.Mode().IsRegular() {
		current.Close()
		return ErrNotRegular
	}
	if info.Size() > MaxTextBytes {
		current.Close()
		return ErrTooLarge
	}
	var currentStat unix.Stat_t
	if err := unix.Fstat(currentFD, &currentStat); err != nil {
		current.Close()
		return err
	}
	hasher := sha256.New()
	_, hashErr := io.Copy(hasher, io.LimitReader(current, MaxTextBytes+1))
	closeErr := current.Close()
	if hashErr != nil {
		return hashErr
	}
	if closeErr != nil {
		return closeErr
	}
	if !strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), expectedSHA256) {
		return ErrConflict
	}
	attributes := &fileAttributes{uid: int(currentStat.Uid), gid: int(currentStat.Gid), mode: currentStat.Mode & 0777}
	return m.atomicWrite(parentFD, base, strings.NewReader(content), MaxTextBytes, true, attributes, expectedSHA256)
}

func (m *Manager) Upload(directory, name string, source io.Reader, overwrite bool) error {
	directory, err := NormalizePath(directory)
	if err != nil || ValidateName(name) != nil {
		return ErrInvalidPath
	}
	unlock := lockSiteMutations(m.root)
	defer unlock()
	rootFD, err := m.openRoot()
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	dirFD, err := m.openBeneath(rootFD, directory, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return err
	}
	defer unix.Close(dirFD)
	return m.atomicWrite(dirFD, name, source, MaxUploadBytes, overwrite, nil, "")
}

func (m *Manager) atomicWrite(parentFD int, base string, source io.Reader, maxBytes int64, overwrite bool, attributes *fileAttributes, expectedSHA256 string) (resultErr error) {
	if overwrite {
		var stat unix.Stat_t
		err := unix.Fstatat(parentFD, base, &stat, unix.AT_SYMLINK_NOFOLLOW)
		if err == nil && stat.Mode&unix.S_IFMT != unix.S_IFREG {
			if stat.Mode&unix.S_IFMT == unix.S_IFLNK {
				return ErrUnsafePath
			}
			return ErrNotRegular
		}
		if err != nil && !errors.Is(err, syscall.ENOENT) {
			return classify(err)
		}
		if err == nil && attributes == nil {
			attributes = &fileAttributes{uid: int(stat.Uid), gid: int(stat.Gid), mode: stat.Mode & 0777}
		}
	}
	tempName, err := randomTempName()
	if err != nil {
		return err
	}
	fd, err := unix.Openat(parentFD, tempName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return classify(err)
	}
	defer func() {
		unix.Close(fd)
		unix.Unlinkat(parentFD, tempName, 0)
	}()
	file := os.NewFile(uintptr(fd), tempName)
	if file == nil {
		return errors.New("open upload temporary file")
	}
	written, err := io.Copy(file, io.LimitReader(source, maxBytes+1))
	if err != nil {
		return err
	}
	if written > maxBytes {
		return ErrTooLarge
	}
	if attributes == nil {
		attributes = &fileAttributes{uid: m.uid, gid: m.gid, mode: fileMode(base)}
	}
	if err := setOwnershipAndSync(fd, attributes.uid, attributes.gid, attributes.mode); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	fd = -1
	if expectedSHA256 != "" {
		matches, err := fileHashMatches(parentFD, base, expectedSHA256)
		if err != nil {
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrNotRegular) || errors.Is(err, ErrTooLarge) {
				return ErrConflict
			}
			return err
		}
		if !matches {
			return ErrConflict
		}
	}
	if overwrite {
		err = unix.Renameat(parentFD, tempName, parentFD, base)
	} else {
		err = unix.Renameat2(parentFD, tempName, parentFD, base, unix.RENAME_NOREPLACE)
	}
	if err != nil {
		return classify(err)
	}
	return syncDirectory(parentFD)
}

func (m *Manager) Move(source, destination string) error {
	var err error
	source, err = NormalizePath(source)
	if err != nil || source == "." {
		return ErrInvalidPath
	}
	destination, err = NormalizePath(destination)
	if err != nil || destination == "." || source == destination || strings.HasPrefix(destination+"/", source+"/") {
		return ErrInvalidPath
	}
	unlock := lockSiteMutations(m.root)
	defer unlock()
	rootFD, err := m.openRoot()
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	sourceParent, sourceBase, err := m.openParent(rootFD, source)
	if err != nil {
		return err
	}
	defer unix.Close(sourceParent)
	destParent, destBase, err := m.openParent(rootFD, destination)
	if err != nil {
		return err
	}
	defer unix.Close(destParent)
	if err := unix.Renameat2(sourceParent, sourceBase, destParent, destBase, unix.RENAME_NOREPLACE); err != nil {
		return classify(err)
	}
	if err := syncDirectory(sourceParent); err != nil {
		return err
	}
	if path.Dir(source) != path.Dir(destination) {
		return syncDirectory(destParent)
	}
	return nil
}

func (m *Manager) Delete(relative string) error {
	relative, err := NormalizePath(relative)
	if err != nil || relative == "." {
		return ErrInvalidPath
	}
	unlock := lockSiteMutations(m.root)
	defer unlock()
	rootFD, err := m.openRoot()
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	parentFD, base, err := m.openParent(rootFD, relative)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	var stat unix.Stat_t
	if err := unix.Fstatat(parentFD, base, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return classify(err)
	}
	flags := 0
	if stat.Mode&unix.S_IFMT == unix.S_IFDIR {
		flags = unix.AT_REMOVEDIR
	}
	if err := unix.Unlinkat(parentFD, base, flags); err != nil {
		if errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, syscall.EEXIST) {
			return ErrDirectoryNotEmpty
		}
		return classify(err)
	}
	return syncDirectory(parentFD)
}

func fileMode(name string) uint32 {
	if name == ".env" || strings.HasPrefix(name, ".env.") {
		return 0640
	}
	return 0644
}

func setOwnershipAndSync(fd, uid, gid int, mode uint32) error {
	if err := unix.Fchown(fd, uid, gid); err != nil {
		return err
	}
	if err := unix.Fchmod(fd, mode); err != nil {
		return err
	}
	return unix.Fsync(fd)
}

func fileHashMatches(parentFD int, base, expectedSHA256 string) (bool, error) {
	fd, err := unix.Openat(parentFD, base, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return false, ErrUnsafePath
		}
		return false, classify(err)
	}
	file := os.NewFile(uintptr(fd), base)
	if file == nil {
		unix.Close(fd)
		return false, errors.New("open file for conflict check")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, ErrNotRegular
	}
	if info.Size() > MaxTextBytes {
		return false, ErrTooLarge
	}
	hasher := sha256.New()
	written, err := io.Copy(hasher, io.LimitReader(file, MaxTextBytes+1))
	if err != nil {
		return false, err
	}
	if written > MaxTextBytes {
		return false, ErrTooLarge
	}
	return strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), expectedSHA256), nil
}

func syncDirectory(fd int) error {
	err := unix.Fsync(fd)
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.EROFS) || errors.Is(err, syscall.EBADF) {
		return nil
	}
	return err
}

func randomTempName() (string, error) {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return ".fluxo-upload-" + hex.EncodeToString(bytes[:]), nil
}

func classify(err error) error {
	switch {
	case errors.Is(err, syscall.ENOENT):
		return ErrNotFound
	case errors.Is(err, syscall.EEXIST):
		return ErrConflict
	case errors.Is(err, syscall.ENOTDIR):
		return ErrNotDirectory
	case errors.Is(err, syscall.EISDIR):
		return ErrNotRegular
	case errors.Is(err, syscall.EXDEV), errors.Is(err, syscall.ELOOP):
		return ErrUnsafePath
	default:
		return err
	}
}
