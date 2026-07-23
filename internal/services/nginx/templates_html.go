package nginx

// Nginx virtual host template for static HTML sites (try_files with 404 fallback).
const htmlSiteTmplStr = `server {
    listen 80;
    listen [::]:80;
` + unconfiguredHTTPSListeners + `
    server_name {{.ServerName}};
    server_tokens off;
    root {{.WebRoot}};

` + `{{if .SSLCertPath}}` + `
    return 301 https://$host$request_uri;
}
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name {{.ServerName}};
    server_tokens off;
    root {{.WebRoot}};

    ssl_certificate {{.SSLCertPath}};
    ssl_certificate_key {{.SSLKeyPath}};
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
