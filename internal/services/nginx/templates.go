package nginx

import (
	"bytes"
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
	Domain      string
	WebRoot     string
	PHPVersion  string
	AppType     string
	AppPort     int
	SSLCertPath string
	SSLKeyPath  string
	ServerName  string
}

// renderSiteTemplate selects the right template by app_type and renders it.
func renderSiteTemplate(domain, webRoot, phpVersion, appType string, appPort int, certPath, keyPath string, aliases []string) string {
	tmplStr := phpSiteTmplStr
	switch appType {
	case "node":
		tmplStr = nodeSiteTmplStr
	case "html":
		tmplStr = htmlSiteTmplStr
	case "wordpress":
		tmplStr = wordpressSiteTmplStr
	}

	serverName := domain
	for _, a := range aliases {
		if a != "" {
			serverName += " " + a
		}
	}

	tmpl, err := template.New("site").Parse(tmplStr)
	if err != nil {
		panic(err)
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, siteConfig{
		Domain:      domain,
		WebRoot:     webRoot,
		PHPVersion:  phpVersion,
		AppType:     appType,
		AppPort:     appPort,
		SSLCertPath: certPath,
		SSLKeyPath:  keyPath,
		ServerName:  serverName,
	})
	if err != nil {
		panic(err)
	}

	return buf.String()
}
