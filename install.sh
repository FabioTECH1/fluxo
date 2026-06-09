#!/bin/bash
set -e

echo "Starting Fluxo Installation..."

# 0. Install Dependencies
echo "Installing Nginx, PHP 8.4 FPM, Certbot, and UFW..."
sudo apt-get update
sudo apt-get install -y nginx php8.4-fpm php8.4-cli php8.4-mysql php8.4-pgsql php8.4-sqlite3 php8.4-curl php8.4-mbstring php8.4-xml php8.4-gd php8.4-zip php8.4-bcmath php8.4-intl php8.4-redis certbot ufw

# 0.5. Initialize Firewall Safely
echo "Initializing UFW Firewall safely..."
sudo ufw allow 22/tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw allow 8080/tcp
sudo ufw --force enable

# 0.6. Database Engine Selection
echo ""
echo "========================================="
echo "  DATABASE ENGINE SELECTION"
echo "========================================="
echo "Which database engine(s) do you want to install?"
echo "You can install additional engines later via the Fluxo GUI (Runtime > Databases)."
echo ""
PS3="Choose an option (1-4): "
select db_choice in "MySQL (MariaDB)" "PostgreSQL" "Both MySQL and PostgreSQL" "None (install later via GUI)"; do
    case $REPLY in
        1) INSTALL_MYSQL=true; INSTALL_POSTGRES=false; break ;;
        2) INSTALL_MYSQL=false; INSTALL_POSTGRES=true; break ;;
        3) INSTALL_MYSQL=true; INSTALL_POSTGRES=true; break ;;
        4) INSTALL_MYSQL=false; INSTALL_POSTGRES=false; break ;;
        *) echo "Invalid option. Please choose 1-4." ;;
    esac
done
echo ""

if [ "$INSTALL_MYSQL" = true ]; then
    echo "Installing MariaDB (MySQL)..."
    sudo apt-get install -y mariadb-server

    echo "Creating fluxo MySQL user..."
    MYSQL_FLUXO_PASS=$(openssl rand -hex 16)
    sudo mysql -e "CREATE USER IF NOT EXISTS 'fluxo'@'localhost' IDENTIFIED BY '${MYSQL_FLUXO_PASS}'"
    sudo mysql -e "GRANT ALL PRIVILEGES ON *.* TO 'fluxo'@'localhost' WITH GRANT OPTION"
    sudo mysql -e "FLUSH PRIVILEGES"
    echo "MySQL fluxo user created. Password: ${MYSQL_FLUXO_PASS}"
    echo ""
fi

if [ "$INSTALL_POSTGRES" = true ]; then
    echo "Installing PostgreSQL..."
    sudo apt-get install -y postgresql

    echo "Creating fluxo PostgreSQL user..."
    PG_FLUXO_PASS=$(openssl rand -hex 16)
    sudo -u postgres psql -c "CREATE ROLE fluxo WITH LOGIN PASSWORD '${PG_FLUXO_PASS}' CREATEDB CREATEROLE" 2>/dev/null || true
    sudo -u postgres psql -c "GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO fluxo" 2>/dev/null || true
    echo "PostgreSQL fluxo user created. Password: ${PG_FLUXO_PASS}"
    echo ""
fi

# 0.6.5 Redis
echo ""
echo "========================================="
echo "  REDIS"
echo "========================================="
echo "Install Redis? It can also be installed later via the Fluxo GUI (Runtime > Databases)."
read -r -p "Install Redis? (y/n): " INSTALL_REDIS
echo ""

if [ "$INSTALL_REDIS" = "y" ] || [ "$INSTALL_REDIS" = "Y" ]; then
    echo "Installing Redis..."
    sudo apt-get install -y redis-server
    echo "Redis installed."
    echo ""
fi

# 0.7. Provision Fluxo System User
echo "Creating fluxo system user..."
id -u fluxo &>/dev/null || sudo useradd fluxo -m -s /bin/bash -G www-data
sudo mkdir -p /home/fluxo/.ssh
sudo chmod 700 /home/fluxo/.ssh
sudo touch /home/fluxo/.ssh/authorized_keys
sudo chmod 600 /home/fluxo/.ssh/authorized_keys
sudo chown -R fluxo:fluxo /home/fluxo/.ssh

# 0.8. Set Sudo Password for Fluxo User
echo "Setting fluxo user sudo password..."
FLUXO_SUDO_PASS=$(openssl rand -hex 8)
echo "fluxo:${FLUXO_SUDO_PASS}" | sudo chpasswd
sudo usermod -aG sudo fluxo
echo "Fluxo sudo password: ${FLUXO_SUDO_PASS}"

# 0.9. Disable SSH Password Authentication (key-only)
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
