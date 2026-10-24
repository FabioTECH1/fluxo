package php

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"fluxo/internal/syscmd"
)

// InstalledFPM preserves package/executable facts independently of old /etc/php
// directories. Partial/custom runtimes are unavailable; residual config alone
// never counts as an installation.
type InstalledFPM struct {
	PackageVersion string
	FullVersion    string
	Available      bool
	Configured     bool
}

var fpmPackagePattern = regexp.MustCompile(`^php([0-9]{1,2}\.[0-9]{1,2})-fpm$`)
var fpmBinaryPattern = regexp.MustCompile(`^php-fpm([0-9]{1,2}\.[0-9]{1,2})$`)

func InstalledFPMVersions(ctx context.Context) (map[string]InstalledFPM, error) {
	// Query all records so an empty PHP installation is a successful inventory,
	// rather than dpkg-query's ambiguous no-matching-package exit status.
	output, err := syscmd.Run(ctx, 5*time.Second, "/usr/bin/dpkg-query", "-W", "-f=${Package}\t${db:Status-Status}\t${Version}\n")
	if err != nil {
		return nil, fmt.Errorf("Could not read installed PHP packages; installation status is unavailable: %w", err)
	}
	versions := installedFPMVersions(output, os.Stat)
	entries, err := os.ReadDir("/usr/sbin")
	if err != nil {
		return nil, fmt.Errorf("Could not inspect existing PHP-FPM executables: %w", err)
	}
	// Preserve custom/partial installations as unavailable. They must not be
	// mistaken for an absent package and offered a blind replacement install.
	for _, entry := range entries {
		match := fpmBinaryPattern.FindStringSubmatch(entry.Name())
		if match != nil {
			if _, known := versions[match[1]]; !known {
				versions[match[1]] = InstalledFPM{}
			}
		}
	}
	return versions, nil
}

func installedFPMVersions(output string, stat func(string) (os.FileInfo, error)) map[string]InstalledFPM {
	versions := map[string]InstalledFPM{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 || fields[1] == "config-files" || fields[1] == "not-installed" {
			continue
		}
		match := fpmPackagePattern.FindStringSubmatch(fields[0])
		if match == nil {
			continue
		}
		branch := match[1]
		item := InstalledFPM{PackageVersion: fields[2], Configured: fields[1] == "installed"}
		patch := regexp.MustCompile(`^(?:[0-9]+:)?(` + regexp.QuoteMeta(branch) + `\.[0-9]+)(?:[^0-9.]|$)`).FindStringSubmatch(fields[2])
		if len(patch) == 2 && fields[1] == "installed" {
			item.FullVersion = patch[1]
		}
		info, err := stat("/usr/sbin/php-fpm" + branch)
		item.Available = fields[1] == "installed" && err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
		versions[branch] = item
	}
	return versions
}
