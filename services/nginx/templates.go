package nginx

import (
	"bytes"
	"text/template"
)

const phpSiteTmplStr = `server {
    listen 80;
    listen [::]:80;
    server_name {{.Domain}};
    server_tokens off;
    root {{.WebRoot}};

    add_header X-Frame-Options "SAMEORIGIN";
    add_header X-XSS-Protection "1; mode=block";
    add_header X-Content-Type-Options "nosniff";

    index index.php index.html index.htm;
    charset utf-8;

    client_max_body_size 100M;

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
    server_name {{.Domain}};
    server_tokens off;
    root {{.WebRoot}};

    add_header X-Frame-Options "SAMEORIGIN";
    add_header X-Content-Type-Options "nosniff";

    index index.html index.htm;
    charset utf-8;

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

const nodeSiteTmplStr = `server {
    listen 80;
    listen [::]:80;
    server_name {{.Domain}};
    server_tokens off;

    add_header X-Frame-Options "SAMEORIGIN";
    add_header X-Content-Type-Options "nosniff";

    charset utf-8;

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

    location ^~ /.well-known/acme-challenge/ {
        allow all;
        root {{.WebRoot}};
    }

    location / {
        proxy_pass http://127.0.0.1:{{.AppPort}};
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
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
}

func renderSiteTemplate(domain, webRoot, phpVersion, appType string, appPort int, sslProvider string) string {
	tmplStr := phpSiteTmplStr
	switch appType {
	case "node":
		tmplStr = nodeSiteTmplStr
	case "html":
		tmplStr = htmlSiteTmplStr
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
	})
	if err != nil {
		panic(err)
	}

	return buf.String()
}
