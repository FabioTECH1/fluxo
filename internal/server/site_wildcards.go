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
	if panel == parent || wildcardContains(parent, panel) {
		return errors.New("Wildcard routing cannot include the reserved panel hostname")
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
