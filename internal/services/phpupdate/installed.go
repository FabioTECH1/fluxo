package phpupdate

import (
	"context"
	"fmt"

	"fluxo/internal/services/php"
)

func requireInstalledFPM(ctx context.Context, version string) error {
	installed, err := php.InstalledFPMVersions(ctx)
	if err != nil {
		return err
	}
	return requireFPMInventory(version, installed)
}

func requireFPMInventory(version string, installed map[string]php.InstalledFPM) error {
	item, present := installed[version]
	if !present {
		return fmt.Errorf("%w: PHP %s has no installed FPM package. Use Install version in Runtime > PHP before checking for updates; retained configuration files are not an installed runtime", ErrNotInstalled, version)
	}
	if !item.Available {
		if item.PackageVersion == "" {
			return fmt.Errorf("%w: PHP %s FPM exists outside a configured APT package. Review that custom installation before automatic maintenance", ErrUnsupported, version)
		}
		if !item.Configured {
			return fmt.Errorf("%w: PHP %s FPM's package is not fully configured. Resolve that package installation before checking for updates", ErrUnsupported, version)
		}
		return fmt.Errorf("%w: PHP %s FPM's package is installed but its executable is missing or unusable. Repair the package before checking for updates", ErrUnsupported, version)
	}
	return nil
}
