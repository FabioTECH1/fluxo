package server

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"fluxo/internal/database"
	"fluxo/internal/services/nginx"
)

var errPanelRoutingConflict = errors.New("Panel hostname routing conflict")

func wildcardContains(parent, host string) bool { return strings.HasSuffix(host, "."+parent) }
func wildcardOverlap(a, b string) bool {
	return a == b || wildcardContains(a, b) || wildcardContains(b, a)
}
func setWildcard(s *database.SiteNetworkSettings, parent string, on bool) {
	out := []string{}
	for _, p := range s.Wildcards {
		if p != parent {
			out = append(out, p)
		}
	}
	if on {
		out = append(out, parent)
	}
	s.Wildcards = out
}
func validateWildcardOwner(siteID int, parent string, s database.SiteNetworkSettings) error {
	if !domainRegex.MatchString(parent) {
		return errors.New("Use a canonical parent domain for wildcard routing")
	}
	var panel string
	e := database.DB.QueryRow("SELECT COALESCE(domain,'') FROM panel_domain LIMIT 1").Scan(&panel)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	if err := wildcardPanelRoute(parent, panel, nginx.PanelConfigStatus); err != nil {
		return err
	}
	for _, v := range s.Wildcards {
		if v != parent && wildcardOverlap(v, parent) {
			domains, e := configuredSiteDomains(siteID)
			if e != nil {
				return e
			}
			if domains[v] {
				return fmt.Errorf("Wildcard scope overlaps %s", v)
			}
		}
	}
	rows, e := database.DB.Query("SELECT site_id,settings FROM site_network_settings WHERE site_id!=?", siteID)
	if e != nil {
		return e
	}
	var ids []int
	for rows.Next() {
		var id int
		var raw string
		if e = rows.Scan(&id, &raw); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, id := range ids {
		other, e := database.GetSiteNetwork(id)
		if e != nil {
			return e
		}
		wild, e := activeWildcards(id, other)
		if e != nil {
			return e
		}
		for _, v := range wild {
			if wildcardOverlap(parent, v) {
				return fmt.Errorf("Wildcard scope is already owned by %s", v)
			}
		}
	}
	// Operator-written wildcard/regex vhosts cannot be silently adopted or
	// shadowed. Ordinary exact hosts retain normal Nginx priority.
	rows, e = database.DB.Query("SELECT config FROM site_vhost_overrides")
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			return e
		}
		names, err := nginx.ServerNames(raw)
		if err != nil {
			return err
		}
		for _, name := range names {
			name = strings.ToLower(strings.TrimSuffix(name, "."))
			if panel != "" && strings.EqualFold(name, panel) {
				return errors.New("Custom hostname routing conflicts with the reserved panel hostname")
			}
			if strings.HasPrefix(name, "~") || strings.Contains(name, "$") || strings.HasSuffix(name, ".*") {
				return errors.New("Review dynamic custom hostname matching before enabling managed wildcard routing")
			}
			if strings.HasPrefix(name, "*.") || strings.HasPrefix(name, ".") {
				claimed := strings.TrimPrefix(strings.TrimPrefix(name, "*"), ".")
				if wildcardOverlap(parent, claimed) {
					return errors.New("Wildcard scope overlaps operator-written hostname routing")
				}
			}
		}
	}
	return rows.Err()
}
func wildcardDomainState(siteID int, items []DomainItem) error {
	state, e := database.GetSiteNetwork(siteID)
	if e != nil {
		return e
	}
	override, e := database.GetSiteVhostOverride(siteID)
	if e != nil {
		return e
	}
	pending, e := database.PendingSiteNetwork(siteID)
	if e != nil {
		return e
	}
	for i := range items {
		item := &items[i]
		item.WildcardSSLState = "disabled"
		enabled := false
		for _, p := range state.Wildcards {
			if p == item.Domain {
				enabled = true
			}
		}
		item.Revision = domainSettingsRevision(item.Domain, item.WWWRedirect, enabled, override != nil)
		if !enabled {
			continue
		}
		if override != nil {
			item.WildcardSSLState = "inactive_custom"
			continue
		}
		item.Wildcard = true
		item.WildcardSSLState = "pending"
		if pending != nil {
			continue
		}
		cert, e := certificateAssignedToDomain(siteID, item.Domain)
		if e != nil {
			return e
		}
		if cert != nil && certificateCoversHostname(*cert, "*."+item.Domain) {
			item.WildcardSSLActive = true
			item.WildcardSSLState = "active"
		}
	}
	return nil
}
func importCertificateHostnames(siteID int, domain, behavior string) ([]string, error) {
	names := configuredDomainHostnames(domain, behavior)
	state, e := database.GetSiteNetwork(siteID)
	if e != nil {
		return nil, e
	}
	for _, p := range state.Wildcards {
		if p == domain {
			names = append(names, "*."+domain)
		}
	}
	return names, nil
}

func validateCustomNetworkClaims(siteID int, config string) error {
	names, e := nginx.ServerNames(config)
	if e != nil {
		return e
	}
	var panel string
	if e = database.DB.QueryRow("SELECT COALESCE(domain,'') FROM panel_domain LIMIT 1").Scan(&panel); e != nil && e != sql.ErrNoRows {
		return e
	}
	for _, name := range names {
		if panel != "" && strings.EqualFold(strings.TrimSuffix(name, "."), panel) {
			return errors.New("Custom hostname routing conflicts with the reserved panel hostname")
		}
	}
	rows, e := database.DB.Query("SELECT id FROM sites WHERE id!=?", siteID)
	if e != nil {
		return e
	}
	var ids []int
	for rows.Next() {
		var id int
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, id := range ids {
		state, e := database.GetSiteNetwork(id)
		if e != nil {
			return e
		}
		parents, e := activeWildcards(id, state)
		if e != nil {
			return e
		}
		for _, parent := range parents {
			for _, name := range names {
				name = strings.ToLower(strings.TrimSuffix(name, "."))
				if strings.HasPrefix(name, "~") || strings.Contains(name, "$") || strings.HasSuffix(name, ".*") {
					return errors.New("Disable managed wildcard routing before saving ambiguous custom hostname matching")
				}
				if strings.HasPrefix(name, "*.") || strings.HasPrefix(name, ".") {
					claimed := strings.TrimPrefix(strings.TrimPrefix(name, "*"), ".")
					if wildcardOverlap(parent, claimed) {
						return errors.New("Custom wildcard overlaps another managed wildcard owner")
					}
				}
			}
		}
	}
	return nil
}

func domainSettingsRevision(domain, behavior string, wildcard, custom bool) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%t\x00%t", domain, behavior, wildcard, custom)))
	return hex.EncodeToString(sum[:])
}

// Only a protected enabled exact panel route may coexist with the wildcard.
// Nginx exact names outrank managed wildcard/regex names on both listeners.
func wildcardPanelRoute(parent, panel string, route func(string) (bool, string)) error {
	if panel == "" {
		return nil
	}
	panel = strings.ToLower(strings.TrimSuffix(panel, "."))
	if panel == parent {
		return errors.New("Wildcard parent is the reserved panel hostname")
	}
	if wildcardContains(parent, panel) {
		if ok, _ := route(panel); !ok {
			return errors.New("Restore the panel's managed exact Nginx route before enabling this wildcard")
		}
	}
	return nil
}

// Moving/removing the exact route would expose the old reserved hostname to a
// site's wildcard (including its password/redirect rules or certificate). Do
// not implicitly make that ownership change; disable that scope first.
func guardPanelWildcardDeparture(current, next string) error {
	current = strings.ToLower(strings.TrimSuffix(current, "."))
	next = strings.ToLower(strings.TrimSuffix(next, "."))
	if current == "" || current == next {
		return nil
	}
	rows, err := database.DB.Query("SELECT site_id FROM site_network_settings")
	if err != nil {
		return err
	}
	var ids []int
	for rows.Next() {
		var id int
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		state, err := database.GetSiteNetwork(id)
		if err != nil {
			return err
		}
		scopes, err := activeWildcards(id, state)
		if err != nil {
			return err
		}
		for _, parent := range scopes {
			if parent == current || wildcardContains(parent, current) {
				return fmt.Errorf("%w: Disable wildcard routing for %s before moving or removing the panel hostname", errPanelRoutingConflict, parent)
			}
		}
	}
	return nil
}

// A later panel connection must not compete with an operator-written exact
// hostname. Managed wildcard/regex routes cannot shadow an exact panel route.
func ensureNoCustomPanelClaim(domain string) error {
	rows, err := database.DB.Query("SELECT config FROM site_vhost_overrides")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var config string
		if err = rows.Scan(&config); err != nil {
			return err
		}
		names, err := nginx.ServerNames(config)
		if err != nil {
			return err
		}
		for _, name := range names {
			if strings.EqualFold(strings.TrimSuffix(name, "."), domain) {
				return fmt.Errorf("%w: Panel hostname is claimed by an operator-written vhost; review it before connecting the panel", errPanelRoutingConflict)
			}
		}
	}
	return rows.Err()
}

// An interrupted Nginx publication can have proposed names on disk that are
// not yet reflected in saved settings. Keep the existing explicit recovery
// protocol; do not infer hostname ownership from that partial state.
func ensurePanelRoutingSettled() error {
	var n int
	if err := database.DB.QueryRow("SELECT count(*) FROM site_network_operations WHERE phase='pending'").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w: Restore the last confirmed Network configuration before changing the panel hostname", errPanelRoutingConflict)
	}
	return nil
}
