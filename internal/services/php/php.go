package php

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"fluxo/syscmd"
)

// EnsureFPMExists checks if the FPM service for the specified version exists.
// If php-fpm is not installed, it silently skips.
func EnsureFPMExists(ctx context.Context, version string) error {
	if _, err := os.Stat("/usr/sbin/php-fpm" + version); os.IsNotExist(err) {
		return nil
	}
	serviceName := fmt.Sprintf("php%s-fpm", version)
	_, err := syscmd.Run(ctx, 5*time.Second, "systemctl", "status", serviceName)
	if err != nil {
		return nil // Not fatal — allow site creation without FPM
	}
	return nil
}

// GeneratePoolConfig generates the FPM pool for the site and reloads the PHP-FPM service.
// If php-fpm is not installed, it silently skips.
func GeneratePoolConfig(ctx context.Context, domain, version string) error {
	poolDir := fmt.Sprintf("/etc/php/%s/fpm/pool.d", version)

	// Silently skip if php isn't installed
	if _, err := os.Stat(fmt.Sprintf("/etc/php/%s", version)); os.IsNotExist(err) {
		return nil
	}

	os.MkdirAll(poolDir, 0755)

	configStr := renderPoolTemplate(domain, version)
	poolPath := filepath.Join(poolDir, fmt.Sprintf("%s.conf", domain))

	err := os.WriteFile(poolPath, []byte(configStr), 0644)
	if err != nil {
		return nil // Non-fatal on dev
	}

	return ReloadFPM(ctx, version)
}

// ReloadFPM safely reloads the PHP-FPM service for a specific version.
func ReloadFPM(ctx context.Context, version string) error {
	serviceName := fmt.Sprintf("php%s-fpm", version)
	_, err := syscmd.Run(ctx, 10*time.Second, "systemctl", "reload", serviceName)
	if err != nil {
		return fmt.Errorf("failed to reload %s: %w", serviceName, err)
	}
	return nil
}
