package nginx

import (
	"bytes"
	"text/template"
)

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

const phpSiteTmplStr = `server {
    listen 80;
    listen [::]:80;
    server_name {{.ServerName}};
    server_tokens off;
    root {{.WebRoot}};

` + `{{if ne .SSLProvider "none"}}` + `
    # Redirect to HTTPS
    return 301 https://$host$request_uri;
}
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name {{.ServerName}};
    server_tokens off;
    root {{.WebRoot}};

    {{if eq .SSLProvider "letsencrypt"}}
    ssl_certificate /etc/letsencrypt/live/{{.Domain}}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/{{.Domain}}/privkey.pem;
    {{end}}
    {{if eq .SSLProvider "custom"}}
    ssl_certificate /etc/nginx/ssl/{{.Domain}}/server.crt;
    ssl_certificate_key /etc/nginx/ssl/{{.Domain}}/server.key;
    {{end}}
    ` + tlsCommon + `
` + securityHeaders + `
` + `{{else}}` + `
` + securityHeaders + `
` + `{{end}}` + `
    index index.php index.html index.htm;
    charset utf-8;

    client_max_body_size 100M;

    location ^~ /.well-known/acme-challenge/ {
        allow all;
        root {{.WebRoot}};
    }

    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }

    location = /favicon.ico { access_log off; log_not_found off; }
    location = /robots.txt  { access_log off; log_not_found off; }

    access_log /var/log/nginx/{{.Domain}}.access.log;
    error_log  /var/log/nginx/{{.Domain}}.error.log error;

    error_page 404 /index.php;

    location ~ \.php$ {
        fastcgi_split_path_info ^(.+\.php)(/.+)$;
        fastcgi_pass unix:/var/run/php/php{{.PHPVersion}}-fpm-{{.Domain}}.sock;
        fastcgi_index index.php;
        fastcgi_param SCRIPT_FILENAME $realpath_root$fastcgi_script_name;
        include fastcgi_params;
        fastcgi_hide_header X-Powered-By;
        fastcgi_buffers 16 16k;
        fastcgi_buffer_size 32k;
    }

    location ~ /\.(?!well-known).* {
        deny all;
    }
}
`

const htmlSiteTmplStr = `server {
    listen 80;
    listen [::]:80;
    server_name {{.ServerName}};
    server_tokens off;
    root {{.WebRoot}};

` + `{{if ne .SSLProvider "none"}}` + `
    return 301 https://$host$request_uri;
}
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name {{.ServerName}};
    server_tokens off;
    root {{.WebRoot}};

    {{if eq .SSLProvider "letsencrypt"}}
    ssl_certificate /etc/letsencrypt/live/{{.Domain}}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/{{.Domain}}/privkey.pem;
    {{end}}
    {{if eq .SSLProvider "custom"}}
    ssl_certificate /etc/nginx/ssl/{{.Domain}}/server.crt;
    ssl_certificate_key /etc/nginx/ssl/{{.Domain}}/server.key;
    {{end}}
    ` + tlsCommon + `
` + securityHeaders + `
` + `{{else}}` + `
` + securityHeaders + `
` + `{{end}}` + `
    index index.html index.htm;
    charset utf-8;

    location ^~ /.well-known/acme-challenge/ {
        allow all;
        root {{.WebRoot}};
    }

    location / {
        try_files $uri $uri/ =404;
    }

    location = /favicon.ico { access_log off; log_not_found off; }
    location = /robots.txt  { access_log off; log_not_found off; }

    access_log /var/log/nginx/{{.Domain}}.access.log;
    error_log  /var/log/nginx/{{.Domain}}.error.log error;

    location ~ /\.(?!well-known).* {
        deny all;
    }
}
`

const nodeSiteTmplStr = `map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 80;
    listen [::]:80;
    server_name {{.ServerName}};
    server_tokens off;

` + `{{if ne .SSLProvider "none"}}` + `
    return 301 https://$host$request_uri;
}
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name {{.ServerName}};
    server_tokens off;

    {{if eq .SSLProvider "letsencrypt"}}
    ssl_certificate /etc/letsencrypt/live/{{.Domain}}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/{{.Domain}}/privkey.pem;
    {{end}}
    {{if eq .SSLProvider "custom"}}
    ssl_certificate /etc/nginx/ssl/{{.Domain}}/server.crt;
    ssl_certificate_key /etc/nginx/ssl/{{.Domain}}/server.key;
    {{end}}
    ` + tlsCommon + `
` + securityHeaders + `
` + `{{else}}` + `
` + securityHeaders + `
` + `{{end}}` + `
    charset utf-8;

    location ^~ /.well-known/acme-challenge/ {
        allow all;
        root {{.WebRoot}};
    }

    location / {
        proxy_pass http://127.0.0.1:{{.AppPort}};
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_cache_bypass $http_upgrade;
    }

    location = /favicon.ico { access_log off; log_not_found off; }
    location = /robots.txt  { access_log off; log_not_found off; }

    access_log /var/log/nginx/{{.Domain}}.access.log;
    error_log  /var/log/nginx/{{.Domain}}.error.log error;

    location ~ /\.(?!well-known).* {
        deny all;
    }
}
`

type siteConfig struct {
	Domain      string
	WebRoot     string
	PHPVersion  string
	AppType     string
	AppPort     int
	SSLProvider string
	ServerName  string
}

func renderSiteTemplate(domain, webRoot, phpVersion, appType string, appPort int, sslProvider string, aliases []string) string {
	tmplStr := phpSiteTmplStr
	switch appType {
	case "node":
		tmplStr = nodeSiteTmplStr
	case "html":
		tmplStr = htmlSiteTmplStr
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
		SSLProvider: sslProvider,
		ServerName:  serverName,
	})
	if err != nil {
		panic(err)
	}

	return buf.String()
}
