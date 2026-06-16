package nginx

// Nginx virtual host template for PHP/Laravel sites (fastcgi pass to PHP-FPM socket).
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
