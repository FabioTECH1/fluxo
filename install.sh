#!/bin/bash
set -e

echo "Starting Fluxo Installation..."

# Initialize credentials file (0600, root-only). Uses mkdir -p to
# handle fresh VPS where /home/fluxo doesn't exist yet.
# Initialize credentials file (0600, root:root — fluxo user cannot read it).
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
sudo apt-get install -y nginx php8.4-fpm php8.4-cli php8.4-mysql php8.4-pgsql php8.4-sqlite3 php8.4-curl php8.4-mbstring php8.4-xml php8.4-gd php8.4-zip php8.4-bcmath php8.4-intl php8.4-redis certbot ufw fail2ban git

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
sudo ufw allow 9595/tcp
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
        echo "Installing Node.js via apt..."
        sudo apt-get install -y nodejs npm
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
    echo "  1) MySQL (MariaDB)"
    echo "  2) PostgreSQL"
    echo "  3) Both MySQL and PostgreSQL"
    echo "  4) None (install later via GUI)"
    echo ""
    while true; do
        read -r -p "Choose an option (1-4): " DB_REPLY < /dev/tty
        case $DB_REPLY in
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
sudo chown fluxo:fluxo /home/fluxo

# 0.8. Set Sudo Password and Sudoers Rules for Fluxo User
echo "Setting fluxo user sudo password and sudoers rules..."
FLUXO_SUDO_PASS=$(openssl rand -hex 8)
echo "fluxo:${FLUXO_SUDO_PASS}" | sudo chpasswd
sudo usermod -aG sudo fluxo
# sudo group membership is intentional — matches Forge/Coolify convention.
# The targeted NOPASSWD rule below handles automated php reloads without
# a password prompt. Interactive sudo use still requires the password.
echo "fluxo ALL=(ALL) NOPASSWD: /usr/bin/systemctl reload php*, /bin/systemctl reload php*" | sudo tee /etc/sudoers.d/fluxo > /dev/null
sudo chmod 0440 /etc/sudoers.d/fluxo
write_cred "Fluxo sudo password: ${FLUXO_SUDO_PASS}"
echo "Fluxo sudo password configured."

# 0.9. Disable SSH Password Authentication (if keys exist)
echo "Hardening SSH configuration..."
if [ -s "/root/.ssh/authorized_keys" ]; then
    echo "SSH keys found. Disabling password authentication..."
    sudo sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config
    sudo sed -i 's/^#\?ChallengeResponseAuthentication.*/ChallengeResponseAuthentication no/' /etc/ssh/sshd_config
    sudo sed -i 's/^#\?PermitRootLogin.*/PermitRootLogin prohibit-password/' /etc/ssh/sshd_config
    sudo systemctl restart ssh || sudo systemctl restart sshd
else
    echo "WARNING: No SSH keys found in /root/.ssh/authorized_keys — keeping password authentication enabled to prevent lockout."
    echo "Run 'ssh-copy-id root@<ip>' first, then re-run this script to harden SSH."
fi

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
    local grep_pattern=${3:-$(basename "$binary")}
    local expected=$(grep "$grep_pattern" "$sums_file" | awk '{print $1}')
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
    # Custom binary URL requires a checksum for security.
    if [ -z "$FLUXO_BINARY_SHA256_URL" ]; then
        echo "Error: FLUXO_BINARY_SHA256_URL is required when FLUXO_BINARY_URL is set."
        exit 1
    fi
    echo "Downloading binary from FLUXO_BINARY_URL..."
    sudo curl -fsSL -o /usr/local/bin/fluxo "$FLUXO_BINARY_URL"
    curl -fsSL -o /tmp/fluxo.sha256 "$FLUXO_BINARY_SHA256_URL"
    verify_checksum /usr/local/bin/fluxo /tmp/fluxo.sha256 || exit 1
    rm -f /tmp/fluxo.sha256

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
    # Download failure is fatal — MITM or CDN failure must not bypass verification.
    if ! curl -fsSL -o /tmp/fluxo.sha256 "$CHECKSUM_URL"; then
        echo "Error: Could not download checksums. Aborting for security."
        exit 1
    fi
    verify_checksum /usr/local/bin/fluxo /tmp/fluxo.sha256 "fluxo-linux-${ARCH}" || exit 1
    rm -f /tmp/fluxo.sha256
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
# Runs as root — required for systemctl, nginx writes, cron.d, ufw, apt, certbot.
# Industry standard for server management tools (Forge, ploi, Coolify all run as root-equivalent).
User=root
Environment=FLUXO_ENV=prod
PrivateTmp=true
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX AF_NETLINK

[Install]
WantedBy=multi-user.target
EOF

# 5. Enable and Start
sudo mkdir -p /etc/nginx/ssl
sudo systemctl daemon-reload
sudo systemctl enable fluxo
sudo systemctl start fluxo

echo ""
echo "Waiting for Fluxo daemon to become ready..."
for i in $(seq 1 10); do
    if curl -sk https://localhost:9595/api/v1/health 2>/dev/null | grep -q "ok"; then
        echo "Daemon is responding."
        break
    fi
    if [ $i -eq 10 ]; then
        echo "WARNING: Daemon did not respond after 10 seconds."
        echo "Check status with: sudo systemctl status fluxo"
        echo "Check logs with: sudo journalctl -u fluxo -n 50"
    fi
    sleep 1
done

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
echo "The certificate is auto-generated by the daemon on first boot (no install-script step needed)."
echo "Accept the browser warning to proceed."
echo ""
echo "Credentials stored in: ${CREDS_FILE} (root-only, chmod 0600)"
echo "Read them with: sudo cat ${CREDS_FILE}"
echo ""
