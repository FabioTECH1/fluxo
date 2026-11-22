package nginx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"fluxo/internal/syscmd"
)

type networkValidationOutput struct{ bytes.Buffer }

func (w *networkValidationOutput) Write(b []byte) (int, error) {
	if len(b) > 65536-w.Len() {
		return 0, errors.New("Nginx validation output exceeded its limit")
	}
	return w.Buffer.Write(b)
}
func validateNetworkConfig(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out := &networkValidationOutput{}
	err := syscmd.RunMaintenance(ctx, "", out, nil, nginxBinaryPath, "-t")
	if err != nil {
		return fmt.Errorf("Nginx config test failed: %s", strings.TrimSpace(out.String()))
	}
	if strings.Contains(strings.ToLower(out.String()), "conflicting server name") {
		return errors.New("Nginx reports conflicting hostname ownership; configuration was not activated")
	}
	return nil
}

// Network and regenerated configs share the existing rollback infrastructure,
// with conflicting-name warnings treated as refusal rather than silent loss.
func InstallNetworkConfigNamedTransactional(ctx context.Context, name, config string) error {
	if err := EnsureDefaultServer(ctx); err != nil {
		return err
	}
	return installSiteConfigWithOptions(ctx, siteConfigEnvironment{sitesAvailable: sitesAvailable, sitesEnabled: sitesEnabled, validate: validateNetworkConfig, reload: reloadService}, name, []byte(config), true)
}
