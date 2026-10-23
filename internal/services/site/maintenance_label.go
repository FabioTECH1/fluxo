package site

import (
	"fmt"
	"strings"
)

// MaintenanceLabel identifies an application consumer without exposing database
// IDs. Orphaned/global records stay visible even when no domain is available.
func MaintenanceLabel(name, domain string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Unnamed process"
	}
	if domain = strings.TrimSpace(domain); domain != "" {
		return fmt.Sprintf("%q on %s", name, domain)
	}
	return fmt.Sprintf("%q (no site domain available)", name)
}
