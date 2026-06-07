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

# 0.6. Provision Fluxo System User
echo "Creating fluxo system user..."
id -u fluxo &>/dev/null || sudo useradd fluxo -m -s /bin/bash -G www-data
sudo mkdir -p /home/fluxo/.ssh
sudo chmod 700 /home/fluxo/.ssh
sudo touch /home/fluxo/.ssh/authorized_keys
sudo chmod 600 /home/fluxo/.ssh/authorized_keys
sudo chown -R fluxo:fluxo /home/fluxo/.ssh

echo "Creating fluxo MySQL user..."
MYSQL_FLUXO_PASS=$(openssl rand -hex 16)
sudo mysql -e "CREATE USER IF NOT EXISTS 'fluxo'@'localhost' IDENTIFIED BY '${MYSQL_FLUXO_PASS}'"
sudo mysql -e "GRANT ALL PRIVILEGES ON *.* TO 'fluxo'@'localhost' WITH GRANT OPTION"
sudo mysql -e "FLUSH PRIVILEGES"
echo "MySQL fluxo user created. Password: ${MYSQL_FLUXO_PASS}"

# 0.7. Set Sudo Password for Fluxo User
echo "Setting fluxo user sudo password..."
FLUXO_SUDO_PASS=$(openssl rand -hex 8)
echo "fluxo:${FLUXO_SUDO_PASS}" | sudo chpasswd
sudo usermod -aG sudo fluxo
echo "Fluxo sudo password: ${FLUXO_SUDO_PASS}"

# 0.8. Disable SSH Password Authentication (key-only)
echo "Hardening SSH configuration..."
sudo sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config
sudo sed -i 's/^#\?ChallengeResponseAuthentication.*/ChallengeResponseAuthentication no/' /etc/ssh/sshd_config
sudo sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
sudo systemctl restart sshd

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
