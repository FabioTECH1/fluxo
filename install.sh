#!/bin/bash
set -e

echo "Starting Fluxo Installation..."

# 0. Install Dependencies
echo "Installing Nginx, PHP 8.4 FPM, Certbot, MariaDB, and UFW..."
sudo apt-get update
sudo apt-get install -y nginx php8.4-fpm certbot mariadb-server ufw

# 0.5. Initialize Firewall Safely
echo "Initializing UFW Firewall safely..."
sudo ufw allow 22/tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw allow 8080/tcp
sudo ufw --force enable

# 1. Build Frontend
echo "Building Vue frontend..."
cd ui
npm install
npm run build
cd ..

# 2. Build Go Daemon
echo "Building Go daemon..."
go build -o fluxo main.go

# 3. Install Binary
echo "Installing to /usr/local/bin..."
sudo mv fluxo /usr/local/bin/fluxo
sudo chmod +x /usr/local/bin/fluxo

# 4. Setup Systemd Service
echo "Configuring systemd service..."
cat <<EOF | sudo tee /etc/systemd/system/fluxo.service
[Unit]
Description=Fluxo Management Daemon
After=network.target

[Service]
ExecStart=/usr/local/bin/fluxo
Restart=always
User=root
Environment=FLUXO_ENV=prod

[Install]
WantedBy=multi-user.target
EOF

# 5. Enable and Start
sudo systemctl daemon-reload
sudo systemctl enable fluxo
sudo systemctl start fluxo

echo "========================================="
echo "Fluxo installed successfully!"
echo "Check daemon logs using: journalctl -u fluxo -f"
echo "========================================="
