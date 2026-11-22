package nginx

import (
	"fmt"
	"regexp"
	"strings"
)

var wildcardParent = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+$`)

func rejectTLS(names []string) string {
	return "\nserver {\n    listen 443 ssl http2;\n    listen [::]:443 ssl http2;\n    server_name " + strings.Join(names, " ") + ";\n    ssl_reject_handshake on;\n    return 444;\n}\n"
}
func renderRejectedHost(domain, webRoot, phpVersion, pool, appType string, port int, g hostGroup) string {
	var http string
	if g.redirectTo == "" {
		http = renderSiteTemplateMode(domain, webRoot, phpVersion, pool, appType, port, "", "", "", "", g.domains, true)
	} else {
		http = "server {\n    listen 80;\n    listen [::]:80;\n    server_name " + strings.Join(g.domains, " ") + ";\n    root " + webRoot + ";\n    location ^~ /.well-known/acme-challenge/ { allow all; root " + webRoot + "; }\n    location / { return 301 https://" + g.redirectTo + "$request_uri; }\n}\n"
	}
	return http + rejectTLS(g.domains)
}

// HTTP Nginx wildcards can match multiple labels; X.509 wildcards cannot.
// HTTPS therefore uses a single-label regex followed by a broader rejection
// block. Exact-host sites always outrank these internally generated regexes.
// With no valid wildcard certificate every matching TLS handshake is rejected,
// never delegated to the site's root certificate or a fallback certificate.
func RenderWildcard(parent, domain, webRoot, phpVersion, pool, appType string, port int, certPath, keyPath string) (string, error) {
	if len(parent) > 253 || !wildcardParent.MatchString(parent) {
		return "", fmt.Errorf("Invalid wildcard parent")
	}
	http := renderSiteTemplateMode(domain, webRoot, phpVersion, pool, appType, port, "", "", "", "", []string{"*." + parent}, true)
	escaped := strings.ReplaceAll(parent, ".", `\.`)
	single := "~^[a-z0-9-]+\\." + escaped + "$"
	var tls string
	if certPath != "" && keyPath != "" {
		raw := renderSiteTemplateMode(domain, webRoot, phpVersion, pool, appType, port, certPath, keyPath, "", "", []string{single}, false)
		tokens, e := lexTrafficConfig(raw)
		if e != nil {
			return "", e
		}
		tree, e := parseTrafficSyntax(tokens, true)
		if e != nil || len(tree.children) != 2 {
			return "", fmt.Errorf("Invalid generated wildcard HTTPS configuration")
		}
		v := tree.children[1]
		tls = raw[v.start:v.close+1] + "\n"
		// HTTP redirects only for the one-label scope the certificate can cover.
		at := strings.Index(http, "{") + 1
		http = http[:at] + "\n    if ($host ~ \"^[a-z0-9-]+[.]" + strings.ReplaceAll(parent, ".", "[.]") + "$\") { return 301 https://$host$request_uri; }\n" + http[at:]
	}
	return http + "\n" + tls + rejectTLS([]string{"~^.+\\." + escaped + "$"}), nil
}
