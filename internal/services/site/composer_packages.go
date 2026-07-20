package site

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const maxComposerLockSize = 16 << 20

type ComposerCapabilities struct {
	LockFound         bool
	Laravel           bool
	LaravelVersion    string
	Nightwatch        bool
	NightwatchVersion string
	Octane            bool
	OctaneVersion     string
}

type composerLock struct {
	Packages []composerPackage `json:"packages"`
}

type composerPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Extra   struct {
		BranchAlias map[string]string `json:"branch-alias"`
	} `json:"extra"`
}

// DetectComposerCapabilities inspects the lock file from the active application release.
func DetectComposerCapabilities(sitePath, deploymentStrategy string) (ComposerCapabilities, error) {
	file, err := os.Open(filepath.Join(ActiveSitePath(sitePath, deploymentStrategy), "composer.lock"))
	if err != nil {
		if os.IsNotExist(err) {
			return ComposerCapabilities{}, nil
		}
		return ComposerCapabilities{}, err
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxComposerLockSize+1))
	if err != nil {
		return ComposerCapabilities{}, err
	}
	if len(data) > maxComposerLockSize {
		return ComposerCapabilities{}, fmt.Errorf("composer.lock exceeds %d bytes", maxComposerLockSize)
	}

	var lock composerLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return ComposerCapabilities{}, fmt.Errorf("parse composer.lock: %w", err)
	}

	versions := make(map[string]string, len(lock.Packages))
	for _, pkg := range lock.Packages {
		version := pkg.Version
		if alias := pkg.Extra.BranchAlias[pkg.Version]; alias != "" {
			version = alias
		}
		versions[strings.ToLower(pkg.Name)] = version
	}

	laravelVersion, hasLaravel := versions["laravel/framework"]
	nightwatchVersion, hasNightwatch := versions["laravel/nightwatch"]
	octaneVersion, hasOctane := versions["laravel/octane"]

	laravel := hasLaravel && versionAtLeast(laravelVersion, 5, 0)
	return ComposerCapabilities{
		LockFound:         true,
		Laravel:           laravel,
		LaravelVersion:    laravelVersion,
		Nightwatch:        laravel && hasNightwatch,
		NightwatchVersion: nightwatchVersion,
		Octane:            laravel && hasOctane && versionAtLeast(octaneVersion, 1, 0),
		OctaneVersion:     octaneVersion,
	}, nil
}

func ActiveSitePath(sitePath, deploymentStrategy string) string {
	if deploymentStrategy == "zero-downtime" {
		return filepath.Join(sitePath, "current")
	}
	return sitePath
}

func versionAtLeast(version string, minimumMajor, minimumMinor int) bool {
	version = strings.TrimSpace(strings.TrimLeft(version, "vV"))
	parts := strings.Split(version, ".")
	if len(parts) == 0 {
		return false
	}

	major, ok := leadingVersionNumber(parts[0])
	if !ok {
		return false
	}
	minor := 0
	if len(parts) > 1 {
		if parsed, ok := leadingVersionNumber(parts[1]); ok {
			minor = parsed
		}
	}

	return major > minimumMajor || (major == minimumMajor && minor >= minimumMinor)
}

func leadingVersionNumber(part string) (int, bool) {
	end := 0
	for end < len(part) && part[end] >= '0' && part[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	value, err := strconv.Atoi(part[:end])
	return value, err == nil
}
