#!/bin/bash
set -e

echo "Starting Fluxo Installation..."

# Initialize credentials file (0600, root-only). Uses mkdir -p to
# handle fresh VPS where /home/fluxo doesn't exist yet.
CREDS_FILE="/home/fluxo/.fluxo_credentials"
sudo mkdir -p "$(dirname "$CREDS_FILE")"
if [ ! -f "$CREDS_FILE" ]; then
    sudo tee "$CREDS_FILE" > /dev/null <<'CREDS_EOF'
Fluxo Installation Credentials
==============================

CREDS_EOF
    sudo chmod 0600 "$CREDS_FILE"
fi

write_cred() {
    echo "$1" | sudo tee -a "$CREDS_FILE" > /dev/null
}

# 0. Install Dependencies
echo "Adding Ondřej Surý's PHP PPA..."
sudo apt-get update
sudo apt-get install -y software-properties-common
sudo add-apt-repository -y ppa:ondrej/php
sudo apt-get update

echo "Installing Nginx, PHP 8.4 FPM, Certbot, UFW, and Fail2Ban..."
sudo apt-get install -y nginx php8.4-fpm php8.4-cli php8.4-mysql php8.4-pgsql php8.4-sqlite3 php8.4-curl php8.4-mbstring php8.4-xml php8.4-gd php8.4-zip php8.4-bcmath php8.4-intl php8.4-redis certbot ufw fail2ban

echo "Setting PHP 8.4 as the default CLI version..."
sudo update-alternatives --set php /usr/bin/php8.4

echo "Installing Composer globally..."
EXPECTED_COMPOSER_SHA384="$(curl -sS https://composer.github.io/installer.sha384sum | awk '{print $1}')"
curl -sS -o /tmp/composer-setup.php https://getcomposer.org/installer
ACTUAL_COMPOSER_SHA384="$(sha384sum /tmp/composer-setup.php | awk '{print $1}')"
if [ "$EXPECTED_COMPOSER_SHA384" != "$ACTUAL_COMPOSER_SHA384" ]; then
    echo "ERROR: Composer installer checksum verification FAILED!"
    rm -f /tmp/composer-setup.php
    exit 1
fi
sudo php /tmp/composer-setup.php --install-dir=/usr/local/bin --filename=composer
rm -f /tmp/composer-setup.php

# 0.5. Initialize Firewall Safely
echo "Initializing UFW Firewall safely..."
sudo ufw allow 22/tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
# Dashboard port (9595) — restrict to your IP in production.
# You can manage firewall rules later via the Fluxo GUI (Settings > Network).
read -r -p "Open Fluxo dashboard port 9595 to the public internet? (y/n): " OPEN_DASHBOARD < /dev/tty
echo ""
if [ "$OPEN_DASHBOARD" = "y" ] || [ "$OPEN_DASHBOARD" = "Y" ]; then
    sudo ufw allow 9595/tcp
    echo "Port 9595 opened. Consider restricting to your IP via: sudo ufw allow from YOUR_IP to any port 9595"
    echo ""
else
    echo "Skipping port 9595. You can open it later via UFW or the Fluxo GUI."
    echo ""
fi
sudo ufw --force enable

# 0.55. Node.js (optional)
echo ""
echo "========================================="
echo "  NODE.JS"
echo "========================================="
if command -v node &>/dev/null; then
    echo "Node.js already installed ($(node --version)). Skipping."
    echo ""
else
    read -r -p "Install Node.js? It can also be installed later via the Fluxo GUI (Runtime > Node). (y/n): " INSTALL_NODE < /dev/tty
    echo ""
    if [ "$INSTALL_NODE" = "y" ] || [ "$INSTALL_NODE" = "Y" ]; then
        echo "Installing Node.js v22..."
        curl -fsSL -o /tmp/nodesource_setup.sh https://deb.nodesource.com/setup_22.x
        sudo -E bash /tmp/nodesource_setup.sh
        rm -f /tmp/nodesource_setup.sh
        sudo apt-get install -y nodejs
        echo "Node.js installed ($(node --version))."
        echo ""
    fi
fi


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
    done < /dev/tty
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
    write_cred "MySQL fluxo user password: ${MYSQL_FLUXO_PASS}"
    echo "MySQL fluxo user created."
    echo ""
fi

if [ "$INSTALL_POSTGRES" = true ]; then
    echo "Installing PostgreSQL..."
    sudo apt-get install -y postgresql

    echo "Creating fluxo PostgreSQL user..."
    PG_FLUXO_PASS=$(openssl rand -hex 16)
    sudo -u postgres psql -c "CREATE ROLE fluxo WITH LOGIN PASSWORD '${PG_FLUXO_PASS}' CREATEDB CREATEROLE" 2>/dev/null || true
    sudo -u postgres psql -c "GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO fluxo" 2>/dev/null || true
    write_cred "PostgreSQL fluxo user password: ${PG_FLUXO_PASS}"
    echo "PostgreSQL fluxo user created."
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
    read -r -p "Install Redis? (y/n): " INSTALL_REDIS < /dev/tty
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
sudo chmod 755 /home/fluxo
sudo mkdir -p /home/fluxo/.ssh
sudo chmod 700 /home/fluxo/.ssh
sudo touch /home/fluxo/.ssh/authorized_keys
sudo chmod 600 /home/fluxo/.ssh/authorized_keys
sudo chown -R fluxo:fluxo /home/fluxo/.ssh

# 0.8. Set Sudo Password and Sudoers Rules for Fluxo User
echo "Setting fluxo user sudo password and sudoers rules..."
FLUXO_SUDO_PASS=$(openssl rand -hex 8)
echo "fluxo:${FLUXO_SUDO_PASS}" | sudo chpasswd
echo "fluxo ALL=(ALL) NOPASSWD: /usr/bin/systemctl reload php*, /bin/systemctl reload php*" | sudo tee /etc/sudoers.d/fluxo > /dev/null
sudo chmod 0440 /etc/sudoers.d/fluxo
write_cred "Fluxo sudo password: ${FLUXO_SUDO_PASS}"
echo "Fluxo sudo password configured."

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

# Set your GitHub repo here, or override via FLUXO_GITHUB_REPO env var.
FLUXO_REPO="${FLUXO_GITHUB_REPO:-FabioTECH1/fluxo}"
FLUXO_VERSION="${FLUXO_VERSION:-latest}"

detect_arch() {
    case "$(uname -m)" in
        x86_64)  echo "amd64" ;;
        aarch64) echo "arm64" ;;
        *)       echo "unsupported" ;;
    esac
}

verify_checksum() {
    local binary=$1
    local sums_file=$2
    local binary_name=$(basename "$binary")
    local expected=$(grep "$binary_name" "$sums_file" | awk '{print $1}')
    local actual=$(sha256sum "$binary" | awk '{print $1}')
    if [ "$expected" != "$actual" ] || [ -z "$expected" ]; then
        echo "ERROR: Checksum verification FAILED!"
        echo "  Expected: $expected"
        echo "  Got:      $actual"
        sudo rm -f "$binary"
        return 1
    fi
    echo "Checksum verified OK."
}

if [ -f "./fluxo" ]; then
    echo "Using local fluxo binary..."
    sudo cp ./fluxo /usr/local/bin/fluxo

elif [ -n "$FLUXO_BINARY_URL" ]; then
    echo "Downloading binary from FLUXO_BINARY_URL..."
    sudo curl -fsSL -o /usr/local/bin/fluxo "$FLUXO_BINARY_URL"
    if [ -n "$FLUXO_BINARY_SHA256_URL" ]; then
        curl -fsSL -o /tmp/fluxo.sha256 "$FLUXO_BINARY_SHA256_URL"
        verify_checksum /usr/local/bin/fluxo /tmp/fluxo.sha256 || exit 1
        rm -f /tmp/fluxo.sha256
    fi

else
    ARCH=$(detect_arch)
    if [ "$ARCH" = "unsupported" ]; then
        echo "Error: Unsupported architecture: $(uname -m)"
        echo "Set FLUXO_BINARY_URL to provide a custom binary."
        exit 1
    fi
    echo "Detected architecture: $ARCH"

    if [ "$FLUXO_VERSION" = "latest" ]; then
        DOWNLOAD_URL="https://github.com/${FLUXO_REPO}/releases/latest/download/fluxo-linux-${ARCH}"
        CHECKSUM_URL="https://github.com/${FLUXO_REPO}/releases/latest/download/SHA256SUMS"
    else
        DOWNLOAD_URL="https://github.com/${FLUXO_REPO}/releases/download/${FLUXO_VERSION}/fluxo-linux-${ARCH}"
        CHECKSUM_URL="https://github.com/${FLUXO_REPO}/releases/download/${FLUXO_VERSION}/SHA256SUMS"
    fi

    echo "Downloading fluxo ${FLUXO_VERSION} (linux-${ARCH})..."
    if ! sudo curl -fsSL -o /usr/local/bin/fluxo "$DOWNLOAD_URL"; then
        echo "Error: Failed to download binary from $DOWNLOAD_URL"
        echo "Check that the release exists at https://github.com/${FLUXO_REPO}/releases"
        echo "You can also build from source if Go and npm are installed."
        exit 1
    fi

    echo "Verifying checksum..."
    if ! curl -fsSL -o /tmp/fluxo.sha256 "$CHECKSUM_URL"; then
        echo "Warning: Could not download checksums file. Skipping verification."
    else
        verify_checksum /usr/local/bin/fluxo /tmp/fluxo.sha256 || exit 1
        rm -f /tmp/fluxo.sha256
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
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ReadWritePaths=/var/lib/fluxo /var/log/fluxo /home/fluxo /etc/nginx/ssl
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK

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
    echo "  https://${ip}:9595"
done
echo ""
echo "The dashboard uses a self-signed TLS certificate."
echo "Your browser will show a security warning — accept it to proceed."
echo ""
echo "Credentials stored in: ${CREDS_FILE} (root-only, chmod 0600)"
echo "Read them with: sudo cat ${CREDS_FILE}"
echo ""
echo "Waiting for daemon to print the Day Zero token..."
sleep 3
token_line=$(sudo journalctl -u fluxo -n 50 --no-pager | grep "Token:" || true)
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
