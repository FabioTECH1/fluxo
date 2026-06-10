#!/bin/bash
set -e

echo "Starting Fluxo Installation..."

# 0. Install Dependencies
echo "Adding Ondřej Surý's PHP PPA..."
sudo apt-get update
sudo apt-get install -y software-properties-common
sudo add-apt-repository -y ppa:ondrej/php
sudo apt-get update

echo "Installing Nginx, PHP 8.4 FPM, Certbot, and UFW..."
sudo apt-get install -y nginx php8.4-fpm php8.4-cli php8.4-mysql php8.4-pgsql php8.4-sqlite3 php8.4-curl php8.4-mbstring php8.4-xml php8.4-gd php8.4-zip php8.4-bcmath php8.4-intl php8.4-redis certbot ufw

echo "Setting PHP 8.4 as the default CLI version..."
sudo update-alternatives --set php /usr/bin/php8.4

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
MYSQL_EXISTS=false
POSTGRES_EXISTS=false
if command -v mysql &>/dev/null || command -v mariadb &>/dev/null; then
    MYSQL_EXISTS=true
fi
if command -v psql &>/dev/null; then
    POSTGRES_EXISTS=true
fi

if [ "$MYSQL_EXISTS" = true ] || [ "$POSTGRES_EXISTS" = true ]; then
    echo "Existing database engine(s) detected:"
    [ "$MYSQL_EXISTS" = true ] && echo " - MySQL / MariaDB (already installed)"
    [ "$POSTGRES_EXISTS" = true ] && echo " - PostgreSQL (already installed)"
    echo "Skipping database engine selection prompt."
    echo ""
    INSTALL_MYSQL=false
    INSTALL_POSTGRES=false
else
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
fi

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
if command -v redis-server &>/dev/null; then
    echo "Redis is already installed. Skipping installation."
    echo ""
else
    echo "Install Redis? It can also be installed later via the Fluxo GUI (Runtime > Databases)."
    read -r -p "Install Redis? (y/n): " INSTALL_REDIS
    echo ""

    if [ "$INSTALL_REDIS" = "y" ] || [ "$INSTALL_REDIS" = "Y" ]; then
        echo "Installing Redis..."
        sudo apt-get install -y redis-server
        echo "Redis installed."
        echo ""
    fi
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
sudo systemctl restart ssh || sudo systemctl restart sshd

# 1. Install Binary
echo "Installing binary to /usr/local/bin..."
if systemctl is-active --quiet fluxo; then
    echo "Stopping existing fluxo daemon..."
    sudo systemctl stop fluxo
fi
sudo rm -f /usr/local/bin/fluxo

if [ -f "./fluxo" ]; then
    echo "Using local fluxo binary..."
    sudo cp ./fluxo /usr/local/bin/fluxo
elif [ -n "$FLUXO_BINARY_URL" ]; then
    echo "Downloading binary from $FLUXO_BINARY_URL..."
    sudo curl -sSL -o /usr/local/bin/fluxo "$FLUXO_BINARY_URL"
else
    # Fallback to local compilation if tools exist, or print error
    if command -v go &>/dev/null && command -v npm &>/dev/null; then
        echo "Building from source..."
        cd ui && npm install && npm run build && cd ..
        go build -o fluxo main.go
        sudo cp fluxo /usr/local/bin/fluxo
    else
        echo "Error: No pre-compiled 'fluxo' binary found in current directory,"
        echo "and Go/NPM are not installed to compile from source."
        echo "Please define FLUXO_BINARY_URL to download the binary."
        exit 1
    fi
fi
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
echo "========================================="
echo ""
echo "Access the Fluxo panel at:"
ips=$(hostname -I)
for ip in $ips; do
    echo "  http://${ip}:8080"
done
echo ""
echo "Waiting for daemon to print the Day Zero token..."
sleep 3
token_line=$(sudo journalctl -u fluxo -n 50 --no-pager | grep "Token:")
if [ -n "$token_line" ]; then
    token_val=$(echo "$token_line" | awk -F 'Token:    ' '{print $2}' | xargs)
    echo "========================================="
    echo "DAY ZERO AUTHENTICATION"
    echo "Use this token with any username at first login."
    echo "Token:  $token_val"
    echo "========================================="
else
    echo "Warning: Could not retrieve bootstrap token automatically."
    echo "Please check the daemon logs manually using: journalctl -u fluxo -n 50"
fi
echo ""
echo "Check daemon status/logs using: journalctl -u fluxo -f"
echo "========================================="
