package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"fluxo/internal/database"
	"fluxo/internal/safeinput"
	"fluxo/internal/services/nginx"
)

var legacyUnconfiguredHTTPSResponse = []byte(`return 421 "HTTPS is not configured for this site.\n";`)

type fallbackHTTPSMigrationSite struct {
	id         int
	domain     string
	configName string
}

func needsFallbackHTTPSMigration(config []byte) bool {
	return bytes.Contains(config, legacyUnconfiguredHTTPSResponse)
}

// MigrateLegacyUnconfiguredHTTPSConfigs regenerates only site configs created
// with the old fallback-certificate response. Unrelated Nginx files are untouched.
func MigrateLegacyUnconfiguredHTTPSConfigs() error {
	rows, err := database.DB.Query(`
		SELECT id, domain, path
		FROM sites
		WHERE COALESCE(deletion_status, '') = ''
		ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("load sites for fallback HTTPS migration: %w", err)
	}
	var migrationErr error
	var sites []fallbackHTTPSMigrationSite
	for rows.Next() {
		var site fallbackHTTPSMigrationSite
		var sitePath string
		if err := rows.Scan(&site.id, &site.domain, &sitePath); err != nil {
			migrationErr = errors.Join(migrationErr, fmt.Errorf("scan site for fallback HTTPS migration: %w", err))
			continue
		}
		managedPath, err := safeinput.NormalizeManagedSitePath(sitePath)
		if err != nil {
			migrationErr = errors.Join(migrationErr, fmt.Errorf("validate site path for %s: %w", site.domain, err))
			continue
		}
		site.configName = filepath.Base(managedPath)
		sites = append(sites, site)
	}
	if err := rows.Err(); err != nil {
		migrationErr = errors.Join(migrationErr, fmt.Errorf("iterate sites for fallback HTTPS migration: %w", err))
	}
	if err := rows.Close(); err != nil {
		migrationErr = errors.Join(migrationErr, fmt.Errorf("close fallback HTTPS migration query: %w", err))
	}

	attempted, regenerationErr := migrateLegacyUnconfiguredHTTPSConfigs(
		sites,
		"/etc/nginx/sites-available",
		regenerateNginxForSiteWithError,
	)
	migrationErr = errors.Join(migrationErr, regenerationErr)
	if attempted > 0 && regenerationErr != nil {
		migrationErr = errors.Join(migrationErr, nginx.Reload(context.Background()))
	}
	return migrationErr
}

func migrateLegacyUnconfiguredHTTPSConfigs(sites []fallbackHTTPSMigrationSite, availableDir string, regenerate func(int) error) (int, error) {
	attempted := 0
	var migrationErr error
	for _, site := range sites {
		configName := site.configName
		if configName == "" {
			configName = site.domain
		}
		configPath := filepath.Join(availableDir, configName)
		config, err := os.ReadFile(configPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			migrationErr = errors.Join(migrationErr, fmt.Errorf("inspect Nginx config for %s: %w", site.domain, err))
			continue
		}
		if !needsFallbackHTTPSMigration(config) {
			continue
		}
		attempted++
		if err := regenerate(site.id); err != nil {
			migrationErr = errors.Join(migrationErr, fmt.Errorf("regenerate Nginx config for %s: %w", site.domain, err))
		}
	}
	return attempted, migrationErr
}
