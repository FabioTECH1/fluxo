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
func EnsureFPMExists(ctx context.Context, version string) error {
	serviceName := fmt.Sprintf("php%s-fpm", version)
	_, err := syscmd.Run(ctx, 5*time.Second, "systemctl", "status", serviceName)
	if err != nil {
		return fmt.Errorf("PHP-FPM service %s is not running or not installed: %w", serviceName, err)
	}
	return nil
}

// GeneratePoolConfig generates the FPM pool for the site and reloads the PHP-FPM service.
func GeneratePoolConfig(ctx context.Context, domain, version string) error {
	poolDir := fmt.Sprintf("/etc/php/%s/fpm/pool.d", version)

	os.MkdirAll(poolDir, 0755)

	configStr := renderPoolTemplate(domain, version)
	poolPath := filepath.Join(poolDir, fmt.Sprintf("%s.conf", domain))

	err := os.WriteFile(poolPath, []byte(configStr), 0644)
	if err != nil {
		return fmt.Errorf("failed to write PHP-FPM pool config: %w", err)
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
