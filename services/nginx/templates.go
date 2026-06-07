package nginx

import (
	"bytes"
	"text/template"
)

const siteTmplStr = `server {
    listen 80;
    listen [::]:80;
    server_name {{.Domain}};
    root {{.WebRoot}};

    add_header X-Frame-Options "SAMEORIGIN";
    add_header X-Content-Type-Options "nosniff";

    index index.php index.html index.htm;

    charset utf-8;

    location ^~ /.well-known/acme-challenge/ {
        allow all;
        root {{.WebRoot}};
    }

{{if ne .SSLProvider "none"}}
    listen 443 ssl;
    listen [::]:443 ssl;

    {{if eq .SSLProvider "letsencrypt"}}
    ssl_certificate /etc/letsencrypt/live/{{.Domain}}/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/{{.Domain}}/privkey.pem;
    {{end}}
    
    {{if eq .SSLProvider "custom"}}
    ssl_certificate /etc/nginx/ssl/{{.Domain}}/server.crt;
    ssl_certificate_key /etc/nginx/ssl/{{.Domain}}/server.key;
    {{end}}

    # Redirect non-HTTPS traffic
    if ($scheme != "https") {
        return 301 https://$host$request_uri;
    }
{{end}}

{{if eq .AppType "node"}}
    location / {
        proxy_pass http://127.0.0.1:{{.AppPort}};
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_cache_bypass $http_upgrade;
    }
{{else}}
    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }

    location ~ \.php$ {
        fastcgi_pass unix:/var/run/php/php{{.PHPVersion}}-fpm-{{.Domain}}.sock;
        fastcgi_param SCRIPT_FILENAME $realpath_root$fastcgi_script_name;
        include fastcgi_params;
        fastcgi_hide_header X-Powered-By;
    }
{{end}}

    location = /favicon.ico { access_log off; log_not_found off; }

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
}

func renderSiteTemplate(domain, webRoot, phpVersion, appType string, appPort int, sslProvider string) string {
	tmpl, err := template.New("site").Parse(siteTmplStr)
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
	})
	if err != nil {
		panic(err)
	}

	return buf.String()
}
