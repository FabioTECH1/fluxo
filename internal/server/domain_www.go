package server

import (
	"database/sql"
	"fmt"
	"strings"

	"fluxo/internal/database"
)

const (
	wwwRedirectNone = "none"
	wwwRedirectFrom = "from_www"
	wwwRedirectTo   = "to_www"
)

func normalizeWWWRedirect(domain, value, defaultValue string) (string, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(domain, "www.") {
		if value == "" || value == wwwRedirectNone {
			return wwwRedirectNone, nil
		}
		return "", fmt.Errorf("WWW redirects are unavailable when the configured domain already starts with www")
	}
	if value == "" {
		value = defaultValue
	}
	switch value {
	case wwwRedirectNone, wwwRedirectFrom, wwwRedirectTo:
		return value, nil
	default:
		return "", fmt.Errorf("invalid WWW redirect behavior")
	}
}

func wwwVariant(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" || strings.HasPrefix(domain, "www.") {
		return ""
	}
	return "www." + domain
}

func configuredDomainHostnames(domain, behavior string) []string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return nil
	}
	hosts := []string{domain}
	if behavior != wwwRedirectNone {
		if www := wwwVariant(domain); www != "" {
			hosts = append(hosts, www)
		}
	}
	return hosts
}

func configuredDomainRouting(domain, behavior string) (applicationHost, redirectHost, redirectTarget string) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	www := wwwVariant(domain)
	switch behavior {
	case wwwRedirectFrom:
		return domain, www, domain
	case wwwRedirectTo:
		return www, domain, www
	default:
		return domain, "", ""
	}
}

func domainWWWRedirect(siteID int, domain string) (string, error) {
	var behavior string
	err := database.DB.QueryRow(`
		SELECT www_redirect FROM (
			SELECT COALESCE(www_redirect, 'none') AS www_redirect, 0 AS ordering
			FROM sites WHERE id = ? AND domain = ? COLLATE NOCASE
			UNION ALL
			SELECT COALESCE(www_redirect, 'none') AS www_redirect, 1 AS ordering
			FROM domain_aliases WHERE site_id = ? AND domain = ? COLLATE NOCASE
		) ORDER BY ordering LIMIT 1`, siteID, domain, siteID, domain,
	).Scan(&behavior)
	return behavior, err
}

// ensureWWWRouteAvailable prevents a generated www host from shadowing an
// explicit domain or another domain's generated www host.
func ensureWWWRouteAvailable(siteID, domainID int, domain, behavior string) error {
	if behavior == wwwRedirectNone {
		return nil
	}
	www := wwwVariant(domain)
	if www == "" {
		return fmt.Errorf("WWW redirects are unavailable for this domain")
	}
	var inUse int
	err := database.DB.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM sites
			WHERE NOT (? = 0 AND id = ?) AND domain = ? COLLATE NOCASE
			UNION ALL
			SELECT 1 FROM domain_aliases
			WHERE NOT (? > 0 AND site_id = ? AND id = ?) AND domain = ? COLLATE NOCASE
			UNION ALL
			SELECT 1 FROM sites
			WHERE NOT (? = 0 AND id = ?) AND COALESCE(www_redirect, 'none') != 'none'
			  AND ('www.' || domain) = ? COLLATE NOCASE
			UNION ALL
			SELECT 1 FROM domain_aliases
			WHERE NOT (? > 0 AND site_id = ? AND id = ?)
			  AND COALESCE(www_redirect, 'none') != 'none'
			  AND ('www.' || domain) = ? COLLATE NOCASE
		)`, domainID, siteID, www, domainID, siteID, domainID, www,
		domainID, siteID, www, domainID, siteID, domainID, www,
	).Scan(&inUse)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if inUse == 1 {
		return fmt.Errorf("%s is already attached to another configured domain", www)
	}
	return nil
}
