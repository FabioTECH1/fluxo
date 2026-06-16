package nginx

// Nginx virtual host template for static HTML sites (try_files with 404 fallback).
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
