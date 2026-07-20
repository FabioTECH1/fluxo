package ssl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fluxo/internal/syscmd"
)

// IssueLetsEncrypt requests a Let's Encrypt certificate for a site's complete hostname set.
func IssueLetsEncrypt(ctx context.Context, domains []string, webRoot, email string) error {
	cleanDomains := make([]string, 0, len(domains))
	seen := make(map[string]struct{}, len(domains))
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		key := strings.ToLower(domain)
		if domain == "" {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		cleanDomains = append(cleanDomains, domain)
	}
	if len(cleanDomains) == 0 {
		return fmt.Errorf("at least one domain is required")
	}
	cmd := []string{
		"certbot", "certonly", "--webroot",
		"-w", webRoot,
		"--cert-name", cleanDomains[0],
	}
	for _, domain := range cleanDomains {
		cmd = append(cmd, "-d", domain)
	}
	cmd = append(cmd,
		"--non-interactive",
		"--agree-tos",
		"-m", email,
		"--deploy-hook", "systemctl reload nginx",
	)

	_, err := syscmd.Run(ctx, 5*time.Minute, cmd[0], cmd[1:]...)
	if err != nil {
		return fmt.Errorf("certbot execution failed: %w", err)
	}

	return nil
}

// DeleteLetsEncrypt removes a Certbot-managed lineage after it is no longer used by Nginx.
func DeleteLetsEncrypt(ctx context.Context, certPath string) error {
	name, err := certbotCertificateName(certPath)
	if err != nil {
		return err
	}

	renewalPath := filepath.Join("/etc/letsencrypt/renewal", name+".conf")
	_, certErr := os.Lstat(certPath)
	_, renewalErr := os.Stat(renewalPath)
	if os.IsNotExist(certErr) && os.IsNotExist(renewalErr) {
		return nil
	}

	_, err = syscmd.Run(ctx, 2*time.Minute,
		"certbot", "delete", "--cert-name", name, "--non-interactive",
	)
	if err != nil {
		return fmt.Errorf("failed to remove Certbot certificate %s: %w", name, err)
	}
	return nil
}

func certbotCertificateName(certPath string) (string, error) {
	const liveRoot = "/etc/letsencrypt/live"
	rel, err := filepath.Rel(liveRoot, filepath.Clean(certPath))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("certificate is outside Certbot-managed storage")
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) != 2 || parts[0] == "" || parts[1] != "fullchain.pem" {
		return "", fmt.Errorf("certificate does not reference a valid Certbot lineage")
	}
	return parts[0], nil
}
