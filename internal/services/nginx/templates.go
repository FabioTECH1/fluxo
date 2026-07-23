package nginx

import (
	"bytes"
	"strings"
	"text/template"
)

// Shared TLS and security snippets included in every template.
const tlsCommon = `    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384:ECDHE-ECDSA-CHACHA20-POLY1305:ECDHE-RSA-CHACHA20-POLY1305;
    ssl_prefer_server_ciphers on;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 10m;
    add_header Strict-Transport-Security "max-age=31536000" always;
`

const securityHeaders = `    add_header X-Frame-Options "SAMEORIGIN";
    add_header X-XSS-Protection "1; mode=block";
    add_header X-Content-Type-Options "nosniff";
    add_header Referrer-Policy "strict-origin-when-cross-origin";
`

type siteConfig struct {
	Domain              string
	WebRoot             string
	PHPVersion          string
	AppType             string
	AppPort             int
	SSLCertPath         string
	SSLKeyPath          string
	FallbackSSLCertPath string
	FallbackSSLKeyPath  string
	ServerName          string
}

const unconfiguredHTTPSBlock = `
{{if not .SSLCertPath}}
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name {{.ServerName}};
    server_tokens off;

    ssl_certificate {{.FallbackSSLCertPath}};
    ssl_certificate_key {{.FallbackSSLKeyPath}};
    ssl_protocols TLSv1.2 TLSv1.3;

    default_type text/plain;
    return 421 "HTTPS is not configured for this site.\n";
}
{{end}}
`

// renderSiteTemplate selects the right template by app_type and renders it.
func renderSiteTemplate(domain, webRoot, phpVersion, appType string, appPort int, certPath, keyPath, fallbackCertPath, fallbackKeyPath string, serverNames []string) string {
	tmplStr := phpSiteTmplStr
	switch appType {
	case "node":
		tmplStr = nodeSiteTmplStr
	case "html":
		tmplStr = htmlSiteTmplStr
	case "wordpress":
		tmplStr = wordpressSiteTmplStr
	}

	serverName := strings.Join(serverNames, " ")
	if serverName == "" {
		serverName = domain
	}

	tmpl, err := template.New("site").Parse(tmplStr)
	if err != nil {
		panic(err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, siteConfig{
		Domain:              domain,
		WebRoot:             webRoot,
		PHPVersion:          phpVersion,
		AppType:             appType,
		AppPort:             appPort,
		SSLCertPath:         certPath,
		SSLKeyPath:          keyPath,
		FallbackSSLCertPath: fallbackCertPath,
		FallbackSSLKeyPath:  fallbackKeyPath,
		ServerName:          serverName,
	})
	if err != nil {
		panic(err)
	}

	return buf.String()
}
