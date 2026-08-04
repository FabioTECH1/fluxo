#!/bin/bash
set -e

# Parse CLI flags
INSTALL_NODE=""
INSTALL_REDIS=""
INSTALL_MYSQL=""
INSTALL_POSTGRES=""
LOCAL_BINARY=""
while [ $# -gt 0 ]; do
    case "$1" in
        --db-engine=*)
            ENGINE="${1#*=}"
            case "$ENGINE" in
                mysql)     INSTALL_MYSQL=true;  INSTALL_POSTGRES=false ;;
                postgres)  INSTALL_MYSQL=false; INSTALL_POSTGRES=true  ;;
                both)      INSTALL_MYSQL=true;  INSTALL_POSTGRES=true  ;;
                none)      INSTALL_MYSQL=false; INSTALL_POSTGRES=false ;;
                *)
                    echo "Invalid --db-engine value: $ENGINE"
                    echo "Valid: mysql, postgres, both, none"
                    exit 1
                    ;;
            esac
            ;;
        --redis)     INSTALL_REDIS=true  ;;
        --no-redis)  INSTALL_REDIS=false ;;
        --node)      INSTALL_NODE=true   ;;
        --no-node)   INSTALL_NODE=false  ;;
        --local-binary=*)
            LOCAL_BINARY="${1#*=}"
            if [ -z "$LOCAL_BINARY" ]; then
                echo "Error: --local-binary requires a path."
                exit 1
            fi
            ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --db-engine=mysql|postgres|both|none"
            echo "  --redis / --no-redis"
            echo "  --node  / --no-node  Install or skip the complete Node.js toolchain"
            echo "  --local-binary=PATH  Explicitly install a trusted local build"
            echo "  --help"
            echo ""
            echo "Environment variables:"
            echo "  FLUXO_VERSION        Release version (default: latest)"
            echo "  FLUXO_GITHUB_REPO    GitHub repo (default: FabioTECH1/fluxo)"
            echo "  FLUXO_BINARY_URL     Custom binary download URL"
            echo "  FLUXO_BINARY_SHA256_URL  SHA256 checksum URL"
            echo "  FLUXO_LOCAL_BINARY_SHA256  Optional checksum for --local-binary"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            echo "Use --help for usage."
            exit 1
            ;;
    esac
    shift
done

# Resolve the release before making system changes so the version announced at
# startup is the same release downloaded later in the installation.
FLUXO_REPO="${FLUXO_GITHUB_REPO:-FabioTECH1/fluxo}"
FLUXO_VERSION="${FLUXO_VERSION:-latest}"
INSTALL_VERSION_LABEL="$FLUXO_VERSION"

if [ -n "$LOCAL_BINARY" ]; then
    if [ "$FLUXO_VERSION" = "latest" ]; then
        INSTALL_VERSION_LABEL="local binary: $LOCAL_BINARY"
    else
        INSTALL_VERSION_LABEL="$FLUXO_VERSION (local binary)"
    fi
elif [ -n "${FLUXO_BINARY_URL:-}" ]; then
    if [ "$FLUXO_VERSION" = "latest" ]; then
        INSTALL_VERSION_LABEL="custom binary"
    else
        INSTALL_VERSION_LABEL="$FLUXO_VERSION (custom binary)"
    fi
elif [ "$FLUXO_VERSION" = "latest" ] && command -v curl >/dev/null 2>&1; then
    latest_release_url="$(curl -fsSIL --connect-timeout 10 --max-time 20 \
        -o /dev/null -w '%{url_effective}' \
        "https://github.com/${FLUXO_REPO}/releases/latest" 2>/dev/null || true)"
    if [[ "$latest_release_url" =~ /releases/tag/([^/?#]+)$ ]]; then
        FLUXO_VERSION="${BASH_REMATCH[1]}"
        INSTALL_VERSION_LABEL="$FLUXO_VERSION"
    fi
fi

echo "========================================="
echo "Starting Fluxo ${INSTALL_VERSION_LABEL} installation..."
echo "========================================="

# The daemon owns credential-file validation and legacy migration.
CREDS_DIR="/var/lib/fluxo"
CREDS_FILE="${CREDS_DIR}/.fluxo_credentials"
sudo mkdir -p "${CREDS_DIR}"
sudo chown root:root "${CREDS_DIR}"
sudo chmod 0700 "${CREDS_DIR}"

# 0. Install Dependencies
echo "Adding Ondřej Surý's PHP PPA..."
sudo apt-get update
sudo apt-get install -y software-properties-common
sudo add-apt-repository -y ppa:ondrej/php
sudo apt-get update

echo "Installing Nginx, PHP 8.4 FPM, Certbot, UFW, and Fail2Ban..."
sudo apt-get install -y nginx php8.4-fpm php8.4-cli php8.4-mysql php8.4-pgsql php8.4-sqlite3 php8.4-curl php8.4-mbstring php8.4-xml php8.4-gd php8.4-zip php8.4-bcmath php8.4-intl php8.4-redis certbot ufw fail2ban git curl gnupg ca-certificates

echo "Setting PHP 8.4 as the default CLI version..."
sudo update-alternatives --set php /usr/bin/php8.4

install_composer() (
    set -e
    local temp_dir expected actual
    temp_dir="$(mktemp -d)"
    trap 'rm -rf "$temp_dir"' EXIT

    expected="$(curl -fsSL https://composer.github.io/installer.sha384sum | awk '{print $1}')"
    curl -fsSL -o "$temp_dir/composer-setup.php" https://getcomposer.org/installer
    actual="$(sha384sum "$temp_dir/composer-setup.php" | awk '{print $1}')"
    if ! [[ "$expected" =~ ^[0-9a-f]{96}$ ]] || [ "$expected" != "$actual" ]; then
        echo "ERROR: Composer installer checksum verification FAILED!"
        exit 1
    fi
    sudo php "$temp_dir/composer-setup.php" --install-dir=/usr/local/bin --filename=composer
)

echo "Installing Composer globally..."
install_composer

install_wp_cli() (
    set -e
    local temp_dir fingerprint
    temp_dir="$(mktemp -d)"
    trap 'rm -rf "$temp_dir"' EXIT

    curl -fsSL -o "$temp_dir/wp-cli.phar" https://raw.githubusercontent.com/wp-cli/builds/gh-pages/phar/wp-cli.phar
    curl -fsSL -o "$temp_dir/wp-cli.phar.asc" https://raw.githubusercontent.com/wp-cli/builds/gh-pages/phar/wp-cli.phar.asc
    curl -fsSL -o "$temp_dir/wp-cli.pgp" https://raw.githubusercontent.com/wp-cli/builds/gh-pages/wp-cli.pgp

    mkdir "$temp_dir/gnupg"
    chmod 0700 "$temp_dir/gnupg"
    GNUPGHOME="$temp_dir/gnupg" gpg --batch --quiet --import "$temp_dir/wp-cli.pgp"
    fingerprint="$(GNUPGHOME="$temp_dir/gnupg" gpg --batch --with-colons --fingerprint releases@wp-cli.org | awk -F: '$1 == "fpr" { print $10; exit }')"
    if [ "$fingerprint" != "63AF7AA15067C05616FDDD88A3A2E8F226F0BC06" ]; then
        echo "ERROR: WP-CLI signing key fingerprint did not match the pinned official key."
        exit 1
    fi
    GNUPGHOME="$temp_dir/gnupg" gpg --batch --verify "$temp_dir/wp-cli.phar.asc" "$temp_dir/wp-cli.phar"
    php "$temp_dir/wp-cli.phar" --info >/dev/null
    sudo install -m 0755 "$temp_dir/wp-cli.phar" /usr/local/bin/wp
)

echo "Installing WP-CLI globally..."
install_wp_cli

# 0.5. Initialize Firewall Safely
echo "Initializing UFW Firewall safely..."
sudo ufw allow 22/tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw allow 9595/tcp
sudo ufw --force enable

# 0.55. Node.js toolchain selection (installation runs after the Fluxo binary is available)
echo ""
echo "========================================="
echo "  NODE.JS TOOLCHAIN"
echo "========================================="
if [ -n "$INSTALL_NODE" ]; then
    if [ "$INSTALL_NODE" = "true" ]; then
        echo "The complete Node.js toolchain will be installed."
    else
        echo "Skipping the Node.js toolchain (--no-node)."
    fi
elif command -v node &>/dev/null; then
    INSTALL_NODE=true
    echo "Existing Node.js detected ($(node --version)); Fluxo will complete and verify the toolchain."
else
    read -r -p "Install the Node.js toolchain (Node.js, npm, pnpm, Yarn, Corepack, and Bun)? It can also be installed later via Runtime > Node.js. (y/n): " INSTALL_NODE < /dev/tty
    echo ""
    if [ "$INSTALL_NODE" = "y" ] || [ "$INSTALL_NODE" = "Y" ]; then
        INSTALL_NODE=true
        echo "The complete Node.js toolchain will be installed."
    else
        INSTALL_NODE=false
        echo "Skipping the Node.js toolchain."
    fi
fi
echo ""


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

if [ -n "$INSTALL_MYSQL" ]; then
    echo "Database engine selection (from flags):"
    [ "$INSTALL_MYSQL" = "true" ]   && echo " - MySQL / MariaDB"
    [ "$INSTALL_POSTGRES" = "true" ] && echo " - PostgreSQL"
    [ "$INSTALL_MYSQL" != "true" ] && [ "$INSTALL_POSTGRES" != "true" ] && echo " - None"

    if [ "$INSTALL_MYSQL" = "true" ] && [ "$MYSQL_EXISTS" = true ]; then
        echo "Note: MySQL / MariaDB is already installed. Skipping MySQL installation."
        INSTALL_MYSQL=false
    fi
    if [ "$INSTALL_POSTGRES" = "true" ] && [ "$POSTGRES_EXISTS" = true ]; then
        echo "Note: PostgreSQL is already installed. Skipping PostgreSQL installation."
        INSTALL_POSTGRES=false
    fi
    echo ""
elif [ "$MYSQL_EXISTS" = true ] || [ "$POSTGRES_EXISTS" = true ]; then
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
    echo "MariaDB installed. Fluxo will create and securely store its administrator credentials on first start."
    echo ""
fi

if [ "$INSTALL_POSTGRES" = true ]; then
    echo "Installing PostgreSQL..."
    sudo apt-get install -y postgresql
    echo "PostgreSQL installed. Fluxo will create and securely store its administrator credentials on first start."
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
elif [ -n "$INSTALL_REDIS" ]; then
    if [ "$INSTALL_REDIS" = "true" ]; then
        echo "Installing Redis..."
        sudo apt-get install -y redis-server
        echo "Redis installed."
        echo ""
    else
        echo "Skipping Redis installation (--no-redis)."
        echo ""
    fi
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

# 0.8. Configure Sudoers Rules for Fluxo User
echo "Configuring fluxo sudoers rules..."
sudo usermod -aG sudo fluxo
# sudo group membership is intentional — matches Forge/Coolify convention.
# The targeted NOPASSWD rule below handles automated php reloads without
# a password prompt. Interactive sudo use still requires the password.
echo "fluxo ALL=(ALL) NOPASSWD: /usr/bin/systemctl reload php*, /bin/systemctl reload php*" | sudo tee /etc/sudoers.d/fluxo > /dev/null
sudo chmod 0440 /etc/sudoers.d/fluxo
echo "Fluxo sudo access configured. The daemon will generate its password on first start."

# 0.9. Disable SSH Password Authentication (if keys exist)
echo "Hardening SSH configuration..."
if [ -s "/root/.ssh/authorized_keys" ]; then
    echo "SSH keys found. Disabling password authentication..."
    SSH_DROPIN="/etc/ssh/sshd_config.d/00-fluxo-hardening.conf"
    sudo mkdir -p /etc/ssh/sshd_config.d
    cat <<'EOF' | sudo tee "$SSH_DROPIN" >/dev/null
# Managed by the Fluxo installer. This sorts before cloud-init SSH settings.
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin prohibit-password
EOF
    if ! sudo sshd -t; then
        echo "ERROR: SSH configuration validation failed. Removing the Fluxo drop-in."
        sudo rm -f "$SSH_DROPIN"
        exit 1
    fi
    SSH_EFFECTIVE="$(sudo sshd -T -C user=root,host=localhost,addr=127.0.0.1)"
    if ! printf '%s\n' "$SSH_EFFECTIVE" | grep -qx 'passwordauthentication no' ||
       ! printf '%s\n' "$SSH_EFFECTIVE" | grep -qx 'kbdinteractiveauthentication no' ||
       ! printf '%s\n' "$SSH_EFFECTIVE" | grep -Eq '^permitrootlogin (prohibit-password|without-password)$'; then
        echo "ERROR: Effective SSH settings do not match Fluxo's hardening policy."
        echo "Inspect with: sudo sshd -T -C user=root,host=localhost,addr=127.0.0.1"
        exit 1
    fi
    sudo systemctl reload ssh || sudo systemctl reload sshd
else
    echo "WARNING: No SSH keys found in /root/.ssh/authorized_keys — keeping password authentication enabled to prevent lockout."
    echo "Run 'ssh-copy-id root@<ip>' first, then re-run this script to harden SSH."
fi

# 1. Install Binary
echo "Installing binary to /usr/local/bin..."

detect_arch() {
    case "$(uname -m)" in
        x86_64)  echo "amd64" ;;
        aarch64) echo "arm64" ;;
        *)       echo "unsupported" ;;
    esac
}

verify_checksum() {
    local binary="$1"
    local sums_file="$2"
    local filename="${3:-}"
    local expected actual

    if [ -n "$filename" ]; then
        expected="$(awk -v name="$filename" '$2 == name || $2 == "*" name { print $1; exit }' "$sums_file")"
    else
        expected="$(awk 'NF { print $1; exit }' "$sums_file")"
    fi
    actual="$(sudo sha256sum "$binary" | awk '{print $1}')"
    expected="$(printf '%s' "$expected" | tr '[:upper:]' '[:lower:]')"
    if ! [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || [ "$expected" != "$actual" ]; then
        echo "ERROR: Checksum verification FAILED!"
        echo "  Expected: $expected"
        echo "  Got:      $actual"
        return 1
    fi
    echo "Checksum verified OK."
}

install_fluxo_binary() (
    set -e
    local binary_tmp=""
    local checksum_tmp=""
    local arch download_url checksum_url actual expected

    cleanup_binary_install() {
        [ -z "$checksum_tmp" ] || rm -f "$checksum_tmp"
        [ -z "$binary_tmp" ] || sudo rm -f "$binary_tmp"
    }
    trap cleanup_binary_install EXIT

    sudo mkdir -p /usr/local/bin
    binary_tmp="$(sudo mktemp /usr/local/bin/.fluxo-install.XXXXXX)"

    if [ -n "$LOCAL_BINARY" ]; then
        if [ ! -f "$LOCAL_BINARY" ] || [ -L "$LOCAL_BINARY" ]; then
            echo "Error: --local-binary must point to a regular, non-symlink file."
            exit 1
        fi
        echo "Installing explicitly selected local binary: $LOCAL_BINARY"
        sudo cp -- "$LOCAL_BINARY" "$binary_tmp"
        if [ -n "${FLUXO_LOCAL_BINARY_SHA256:-}" ]; then
            actual="$(sudo sha256sum "$binary_tmp" | awk '{print $1}')"
            expected="$(printf '%s' "$FLUXO_LOCAL_BINARY_SHA256" | tr '[:upper:]' '[:lower:]')"
            if ! [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || [ "$expected" != "$actual" ]; then
                echo "ERROR: Local binary checksum verification FAILED!"
                exit 1
            fi
            echo "Local binary checksum verified OK."
        fi

    elif [ -n "${FLUXO_BINARY_URL:-}" ]; then
        if [ -z "${FLUXO_BINARY_SHA256_URL:-}" ]; then
            echo "Error: FLUXO_BINARY_SHA256_URL is required when FLUXO_BINARY_URL is set."
            exit 1
        fi
        echo "Downloading binary from FLUXO_BINARY_URL..."
        sudo curl -fsSL -o "$binary_tmp" "$FLUXO_BINARY_URL"
        checksum_tmp="$(mktemp)"
        curl -fsSL -o "$checksum_tmp" "$FLUXO_BINARY_SHA256_URL"
        verify_checksum "$binary_tmp" "$checksum_tmp"

    else
        arch="$(detect_arch)"
        if [ "$arch" = "unsupported" ]; then
            echo "Error: Unsupported architecture: $(uname -m)"
            echo "Set FLUXO_BINARY_URL to provide a custom binary."
            exit 1
        fi
        echo "Detected architecture: $arch"

        if [ "$FLUXO_VERSION" = "latest" ]; then
            download_url="https://github.com/${FLUXO_REPO}/releases/latest/download/fluxo-linux-${arch}"
            checksum_url="https://github.com/${FLUXO_REPO}/releases/latest/download/SHA256SUMS"
        else
            download_url="https://github.com/${FLUXO_REPO}/releases/download/${FLUXO_VERSION}/fluxo-linux-${arch}"
            checksum_url="https://github.com/${FLUXO_REPO}/releases/download/${FLUXO_VERSION}/SHA256SUMS"
        fi

        echo "Downloading fluxo ${FLUXO_VERSION} (linux-${arch})..."
        if ! sudo curl -fsSL -o "$binary_tmp" "$download_url"; then
            echo "Error: Failed to download binary from $download_url"
            echo "Check that the release exists at https://github.com/${FLUXO_REPO}/releases"
            echo "You can also build from source if Go and npm are installed."
            exit 1
        fi

        echo "Verifying checksum..."
        checksum_tmp="$(mktemp)"
        if ! curl -fsSL -o "$checksum_tmp" "$checksum_url"; then
            echo "Error: Could not download checksums. Aborting for security."
            exit 1
        fi
        verify_checksum "$binary_tmp" "$checksum_tmp" "fluxo-linux-${arch}"
    fi

    sudo chmod 0755 "$binary_tmp"
    if [ "$INSTALL_NODE" = "true" ] && ! sudo "$binary_tmp" --supports-node-toolchain >/dev/null 2>&1; then
        echo "Error: The selected Fluxo binary does not support managed Node.js toolchains."
        echo "Install a newer Fluxo release or rerun with --no-node."
        exit 1
    fi
    if [ "$INSTALL_NODE" = "true" ]; then
        echo "Installing and verifying the Node.js toolchain..."
        sudo "$binary_tmp" node-toolchain install
        echo "Node.js toolchain installed successfully."
        echo ""
    fi
    sudo mv -f "$binary_tmp" /usr/local/bin/fluxo
    binary_tmp=""
)

install_fluxo_binary

installed_version_output="$(sudo /usr/local/bin/fluxo --version 2>/dev/null || true)"
case "$installed_version_output" in
    "fluxo version "*) INSTALLED_FLUXO_VERSION="${installed_version_output#fluxo version }" ;;
    *)                 INSTALLED_FLUXO_VERSION="$INSTALL_VERSION_LABEL" ;;
esac

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

configure_fluxo_fail2ban() {
    echo "Configuring Fail2Ban protection for Fluxo login attempts..."
    cat <<'EOF' | sudo tee /etc/fail2ban/filter.d/fluxo-auth.conf >/dev/null
[Definition]
failregex = ^.*fluxo_auth_failed remote=<HOST> username=.*$
ignoreregex =

[Init]
journalmatch = _SYSTEMD_UNIT=fluxo.service
EOF
    cat <<'EOF' | sudo tee /etc/fail2ban/jail.d/fluxo-auth.conf >/dev/null
[fluxo-auth]
enabled = true
filter = fluxo-auth
backend = systemd
port = 9595
protocol = tcp
maxretry = 5
findtime = 15m
bantime = 1h
banaction = ufw
EOF
    sudo fail2ban-client -t
    sudo systemctl enable fail2ban
    sudo systemctl restart fail2ban
}

configure_fluxo_fail2ban

# 5. Enable and Start
sudo mkdir -p /etc/nginx/ssl
sudo systemctl daemon-reload
sudo systemctl enable fluxo
sudo systemctl restart fluxo

echo ""
echo "Waiting for Fluxo daemon to become ready..."
# -sk is intentional — skips TLS verification for the self-signed cert on loopback.
for i in $(seq 1 60); do
    if curl -sk https://localhost:9595/api/v1/health 2>/dev/null | grep -q "ok"; then
        echo "Daemon is responding."
        break
    fi
    if [ $i -eq 60 ]; then
        echo "ERROR: Daemon did not respond after 60 seconds."
        echo "Check status with: sudo systemctl status fluxo"
        echo "Check logs with: sudo journalctl -u fluxo -n 50"
        exit 1
    fi
    sleep 1
done

# Retry token read — daemon may still be flushing the credentials file to disk.
bootstrap_token=""
for i in $(seq 1 3); do
    bootstrap_token=$(sudo grep "^Fluxo bootstrap token" "${CREDS_FILE}" 2>/dev/null | tail -1 | sed 's/^[^:]*: //')
    [ -n "$bootstrap_token" ] && break
    sleep 1
done
if [ -n "$bootstrap_token" ]; then
    echo ""
    echo "========================================================="
    echo "DAY ZERO AUTHENTICATION"
    echo "Use this token with any username at first login."
    echo "Token:    $bootstrap_token"
    echo "Please save this token. It will only be shown once."
    echo "========================================================="
fi

echo "========================================="
echo "Fluxo ${INSTALLED_FLUXO_VERSION} installed successfully!"
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
if [ -n "$bootstrap_token" ]; then
    echo "Bootstrap token stored in: ${CREDS_FILE} (root-only, chmod 0600)"
    echo "Read it with: sudo cat ${CREDS_FILE}"
else
    echo "No new bootstrap token was generated for this existing installation."
fi
echo ""
