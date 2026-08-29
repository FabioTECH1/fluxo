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
	PHPFPMName          string
	AppType             string
	AppPort             int
	SSLCertPath         string
	SSLKeyPath          string
	FallbackSSLCertPath string
	FallbackSSLKeyPath  string
	ServerName          string
}

const unconfiguredHTTPSListeners = `
{{if not .SSLCertPath}}
    listen 443 ssl http2;
    listen [::]:443 ssl http2;

    ssl_certificate {{.FallbackSSLCertPath}};
    ssl_certificate_key {{.FallbackSSLKeyPath}};
    ssl_protocols TLSv1.2 TLSv1.3;
{{end}}
`

// renderSiteTemplate selects the right template by app_type and renders it.
func renderSiteTemplate(domain, webRoot, phpVersion, appType string, appPort int, certPath, keyPath, fallbackCertPath, fallbackKeyPath string, serverNames []string) string {
	return renderSiteTemplateWithPool(domain, webRoot, phpVersion, domain, appType, appPort, certPath, keyPath, fallbackCertPath, fallbackKeyPath, serverNames)
}

func renderSiteTemplateWithPool(domain, webRoot, phpVersion, phpFPMName, appType string, appPort int, certPath, keyPath, fallbackCertPath, fallbackKeyPath string, serverNames []string) string {
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
		PHPFPMName:          phpFPMName,
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

func renderRedirectHost(domain, webRoot, redirectTo, certPath, keyPath, fallbackCertPath, fallbackKeyPath string, serverNames []string) string {
	serverName := strings.Join(serverNames, " ")
	if serverName == "" {
		serverName = domain
	}
	if certPath == "" || keyPath == "" {
		return strings.TrimSpace(`
server {
    listen 80;
    listen [::]:80;
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name `+serverName+`;
    server_tokens off;
    root `+webRoot+`;

    ssl_certificate `+fallbackCertPath+`;
    ssl_certificate_key `+fallbackKeyPath+`;
    ssl_protocols TLSv1.2 TLSv1.3;

    location ^~ /.well-known/acme-challenge/ {
        allow all;
        root `+webRoot+`;
    }

    location / {
        return 301 https://`+redirectTo+`$request_uri;
    }
}
`) + "\n"
	}
	return strings.TrimSpace(`
server {
    listen 80;
    listen [::]:80;
    server_name `+serverName+`;
    server_tokens off;
    root `+webRoot+`;

    location ^~ /.well-known/acme-challenge/ {
        allow all;
        root `+webRoot+`;
    }

    location / {
        return 301 https://`+redirectTo+`$request_uri;
    }
}

server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name `+serverName+`;
    server_tokens off;

    ssl_certificate `+certPath+`;
    ssl_certificate_key `+keyPath+`;
`+tlsCommon+`
    return 301 https://`+redirectTo+`$request_uri;
}
`) + "\n"
}
