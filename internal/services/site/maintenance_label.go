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

// A known global job is not an orphaned site and requires no domain binding.
func ScheduledTaskLabel(name, domain string, siteID int) string {
	if siteID == 0 {
		name = strings.TrimSpace(name)
		if name == "" {
			name = "Unnamed job"
		}
		return fmt.Sprintf("%q (server-wide)", name)
	}
	return MaintenanceLabel(name, domain)
}
