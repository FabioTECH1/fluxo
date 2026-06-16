package nginx

// Nginx virtual host template for Node.js sites (reverse proxy with WebSocket upgrade).
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
