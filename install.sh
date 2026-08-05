#!/bin/bash
set -Eeuo pipefail

# Parse CLI flags
INSTALL_NODE=""
INSTALL_REDIS=""
INSTALL_MYSQL=""
INSTALL_POSTGRES=""
HARDEN_SSH=""
SSH_HARDENING_EXPLICIT=false
MANAGEMENT_CIDR=""
LOCAL_BINARY=""
MYSQL_EXISTS=false
POSTGRES_EXISTS=false
REDIS_EXISTS=false
EXISTING_INSTALL=false
MANAGED_FLUXO_ACCOUNT=false
SSH_PORTS=""
UFW_WAS_ACTIVE=false
UFW_RULES_MANAGED_BY_INSTALLER=false
PREPARED_BINARY=""
PREPARED_CHECKSUM=""
CANDIDATE_BINARY=""
ATTESTATION_TEMP_DIR=""
UPGRADE_BACKUP_DIR=""
UPGRADE_ROLLBACK_ARMED=false
UPGRADE_SERVICE_STOPPED=false
UPGRADE_WAS_ACTIVE=false
UPGRADE_WAS_ENABLED=false
NODE_ROLLBACK_DIR=""
NODE_ROLLBACK_ARMED=false
UPGRADE_RESTART_ALLOWED=true
CONFIG_ROLLBACK_DIR=""
CONFIG_ROLLBACK_ARMED=false
DEFAULT_COMPOSER_VERSION="2.10.2"
DEFAULT_COMPOSER_SHA256="5ee7125f8a30a34d246cefdc0bc85b8a783b28f2aec968994118512350d28027"
DEFAULT_WP_CLI_VERSION="2.12.0"
COMPOSER_VERSION="$DEFAULT_COMPOSER_VERSION"
COMPOSER_SHA256="$DEFAULT_COMPOSER_SHA256"
WP_CLI_VERSION="$DEFAULT_WP_CLI_VERSION"
SKIP_RELEASE_ATTESTATION=false
MIN_ATTESTED_FLUXO_VERSION="0.4.10"
ATTESTATION_GH_VERSION="2.94.0"
ATTESTATION_GH_AMD64_SHA256="a757f1ba6db18f4de8cbadb244843a5f89bc75b5e7c6fc127d2bd77fbd12ed62"
ATTESTATION_GH_ARM64_SHA256="705a23b70b0f1b7ba4c302fdcef392ce3edaacfa7ce8e85e4d93d72ea800a538"

cleanup_installer() {
    local status=$?
    trap - EXIT
    set +e
    [ -z "$PREPARED_CHECKSUM" ] || rm -f -- "$PREPARED_CHECKSUM"
    [ -z "$PREPARED_BINARY" ] || rm -f -- "$PREPARED_BINARY"
    [ -z "$ATTESTATION_TEMP_DIR" ] || rm -rf -- "$ATTESTATION_TEMP_DIR"
    [ -z "$CANDIDATE_BINARY" ] || sudo rm -f -- "$CANDIDATE_BINARY"

    if [ "$status" -ne 0 ]; then
        if [ "$NODE_ROLLBACK_ARMED" = true ]; then
            sudo systemctl stop fluxo >/dev/null 2>&1 || true
            if ! rollback_node_toolchain; then
                UPGRADE_RESTART_ALLOWED=false
            fi
        fi
        if [ "$UPGRADE_ROLLBACK_ARMED" = true ]; then
            rollback_upgrade
        elif [ "$UPGRADE_SERVICE_STOPPED" = true ] && [ "$UPGRADE_WAS_ACTIVE" = true ] && [ "$UPGRADE_RESTART_ALLOWED" = true ]; then
            echo "Restarting the unchanged Fluxo service after the interrupted upgrade..."
            sudo systemctl start fluxo
        fi
        if [ "$UPGRADE_RESTART_ALLOWED" != true ]; then
            echo "ERROR: Fluxo remains stopped because the Node.js toolchain could not be restored completely."
        fi
        if [ "$CONFIG_ROLLBACK_ARMED" = true ]; then
            rollback_upgrade_config || true
        fi
    fi
    exit "$status"
}
trap cleanup_installer EXIT

detect_arch() {
    case "$(uname -m)" in
        x86_64)  echo "amd64" ;;
        aarch64) echo "arm64" ;;
        *)       echo "unsupported" ;;
    esac
}

curl_fetch() {
    local destination="$1"
    local url="$2"
    local max_time="${3:-300}"
    curl --fail --silent --show-error --location \
        --retry 3 --retry-all-errors --retry-delay 2 \
        --connect-timeout 10 --max-time "$max_time" \
        -o "$destination" "$url"
}

atomic_install_root() {
    local source="$1"
    local target="$2"
    local mode="$3"
    local staged
    staged="$(sudo mktemp "${target}.install.XXXXXX")"
    if ! sudo install -m "$mode" -o root -g root "$source" "$staged" ||
       ! sudo mv -f "$staged" "$target"; then
        sudo rm -f -- "$staged"
        return 1
    fi
    return 0
}

snapshot_upgrade_config_file() {
    local target="$1" label="$2"
    if sudo test -L "$target"; then
        echo "ERROR: Refusing to snapshot symlinked Fluxo configuration $target."
        return 1
    fi
    if sudo test -f "$target"; then
        sudo touch "$CONFIG_ROLLBACK_DIR/${label}.present"
        sudo cp --preserve=mode,ownership,timestamps -- "$target" "$CONFIG_ROLLBACK_DIR/$label"
    else
        sudo touch "$CONFIG_ROLLBACK_DIR/${label}.absent"
    fi
}

prepare_upgrade_config_snapshot() {
    local timestamp
    if [ "$EXISTING_INSTALL" != true ]; then
        return
    fi
    timestamp="$(date -u +%Y%m%dT%H%M%SZ)-$$"
    CONFIG_ROLLBACK_DIR="/var/lib/fluxo/config-rollbacks/$timestamp"
    sudo install -d -m 0700 -o root -g root "$CONFIG_ROLLBACK_DIR"
    if ! snapshot_upgrade_config_file /etc/sudoers.d/fluxo sudoers ||
       ! snapshot_upgrade_config_file /etc/ssh/sshd_config.d/00-fluxo-hardening.conf ssh-hardening ||
       ! snapshot_upgrade_config_file /etc/fail2ban/filter.d/fluxo-auth.conf fail2ban-filter ||
       ! snapshot_upgrade_config_file /etc/fail2ban/jail.d/fluxo-auth.conf fail2ban-jail; then
        sudo rm -rf -- "$CONFIG_ROLLBACK_DIR"
        CONFIG_ROLLBACK_DIR=""
        return 1
    fi
    CONFIG_ROLLBACK_ARMED=true
}

restore_upgrade_config_file() {
    local target="$1" label="$2" mode="$3"
    if sudo test -f "$CONFIG_ROLLBACK_DIR/${label}.present"; then
        atomic_install_root "$CONFIG_ROLLBACK_DIR/$label" "$target" "$mode"
    elif sudo test -f "$CONFIG_ROLLBACK_DIR/${label}.absent"; then
        sudo rm -f -- "$target"
    else
        return 1
    fi
}

rollback_upgrade_config() {
    local restore_failed=false
    echo "Restoring pre-upgrade security configuration..."
    restore_upgrade_config_file /etc/sudoers.d/fluxo sudoers 0440 || restore_failed=true
    restore_upgrade_config_file /etc/ssh/sshd_config.d/00-fluxo-hardening.conf ssh-hardening 0644 || restore_failed=true
    restore_upgrade_config_file /etc/fail2ban/filter.d/fluxo-auth.conf fail2ban-filter 0644 || restore_failed=true
    restore_upgrade_config_file /etc/fail2ban/jail.d/fluxo-auth.conf fail2ban-jail 0644 || restore_failed=true
    sudo visudo -cf /etc/sudoers >/dev/null || restore_failed=true
    if command -v sshd >/dev/null 2>&1; then
        sudo sshd -t || restore_failed=true
        sudo systemctl reload ssh 2>/dev/null || sudo systemctl reload sshd 2>/dev/null || restore_failed=true
    fi
    if command -v fail2ban-client >/dev/null 2>&1; then
        sudo fail2ban-client -t >/dev/null || restore_failed=true
        sudo systemctl restart fail2ban >/dev/null || restore_failed=true
    fi
    if [ "$restore_failed" = true ]; then
        echo "ERROR: Security configuration rollback was incomplete. Snapshot retained at $CONFIG_ROLLBACK_DIR."
        return 1
    fi
    CONFIG_ROLLBACK_ARMED=false
    sudo rm -rf -- "$CONFIG_ROLLBACK_DIR"
    CONFIG_ROLLBACK_DIR=""
}

commit_upgrade_config_snapshot() {
    if [ "$CONFIG_ROLLBACK_ARMED" != true ]; then
        return
    fi
    CONFIG_ROLLBACK_ARMED=false
    sudo rm -rf -- "$CONFIG_ROLLBACK_DIR" || echo "WARNING: Could not remove security configuration snapshot $CONFIG_ROLLBACK_DIR."
    CONFIG_ROLLBACK_DIR=""
}

atomic_install_root_locked() {
    local source="$1"
    local target="$2"
    local mode="$3"
    local lock_file="$4"
    local staged
    staged="$(sudo mktemp "${target}.install.XXXXXX")"
    if ! sudo install -m "$mode" -o root -g root "$source" "$staged" ||
       ! sudo /usr/bin/flock -w 120 "$lock_file" mv -f "$staged" "$target"; then
        sudo rm -f -- "$staged"
        return 1
    fi
    return 0
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
    actual="$(sha256sum "$binary" | awk '{print $1}')"
    expected="$(printf '%s' "$expected" | tr '[:upper:]' '[:lower:]')"
    if ! [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || [ "$expected" != "$actual" ]; then
        echo "ERROR: Checksum verification FAILED!"
        echo "  Expected: $expected"
        echo "  Got:      $actual"
        return 1
    fi
    echo "Checksum verified OK."
}

local_database_server_exists() {
    local engine="$1"
    case "$engine" in
        mysql)
            [ -x /usr/sbin/mariadbd ] || [ -x /usr/sbin/mysqld ] || \
                sudo systemctl list-unit-files mariadb.service mysql.service --no-legend 2>/dev/null | grep -Eq '^(mariadb|mysql)\.service'
            ;;
        postgres)
            compgen -G '/usr/lib/postgresql/*/bin/postgres' >/dev/null || \
                sudo systemctl list-unit-files postgresql.service --no-legend 2>/dev/null | grep -q '^postgresql\.service'
            ;;
    esac
}

validate_existing_fluxo_account() {
    local passwd_entry username uid home shell home_owner
    if ! id -u fluxo >/dev/null 2>&1; then
        return
    fi

    if [ "$EXISTING_INSTALL" != true ] && [ "$MANAGED_FLUXO_ACCOUNT" != true ]; then
        echo "ERROR: A system account named 'fluxo' already exists, but no existing Fluxo installation was found."
        echo "The installer will not take over or modify an unrelated account."
        exit 1
    fi

    passwd_entry="$(getent passwd fluxo)"
    IFS=: read -r username _ uid _ _ home shell <<< "$passwd_entry"
    if [ "$username" != "fluxo" ] || [ "$uid" = "0" ] || [ "$home" != "/home/fluxo" ] || [ "$shell" != "/bin/bash" ]; then
        echo "ERROR: The existing fluxo account does not match Fluxo's managed account settings."
        echo "Expected a non-root account with home /home/fluxo and shell /bin/bash."
        exit 1
    fi
    if [ -d "$home" ]; then
        home_owner="$(stat -c '%U' "$home")"
        if [ "$home_owner" != "fluxo" ]; then
            echo "ERROR: /home/fluxo is owned by $home_owner instead of fluxo."
            exit 1
        fi
    fi
}

validate_existing_fluxo_installation() {
    local service=/etc/systemd/system/fluxo.service
    local binary=/usr/local/bin/fluxo
    local database_path=/var/lib/fluxo/fluxo.db
    local marker=/var/lib/fluxo/.managed-system-user
    local service_owner service_mode binary_owner binary_mode database_owner database_mode
    local exec_start version_output service_exists=false artifacts_exist=false

    if sudo test -e "$marker" || sudo test -L "$marker"; then
        if sudo test -L "$marker" || ! sudo test -f "$marker" ||
           [ "$(sudo stat -c '%U:%G' "$marker")" != "root:root" ] ||
           (( (8#$(sudo stat -c '%a' "$marker") & 8#022) != 0 )); then
            echo "ERROR: The Fluxo managed-install marker must be root-owned, regular, and not group/world writable."
            exit 1
        fi
        MANAGED_FLUXO_ACCOUNT=true
    fi

    if [ -e "$binary" ] || sudo test -e "$database_path" || sudo test -L "$database_path"; then
        artifacts_exist=true
    fi

    if [ ! -e "$service" ]; then
        if [ "$MANAGED_FLUXO_ACCOUNT" != true ] && [ "$artifacts_exist" = true ]; then
            echo "ERROR: Partial Fluxo artifacts exist without a managed marker or systemd service."
            echo "Refusing to adopt an installation whose ownership cannot be established."
            exit 1
        fi
    else
        service_exists=true
        if [ -L "$service" ] || [ ! -f "$service" ]; then
            echo "ERROR: The existing Fluxo systemd service must be a regular, non-symlink file."
            exit 1
        fi
        read -r service_owner service_mode < <(stat -c '%U:%G %a' "$service")
        if [ "$service_owner" != "root:root" ] || (( (8#$service_mode & 8#022) != 0 )); then
            echo "ERROR: The existing Fluxo service must be root-owned and not group/world writable."
            exit 1
        fi
        exec_start="$(sudo systemctl show fluxo.service --property=ExecStart --value 2>/dev/null || true)"
        if ! [[ "$exec_start" =~ (^|[[:space:];\{])path=/usr/local/bin/fluxo([[:space:];\}]|$) ]]; then
            echo "ERROR: The existing fluxo.service does not effectively start /usr/local/bin/fluxo."
            exit 1
        fi
    fi

    if [ -e "$binary" ] || [ -L "$binary" ]; then
        if [ ! -x "$binary" ] || [ -L "$binary" ] || [ ! -f "$binary" ]; then
            echo "ERROR: /usr/local/bin/fluxo must be an executable, non-symlink regular file."
            exit 1
        fi
        read -r binary_owner binary_mode < <(stat -c '%U:%G %a' "$binary")
        if [ "$binary_owner" != "root:root" ] || (( (8#$binary_mode & 8#022) != 0 )); then
            echo "ERROR: /usr/local/bin/fluxo must be root-owned and not group/world writable."
            exit 1
        fi
        version_output="$(sudo "$binary" --version 2>/dev/null || true)"
        if [[ "$version_output" != "fluxo version "* ]]; then
            echo "ERROR: /usr/local/bin/fluxo does not identify itself as a Fluxo binary."
            exit 1
        fi
    else
        if [ "$service_exists" = true ] && [ "$MANAGED_FLUXO_ACCOUNT" != true ]; then
            echo "ERROR: A legacy Fluxo service exists without a valid /usr/local/bin/fluxo binary."
            exit 1
        fi
    fi

    if sudo test -e "$database_path"; then
        if sudo test -L "$database_path" || ! sudo test -f "$database_path"; then
            echo "ERROR: The existing Fluxo database must be a regular, non-symlink file."
            exit 1
        fi
        read -r database_owner database_mode < <(sudo stat -c '%U:%G %a' "$database_path")
        if [ "$database_owner" != "root:root" ] || (( (8#$database_mode & 8#022) != 0 )); then
            echo "ERROR: The existing Fluxo database must be root-owned and not group/world writable."
            exit 1
        fi
        if ! sudo python3 -c 'import sqlite3, sys
db = sqlite3.connect("file:" + sys.argv[1] + "?mode=ro", uri=True, timeout=5)
if db.execute("PRAGMA quick_check").fetchone()[0] != "ok": raise SystemExit(1)
tables = {row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type = \"table\"")}
if not {"sites", "users"}.issubset(tables): raise SystemExit(1)' "$database_path"; then
            echo "ERROR: The existing Fluxo SQLite database failed integrity or schema validation."
            exit 1
        fi
    elif [ "$service_exists" = true ] || { [ "$MANAGED_FLUXO_ACCOUNT" = true ] && [ "$artifacts_exist" = true ]; }; then
        echo "ERROR: Existing Fluxo installation artifacts were found without /var/lib/fluxo/fluxo.db."
        echo "Refusing to treat a database-less installation as an upgrade because new recovery credentials could be suppressed."
        exit 1
    fi

    if [ "$service_exists" = true ] && [ "$MANAGED_FLUXO_ACCOUNT" != true ]; then
        echo "Validated a legacy Fluxo installation; it will receive the managed-install marker during upgrade."
    fi
    if [ "$service_exists" = true ] || { [ "$MANAGED_FLUXO_ACCOUNT" = true ] && [ "$artifacts_exist" = true ]; }; then
        EXISTING_INSTALL=true
    fi
}

validate_management_cidr() {
    if [ -z "$MANAGEMENT_CIDR" ]; then
        return
    fi
    if ! MANAGEMENT_CIDR="$(python3 -c 'import ipaddress, sys; print(ipaddress.ip_network(sys.argv[1], strict=False))' "$MANAGEMENT_CIDR" 2>/dev/null)"; then
        echo "ERROR: --management-cidr must be an IPv4 or IPv6 CIDR, for example 203.0.113.4/32."
        exit 1
    fi
}

preflight_host() {
    local required command_name os_id os_version needs_tty=false account_service ssh_effective
    local ufw_status ufw_config_enabled=false
    required=(sudo curl awk grep sed sha256sum mktemp install getent stat systemctl python3 tar dpkg readlink)
    for command_name in "${required[@]}"; do
        if ! command -v "$command_name" >/dev/null 2>&1; then
            echo "ERROR: Required command '$command_name' is unavailable."
            exit 1
        fi
    done
    sudo -v

    if [ ! -r /etc/os-release ]; then
        echo "ERROR: Unable to identify this operating system. Fluxo currently supports Ubuntu 22.04 or newer."
        exit 1
    fi
    os_id="$(. /etc/os-release && printf '%s' "${ID:-}")"
    os_version="$(. /etc/os-release && printf '%s' "${VERSION_ID:-}")"
    if [ "$os_id" != "ubuntu" ] || ! dpkg --compare-versions "$os_version" ge 22.04; then
        echo "ERROR: Unsupported operating system: ${os_id:-unknown} ${os_version:-unknown}."
        echo "Fluxo currently supports Ubuntu 22.04 or newer."
        exit 1
    fi
    if [ ! -d /run/systemd/system ]; then
        echo "ERROR: Fluxo requires a systemd-based server."
        exit 1
    fi

    if [ "$(detect_arch)" = "unsupported" ]; then
        echo "ERROR: Unsupported architecture: $(uname -m). Fluxo supports amd64 and arm64."
        exit 1
    fi

    validate_existing_fluxo_installation
    validate_existing_fluxo_account
    validate_management_cidr

    if local_database_server_exists mysql; then
        MYSQL_EXISTS=true
    fi
    if local_database_server_exists postgres; then
        POSTGRES_EXISTS=true
    fi
    if command -v redis-server >/dev/null 2>&1; then
        REDIS_EXISTS=true
    fi

    if [ -z "$INSTALL_NODE" ] && ! command -v node >/dev/null 2>&1; then
        needs_tty=true
    fi
    if [ -z "$INSTALL_MYSQL" ] && [ "$MYSQL_EXISTS" != true ] && [ "$POSTGRES_EXISTS" != true ]; then
        needs_tty=true
    fi
    if [ -z "$INSTALL_REDIS" ] && [ "$REDIS_EXISTS" != true ]; then
        needs_tty=true
    fi
    if [ "$needs_tty" = true ] && ! ( : </dev/tty >/dev/tty ) 2>/dev/null; then
        echo "ERROR: This installation needs interactive choices, but no terminal is available."
        echo "Use --db-engine, --redis/--no-redis, and --node/--no-node for unattended installation."
        exit 1
    fi

    if [ -z "$HARDEN_SSH" ]; then
        if [ -f /etc/ssh/sshd_config.d/00-fluxo-hardening.conf ]; then
            HARDEN_SSH=true
        else
            HARDEN_SSH=false
        fi
    fi
    if [ "$HARDEN_SSH" = true ]; then
        if ! command -v sshd >/dev/null 2>&1 || ! command -v ssh-keygen >/dev/null 2>&1; then
            echo "ERROR: --harden-ssh requires OpenSSH server tools."
            exit 1
        fi
        if [ "$SSH_HARDENING_EXPLICIT" = true ] &&
           { ! sudo test -s /root/.ssh/authorized_keys || ! sudo ssh-keygen -l -f /root/.ssh/authorized_keys >/dev/null 2>&1; }; then
            echo "ERROR: --harden-ssh requires at least one valid root authorized key."
            echo "Confirm key-based root access in a second session before enabling this option."
            exit 1
        fi
    fi

    if command -v sshd >/dev/null 2>&1; then
        if ! ssh_effective="$(sudo sshd -T -C user=root,host=localhost,addr=127.0.0.1 2>&1)"; then
            echo "ERROR: Unable to evaluate the current SSH server configuration:"
            printf '%s\n' "$ssh_effective"
            exit 1
        fi
        SSH_PORTS="$(printf '%s\n' "$ssh_effective" | awk '$1 == "port" { print $2 }')"
    fi
    if [ -z "$SSH_PORTS" ]; then
        echo "ERROR: Unable to determine the effective SSH port with sshd -T."
        exit 1
    fi
    while IFS= read -r account_service; do
        if ! [[ "$account_service" =~ ^[0-9]+$ ]] || [ "$account_service" -lt 1 ] || [ "$account_service" -gt 65535 ]; then
            echo "ERROR: sshd reported an invalid port: $account_service"
            exit 1
        fi
    done <<< "$SSH_PORTS"

    if command -v ufw >/dev/null 2>&1; then
        if ! ufw_status="$(sudo ufw status 2>&1)"; then
            echo "ERROR: Unable to determine UFW's effective state. No firewall changes were made."
            printf '%s\n' "$ufw_status"
            exit 1
        fi
        if sudo grep -Eq '^ENABLED=yes$' /etc/ufw/ufw.conf 2>/dev/null; then
            ufw_config_enabled=true
        fi
        case "$ufw_status" in
            "Status: active"*)
                if [ "$ufw_config_enabled" != true ]; then
                    echo "ERROR: UFW reports active, but /etc/ufw/ufw.conf does not."
                    echo "Resolve the inconsistent firewall state before installing Fluxo."
                    exit 1
                fi
                UFW_WAS_ACTIVE=true
                ;;
            "Status: inactive"*)
                if [ "$ufw_config_enabled" = true ]; then
                    echo "ERROR: UFW reports inactive while /etc/ufw/ufw.conf reports enabled."
                    echo "Resolve the inconsistent firewall state before installing Fluxo."
                    exit 1
                fi
                ;;
            *)
                echo "ERROR: UFW returned an unrecognized effective state. No firewall changes were made."
                printf '%s\n' "$ufw_status"
                exit 1
                ;;
        esac
    elif sudo test -e /etc/ufw/ufw.conf; then
        echo "ERROR: UFW configuration exists, but the ufw command is unavailable."
        echo "Repair or remove the partial UFW installation before installing Fluxo."
        exit 1
    fi

    if [ "$UFW_WAS_ACTIVE" = true ] && [ -n "$MANAGEMENT_CIDR" ]; then
        echo "ERROR: --management-cidr cannot safely replace rules in an existing active UFW policy."
        echo "Restrict port 9595 manually, then rerun without --management-cidr."
        exit 1
    fi
}

select_optional_components() {
    echo ""
    echo "========================================="
    echo "  NODE.JS TOOLCHAIN"
    echo "========================================="
    if [ -n "$INSTALL_NODE" ]; then
        if [ "$INSTALL_NODE" = true ]; then
            echo "The complete Node.js toolchain will be installed."
        else
            echo "Skipping the Node.js toolchain (--no-node)."
        fi
    elif command -v node >/dev/null 2>&1; then
        INSTALL_NODE=true
        echo "Existing Node.js detected ($(node --version)); Fluxo will complete and verify the toolchain."
    else
        read -r -p "Install the Node.js toolchain (Node.js, npm, pnpm, Yarn, Corepack, and Bun)? It can also be installed later via Runtime > Node.js. (y/n): " INSTALL_NODE < /dev/tty
        if [ "$INSTALL_NODE" = "y" ] || [ "$INSTALL_NODE" = "Y" ]; then
            INSTALL_NODE=true
        else
            INSTALL_NODE=false
        fi
    fi

    echo ""
    echo "========================================="
    echo "  DATABASE ENGINE SELECTION"
    echo "========================================="
    if [ -n "$INSTALL_MYSQL" ]; then
        echo "Database engine selection (from flags):"
        [ "$INSTALL_MYSQL" = true ] && echo " - MySQL / MariaDB"
        [ "$INSTALL_POSTGRES" = true ] && echo " - PostgreSQL"
        [ "$INSTALL_MYSQL" != true ] && [ "$INSTALL_POSTGRES" != true ] && echo " - None"
        if [ "$INSTALL_MYSQL" = true ] && [ "$MYSQL_EXISTS" = true ]; then
            echo "Note: MySQL / MariaDB is already installed. Skipping its installation."
            INSTALL_MYSQL=false
        fi
        if [ "$INSTALL_POSTGRES" = true ] && [ "$POSTGRES_EXISTS" = true ]; then
            echo "Note: PostgreSQL is already installed. Skipping its installation."
            INSTALL_POSTGRES=false
        fi
    elif [ "$MYSQL_EXISTS" = true ] || [ "$POSTGRES_EXISTS" = true ]; then
        echo "Existing local database server(s) detected:"
        [ "$MYSQL_EXISTS" = true ] && echo " - MySQL / MariaDB"
        [ "$POSTGRES_EXISTS" = true ] && echo " - PostgreSQL"
        INSTALL_MYSQL=false
        INSTALL_POSTGRES=false
    else
        echo "Which database engine(s) do you want to install?"
        echo "  1) MySQL (MariaDB)"
        echo "  2) PostgreSQL"
        echo "  3) Both MySQL and PostgreSQL"
        echo "  4) None (install later via GUI)"
        while true; do
            read -r -p "Choose an option (1-4): " DB_REPLY < /dev/tty
            case "$DB_REPLY" in
                1) INSTALL_MYSQL=true; INSTALL_POSTGRES=false; break ;;
                2) INSTALL_MYSQL=false; INSTALL_POSTGRES=true; break ;;
                3) INSTALL_MYSQL=true; INSTALL_POSTGRES=true; break ;;
                4) INSTALL_MYSQL=false; INSTALL_POSTGRES=false; break ;;
                *) echo "Invalid option. Please choose 1-4." ;;
            esac
        done
    fi

    echo ""
    echo "========================================="
    echo "  REDIS"
    echo "========================================="
    if [ "$REDIS_EXISTS" = true ]; then
        echo "Redis is already installed. Skipping installation."
        INSTALL_REDIS=false
    elif [ -n "$INSTALL_REDIS" ]; then
        if [ "$INSTALL_REDIS" = true ]; then
            echo "Redis will be installed."
        else
            echo "Skipping Redis (--no-redis)."
        fi
    else
        read -r -p "Install Redis? It can also be installed later via Runtime > Databases. (y/n): " INSTALL_REDIS < /dev/tty
        if [ "$INSTALL_REDIS" = "y" ] || [ "$INSTALL_REDIS" = "Y" ]; then
            INSTALL_REDIS=true
        else
            INSTALL_REDIS=false
        fi
    fi
    echo ""
}
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
        --harden-ssh)    HARDEN_SSH=true;  SSH_HARDENING_EXPLICIT=true ;;
        --no-harden-ssh) HARDEN_SSH=false; SSH_HARDENING_EXPLICIT=true ;;
        --management-cidr=*)
            MANAGEMENT_CIDR="${1#*=}"
            ;;
        --local-binary=*)
            LOCAL_BINARY="${1#*=}"
            if [ -z "$LOCAL_BINARY" ]; then
                echo "Error: --local-binary requires a path."
                exit 1
            fi
            ;;
        --skip-release-attestation)
            SKIP_RELEASE_ATTESTATION=true
            ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --db-engine=mysql|postgres|both|none"
            echo "  --redis / --no-redis"
            echo "  --node  / --no-node  Install or skip the complete Node.js toolchain"
            echo "  --harden-ssh / --no-harden-ssh"
            echo "  --management-cidr=CIDR  Restrict a newly configured port 9595 rule"
            echo "  --local-binary=PATH  Explicitly install a trusted local build"
            echo "  --skip-release-attestation  Trust a custom binary URL without provenance verification"
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
    latest_release_url="$(curl -fsSIL --retry 3 --retry-all-errors --retry-delay 2 \
        --connect-timeout 10 --max-time 30 \
        -o /dev/null -w '%{url_effective}' \
        "https://github.com/${FLUXO_REPO}/releases/latest" 2>/dev/null || true)"
    if [[ "$latest_release_url" =~ /releases/tag/([^/?#]+)$ ]]; then
        FLUXO_VERSION="${BASH_REMATCH[1]}"
        INSTALL_VERSION_LABEL="$FLUXO_VERSION"
    fi
fi

verify_release_attestation() {
    local release_version release_core gh_arch gh_sha gh_asset gh_url bundle_url actual gh_binary

    if [ -n "$LOCAL_BINARY" ]; then
        echo "Skipping release attestation for the explicitly trusted local binary."
        return
    fi
    if [ -n "${FLUXO_BINARY_URL:-}" ]; then
        if [ "$SKIP_RELEASE_ATTESTATION" != true ]; then
            echo "ERROR: A custom FLUXO_BINARY_URL has no Fluxo release provenance to verify."
            echo "Use a published release, --local-binary, or explicitly accept this risk with --skip-release-attestation."
            exit 1
        fi
        echo "WARNING: Release attestation verification was explicitly disabled for the custom binary URL."
        return
    fi
    if [ "$SKIP_RELEASE_ATTESTATION" = true ]; then
        echo "ERROR: --skip-release-attestation is only accepted with FLUXO_BINARY_URL."
        exit 1
    fi
    if [ "$FLUXO_VERSION" = "latest" ]; then
        echo "ERROR: Could not resolve the latest Fluxo release tag required for provenance verification."
        exit 1
    fi

    release_version="${FLUXO_VERSION#v}"
    if ! [[ "$release_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$ ]]; then
        echo "ERROR: Invalid Fluxo release version for provenance verification: $FLUXO_VERSION"
        exit 1
    fi
    release_core="${release_version%%-*}"
    if ! dpkg --compare-versions "$release_core" ge "$MIN_ATTESTED_FLUXO_VERSION"; then
        echo "WARNING: Fluxo $FLUXO_VERSION predates enforced release attestations."
        echo "Its release checksum was verified, but cryptographic build provenance is unavailable."
        return
    fi

    case "$(detect_arch)" in
        amd64)
            gh_arch="amd64"
            gh_sha="$ATTESTATION_GH_AMD64_SHA256"
            ;;
        arm64)
            gh_arch="arm64"
            gh_sha="$ATTESTATION_GH_ARM64_SHA256"
            ;;
        *)
            echo "ERROR: Unsupported architecture for release attestation verification."
            exit 1
            ;;
    esac

    ATTESTATION_TEMP_DIR="$(mktemp -d)"
    gh_asset="gh_${ATTESTATION_GH_VERSION}_linux_${gh_arch}.tar.gz"
    gh_url="https://github.com/cli/cli/releases/download/v${ATTESTATION_GH_VERSION}/${gh_asset}"
    bundle_url="https://github.com/${FLUXO_REPO}/releases/download/${FLUXO_VERSION}/fluxo-release-attestation.json"
    echo "Verifying cryptographic build provenance for Fluxo $FLUXO_VERSION..."
    curl_fetch "$ATTESTATION_TEMP_DIR/$gh_asset" "$gh_url" 120
    curl_fetch "$ATTESTATION_TEMP_DIR/fluxo-release-attestation.json" "$bundle_url" 60
    actual="$(sha256sum "$ATTESTATION_TEMP_DIR/$gh_asset" | awk '{print $1}')"
    if [ "$actual" != "$gh_sha" ]; then
        echo "ERROR: The pinned GitHub attestation verifier checksum did not match."
        echo "  Expected: $gh_sha"
        echo "  Got:      $actual"
        exit 1
    fi
    tar -xzf "$ATTESTATION_TEMP_DIR/$gh_asset" -C "$ATTESTATION_TEMP_DIR"
    gh_binary="$ATTESTATION_TEMP_DIR/gh_${ATTESTATION_GH_VERSION}_linux_${gh_arch}/bin/gh"
    if [ ! -x "$gh_binary" ]; then
        echo "ERROR: The pinned GitHub attestation verifier archive was incomplete."
        exit 1
    fi
    if ! GH_CONFIG_DIR="$ATTESTATION_TEMP_DIR/config" GH_PROMPT_DISABLED=1 "$gh_binary" attestation verify \
        "$PREPARED_BINARY" \
        --repo "$FLUXO_REPO" \
        --bundle "$ATTESTATION_TEMP_DIR/fluxo-release-attestation.json" \
        --signer-workflow "$FLUXO_REPO/.github/workflows/release.yml" \
        --source-ref "refs/tags/$FLUXO_VERSION" \
        --deny-self-hosted-runners >/dev/null; then
        echo "ERROR: Fluxo release provenance verification failed. The candidate was not executed."
        exit 1
    fi
    echo "Fluxo release provenance verified against GitHub's signed attestation and transparency log."
    rm -rf -- "$ATTESTATION_TEMP_DIR"
    ATTESTATION_TEMP_DIR=""
}

prepare_fluxo_binary() {
    local arch download_url checksum_url actual expected
    PREPARED_BINARY="$(mktemp)"

    if [ -n "$LOCAL_BINARY" ]; then
        if [ ! -f "$LOCAL_BINARY" ] || [ -L "$LOCAL_BINARY" ]; then
            echo "ERROR: --local-binary must point to a regular, non-symlink file."
            exit 1
        fi
        echo "Preparing explicitly selected local binary: $LOCAL_BINARY"
        cp -- "$LOCAL_BINARY" "$PREPARED_BINARY"
        if [ -n "${FLUXO_LOCAL_BINARY_SHA256:-}" ]; then
            actual="$(sha256sum "$PREPARED_BINARY" | awk '{print $1}')"
            expected="$(printf '%s' "$FLUXO_LOCAL_BINARY_SHA256" | tr '[:upper:]' '[:lower:]')"
            if ! [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || [ "$expected" != "$actual" ]; then
                echo "ERROR: Local binary checksum verification FAILED!"
                exit 1
            fi
            echo "Local binary checksum verified OK."
        fi
    elif [ -n "${FLUXO_BINARY_URL:-}" ]; then
        if [ -z "${FLUXO_BINARY_SHA256_URL:-}" ]; then
            echo "ERROR: FLUXO_BINARY_SHA256_URL is required when FLUXO_BINARY_URL is set."
            exit 1
        fi
        echo "Downloading the candidate binary from FLUXO_BINARY_URL..."
        curl_fetch "$PREPARED_BINARY" "$FLUXO_BINARY_URL"
        PREPARED_CHECKSUM="$(mktemp)"
        curl_fetch "$PREPARED_CHECKSUM" "$FLUXO_BINARY_SHA256_URL" 60
        verify_checksum "$PREPARED_BINARY" "$PREPARED_CHECKSUM"
    else
        arch="$(detect_arch)"
        if [ "$FLUXO_VERSION" = "latest" ]; then
            download_url="https://github.com/${FLUXO_REPO}/releases/latest/download/fluxo-linux-${arch}"
            checksum_url="https://github.com/${FLUXO_REPO}/releases/latest/download/SHA256SUMS"
        else
            download_url="https://github.com/${FLUXO_REPO}/releases/download/${FLUXO_VERSION}/fluxo-linux-${arch}"
            checksum_url="https://github.com/${FLUXO_REPO}/releases/download/${FLUXO_VERSION}/SHA256SUMS"
        fi
        echo "Downloading and validating Fluxo ${FLUXO_VERSION} (linux-${arch})..."
        curl_fetch "$PREPARED_BINARY" "$download_url"
        PREPARED_CHECKSUM="$(mktemp)"
        curl_fetch "$PREPARED_CHECKSUM" "$checksum_url" 60
        verify_checksum "$PREPARED_BINARY" "$PREPARED_CHECKSUM" "fluxo-linux-${arch}"
    fi

    chmod 0755 "$PREPARED_BINARY"
}

load_release_installer_tool_versions() {
    local output status embedded_composer embedded_composer_sha256 embedded_wp_cli
    if output="$("$PREPARED_BINARY" --installer-tool-versions 2>&1)"; then
        embedded_composer="$(printf '%s\n' "$output" | awk -F= '$1 == "composer" { print $2; exit }')"
        embedded_composer_sha256="$(printf '%s\n' "$output" | awk -F= '$1 == "composer-sha256" { print $2; exit }')"
        embedded_wp_cli="$(printf '%s\n' "$output" | awk -F= '$1 == "wp-cli" { print $2; exit }')"
        if [ -z "$embedded_composer_sha256" ] && [ "$embedded_composer" = "$DEFAULT_COMPOSER_VERSION" ]; then
            embedded_composer_sha256="$DEFAULT_COMPOSER_SHA256"
            echo "Candidate predates embedded Composer hashes; using the installer's exact compatibility hash."
        fi
        if ! [[ "$embedded_composer" =~ ^2\.[0-9]+\.[0-9]+$ ]] ||
           ! [[ "$embedded_composer_sha256" =~ ^[0-9a-f]{64}$ ]] ||
           ! [[ "$embedded_wp_cli" =~ ^2\.[0-9]+\.[0-9]+$ ]]; then
            echo "ERROR: The candidate returned invalid or unsupported installer tool versions."
            exit 1
        fi
        COMPOSER_VERSION="$embedded_composer"
        COMPOSER_SHA256="$embedded_composer_sha256"
        WP_CLI_VERSION="$embedded_wp_cli"
        echo "Using release-authenticated Composer ${COMPOSER_VERSION} and WP-CLI ${WP_CLI_VERSION} baselines."
        return
    else
        status=$?
    fi
    if [ "$status" -ne 2 ]; then
        echo "ERROR: The candidate could not report valid installer tool metadata:"
        echo "$output"
        exit 1
    fi

    # Compatibility for Fluxo binaries published before installer-tool metadata
    # was embedded. These remain exact versions rather than moving latest URLs.
    echo "Candidate has no embedded installer tool metadata; using pinned compatibility versions."
    echo "Composer ${COMPOSER_VERSION}; WP-CLI ${WP_CLI_VERSION}."
}

preflight_host
select_optional_components
prepare_fluxo_binary
verify_release_attestation
load_release_installer_tool_versions

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
sudo env DEBIAN_FRONTEND=noninteractive apt-get update
sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y software-properties-common
sudo add-apt-repository -y ppa:ondrej/php
sudo env DEBIAN_FRONTEND=noninteractive apt-get update

echo "Installing Nginx, PHP 8.4 FPM, Certbot, UFW, and Fail2Ban..."
sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y nginx php8.4-fpm php8.4-cli php8.4-mysql php8.4-pgsql php8.4-sqlite3 php8.4-curl php8.4-mbstring php8.4-xml php8.4-gd php8.4-zip php8.4-bcmath php8.4-intl php8.4-redis certbot ufw fail2ban git curl gnupg ca-certificates util-linux

echo "Setting PHP 8.4 as the default CLI version..."
sudo update-alternatives --set php /usr/bin/php8.4

install_composer() (
    set -Eeuo pipefail
    local temp_dir version_output actual_sha256
    temp_dir="$(mktemp -d)"
    trap 'rm -rf "$temp_dir"' EXIT

    curl_fetch "$temp_dir/composer.phar" "https://getcomposer.org/download/${COMPOSER_VERSION}/composer.phar" 120
    actual_sha256="$(sha256sum "$temp_dir/composer.phar" | awk '{print $1}')"
    if [ "$actual_sha256" != "$COMPOSER_SHA256" ]; then
        echo "ERROR: Composer ${COMPOSER_VERSION} did not match the SHA-256 authenticated by the Fluxo release."
        exit 1
    fi
    version_output="$(COMPOSER_ALLOW_SUPERUSER=1 php "$temp_dir/composer.phar" --version --no-ansi 2>&1)"
    if [[ "$version_output" != "Composer version ${COMPOSER_VERSION} "* ]]; then
        echo "ERROR: Composer ${COMPOSER_VERSION} was requested, but the downloaded PHAR reported:"
        echo "$version_output"
        exit 1
    fi
    atomic_install_root_locked "$temp_dir/composer.phar" /usr/local/bin/composer 0755 /run/lock/fluxo-composer.lock
)

install_wp_cli() (
    set -Eeuo pipefail
    local temp_dir fingerprint version_output
    temp_dir="$(mktemp -d)"
    trap 'rm -rf "$temp_dir"' EXIT

    curl_fetch "$temp_dir/wp-cli.phar" "https://github.com/wp-cli/wp-cli/releases/download/v${WP_CLI_VERSION}/wp-cli-${WP_CLI_VERSION}.phar"
    curl_fetch "$temp_dir/wp-cli.phar.asc" "https://github.com/wp-cli/wp-cli/releases/download/v${WP_CLI_VERSION}/wp-cli-${WP_CLI_VERSION}.phar.asc" 60
    curl_fetch "$temp_dir/wp-cli.pgp" https://raw.githubusercontent.com/wp-cli/builds/gh-pages/wp-cli.pgp 60

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
    version_output="$(php "$temp_dir/wp-cli.phar" --allow-root --version 2>&1)"
    if [ "$version_output" != "WP-CLI ${WP_CLI_VERSION}" ]; then
        echo "ERROR: WP-CLI ${WP_CLI_VERSION} was requested, but the downloaded PHAR reported:"
        echo "$version_output"
        exit 1
    fi
    atomic_install_root "$temp_dir/wp-cli.phar" /usr/local/bin/wp 0755
)

# 0.5. Initialize Firewall Safely
echo "Initializing UFW Firewall safely..."
current_ufw_status="$(sudo ufw status 2>&1)" || {
    echo "ERROR: UFW state could not be revalidated immediately before firewall setup."
    printf '%s\n' "$current_ufw_status"
    exit 1
}
current_ufw_config_enabled=false
if sudo grep -Eq '^ENABLED=yes$' /etc/ufw/ufw.conf 2>/dev/null; then
    current_ufw_config_enabled=true
fi
case "$current_ufw_status" in
    "Status: active"*) current_ufw_active=true ;;
    "Status: inactive"*) current_ufw_active=false ;;
    *)
        echo "ERROR: UFW returned an unrecognized state immediately before firewall setup."
        printf '%s\n' "$current_ufw_status"
        exit 1
        ;;
esac
if [ "$current_ufw_active" != "$current_ufw_config_enabled" ] ||
   [ "$current_ufw_active" != "$UFW_WAS_ACTIVE" ]; then
    echo "ERROR: UFW state changed or became inconsistent after preflight."
    echo "No Fluxo firewall rules were added. Review UFW and rerun the installer."
    exit 1
fi
if [ "$UFW_WAS_ACTIVE" = true ]; then
    echo "UFW was already active. Preserving its existing rules without adding broader access."
    echo "Confirm that the effective SSH port(s), 80/tcp, 443/tcp, and 9595/tcp are allowed as intended."
elif [ "$EXISTING_INSTALL" = true ]; then
    echo "UFW was inactive before this upgrade. Preserving that state and leaving its rules unchanged."
else
    while IFS= read -r ssh_port; do
        sudo ufw allow "${ssh_port}/tcp"
    done <<< "$SSH_PORTS"
    sudo ufw allow 80/tcp
    sudo ufw allow 443/tcp
    if [ -n "$MANAGEMENT_CIDR" ]; then
        sudo ufw allow from "$MANAGEMENT_CIDR" to any port 9595 proto tcp
    else
        sudo ufw allow 9595/tcp
    fi
    sudo ufw --force enable
    enabled_ufw_status="$(sudo ufw status 2>&1)" || {
        echo "ERROR: UFW could not be verified after enabling it."
        printf '%s\n' "$enabled_ufw_status"
        exit 1
    }
    if [[ "$enabled_ufw_status" != "Status: active"* ]] ||
       ! sudo grep -Eq '^ENABLED=yes$' /etc/ufw/ufw.conf 2>/dev/null; then
        echo "ERROR: UFW did not reach a consistent active state after setup."
        exit 1
    fi
    UFW_RULES_MANAGED_BY_INSTALLER=true
fi

write_installer_firewall_manifest() {
    local manifest_temp
    if [ "$UFW_RULES_MANAGED_BY_INSTALLER" != true ]; then
        return
    fi
    manifest_temp="$(mktemp)"
    if ! python3 -c 'import json, sys
ports = [line.strip() for line in sys.argv[1].splitlines() if line.strip()]
source = sys.argv[2] or "Any"
rules = [{"name": "SSH", "rule_type": "allow", "port": port + "/tcp", "from_ip": "Any"} for port in ports]
rules.extend([
    {"name": "HTTP", "rule_type": "allow", "port": "80/tcp", "from_ip": "Any"},
    {"name": "HTTPS", "rule_type": "allow", "port": "443/tcp", "from_ip": "Any"},
    {"name": "Fluxo Dashboard", "rule_type": "allow", "port": "9595/tcp", "from_ip": source},
])
json.dump({"version": 1, "rules": rules}, sys.stdout, separators=(",", ":"))
sys.stdout.write("\n")' "$SSH_PORTS" "$MANAGEMENT_CIDR" > "$manifest_temp"; then
        rm -f -- "$manifest_temp"
        echo "ERROR: Could not serialize the installer-managed firewall rules."
        exit 1
    fi
    if ! atomic_install_root "$manifest_temp" /var/lib/fluxo/installer-firewall-rules.json 0600; then
        rm -f -- "$manifest_temp"
        echo "ERROR: Could not persist the installer-managed firewall rule manifest."
        exit 1
    fi
    rm -f -- "$manifest_temp"
}

write_installer_firewall_manifest

# Optional component choices were collected before the host was modified.
if [ "$INSTALL_MYSQL" = true ]; then
    echo "Installing MariaDB (MySQL)..."
    sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y mariadb-server
    echo "MariaDB installed. Fluxo will create and securely store its administrator credentials on first start."
    echo ""
fi

if [ "$INSTALL_POSTGRES" = true ]; then
    echo "Installing PostgreSQL..."
    sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y postgresql
    echo "PostgreSQL installed. Fluxo will create and securely store its administrator credentials on first start."
    echo ""
fi

# 0.6.5 Redis
if [ "$INSTALL_REDIS" = true ]; then
    echo "Installing Redis..."
    sudo env DEBIAN_FRONTEND=noninteractive apt-get install -y redis-server
    echo "Redis installed."
    echo ""
fi

# 0.7. Provision Fluxo System User
echo "Creating fluxo system user..."
if ! id -u fluxo >/dev/null 2>&1; then
    sudo useradd fluxo -m -s /bin/bash -G www-data
    sudo install -d -m 0700 -o root -g root /var/lib/fluxo
    printf '%s\n' 'Managed by the Fluxo installer.' | sudo tee /var/lib/fluxo/.managed-system-user >/dev/null
    sudo chmod 0600 /var/lib/fluxo/.managed-system-user
fi
sudo usermod -aG www-data fluxo
sudo chmod 755 /home/fluxo
sudo mkdir -p /home/fluxo/.ssh
sudo chmod 700 /home/fluxo/.ssh
sudo touch /home/fluxo/.ssh/authorized_keys
sudo chmod 600 /home/fluxo/.ssh/authorized_keys
sudo chown -R fluxo:fluxo /home/fluxo/.ssh
sudo chown fluxo:fluxo /home/fluxo
sudo install -d -m 0700 -o root -g root /var/lib/fluxo
printf '%s\n' 'Managed by the Fluxo installer.' | sudo tee /var/lib/fluxo/.managed-system-user >/dev/null
sudo chmod 0600 /var/lib/fluxo/.managed-system-user

prepare_upgrade_config_snapshot

# 0.8. Configure Sudoers Rules for Fluxo User
echo "Configuring fluxo sudoers rules..."
sudo usermod -aG sudo fluxo
# sudo group membership is intentional — matches Forge/Coolify convention.
# Automated deployments may reload only explicitly supported PHP-FPM units.
# Interactive sudo use still requires the generated fluxo password.
SUDOERS_CANDIDATE="$(mktemp)"
SUDOERS_BACKUP="$(mktemp)"
SUDOERS_EXISTED=false
cat > "$SUDOERS_CANDIDATE" <<'EOF'
Cmnd_Alias FLUXO_PHP_RELOAD = /usr/bin/systemctl reload php7.4-fpm, /usr/bin/systemctl reload php8.0-fpm, /usr/bin/systemctl reload php8.1-fpm, /usr/bin/systemctl reload php8.2-fpm, /usr/bin/systemctl reload php8.3-fpm, /usr/bin/systemctl reload php8.4-fpm, /usr/bin/systemctl reload php8.5-fpm, /bin/systemctl reload php7.4-fpm, /bin/systemctl reload php8.0-fpm, /bin/systemctl reload php8.1-fpm, /bin/systemctl reload php8.2-fpm, /bin/systemctl reload php8.3-fpm, /bin/systemctl reload php8.4-fpm, /bin/systemctl reload php8.5-fpm
fluxo ALL=(root) NOPASSWD: FLUXO_PHP_RELOAD
EOF
chmod 0440 "$SUDOERS_CANDIDATE"
sudo visudo -cf "$SUDOERS_CANDIDATE" >/dev/null
if [ -f /etc/sudoers.d/fluxo ]; then
    sudo cp -- /etc/sudoers.d/fluxo "$SUDOERS_BACKUP"
    SUDOERS_EXISTED=true
fi
if ! atomic_install_root "$SUDOERS_CANDIDATE" /etc/sudoers.d/fluxo 0440; then
    echo "ERROR: Could not install Fluxo's sudoers policy. The previous policy is unchanged."
    rm -f -- "$SUDOERS_CANDIDATE" "$SUDOERS_BACKUP"
    exit 1
fi
if ! sudo visudo -cf /etc/sudoers >/dev/null; then
    echo "ERROR: The complete sudoers policy rejected Fluxo's rules; restoring the previous file."
    if [ "$SUDOERS_EXISTED" = true ]; then
        atomic_install_root "$SUDOERS_BACKUP" /etc/sudoers.d/fluxo 0440
    else
        sudo rm -f /etc/sudoers.d/fluxo
    fi
    rm -f -- "$SUDOERS_CANDIDATE" "$SUDOERS_BACKUP"
    exit 1
fi
rm -f -- "$SUDOERS_CANDIDATE" "$SUDOERS_BACKUP"
echo "Fluxo sudo access configured. The daemon will generate its password on first start."

# 0.9. Optionally disable SSH password authentication.
configure_ssh_hardening() {
    local target candidate validation_config backup effective had_previous=false
    target="/etc/ssh/sshd_config.d/00-fluxo-hardening.conf"
    if [ "$HARDEN_SSH" != true ]; then
        if [ -f "$target" ]; then
            echo "Preserving the existing Fluxo SSH hardening configuration (--no-harden-ssh)."
        else
            echo "SSH configuration was not changed. Re-run with --harden-ssh after verifying root key access."
        fi
        return
    fi

    echo "Validating SSH hardening before activation..."
    candidate="$(mktemp)"
    validation_config="$(mktemp)"
    backup="$(mktemp)"
    cat > "$candidate" <<'EOF'
# Managed by the Fluxo installer. This sorts before cloud-init SSH settings.
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin prohibit-password
EOF
    {
        printf 'Include %s\n' "$candidate"
        printf 'Include /etc/ssh/sshd_config\n'
    } > "$validation_config"

    if ! sudo sshd -t -f "$validation_config"; then
        rm -f -- "$candidate" "$validation_config" "$backup"
        echo "ERROR: The staged SSH configuration is invalid. No SSH settings were changed."
        exit 1
    fi
    effective="$(sudo sshd -T -f "$validation_config" -C user=root,host=localhost,addr=127.0.0.1)"
    if ! printf '%s\n' "$effective" | grep -qx 'passwordauthentication no' ||
       ! printf '%s\n' "$effective" | grep -qx 'kbdinteractiveauthentication no' ||
       ! printf '%s\n' "$effective" | grep -qx 'pubkeyauthentication yes' ||
       ! printf '%s\n' "$effective" | grep -Eq '^permitrootlogin (prohibit-password|without-password)$'; then
        rm -f -- "$candidate" "$validation_config" "$backup"
        echo "ERROR: The staged effective SSH policy would not provide the required key-only root access."
        exit 1
    fi

    sudo mkdir -p /etc/ssh/sshd_config.d
    if [ -f "$target" ]; then
        sudo cp -- "$target" "$backup"
        had_previous=true
    fi
    if ! atomic_install_root "$candidate" "$target" 0644; then
        rm -f -- "$candidate" "$validation_config" "$backup"
        echo "ERROR: Could not install the staged SSH policy. The previous configuration is unchanged."
        exit 1
    fi
    effective="$(sudo sshd -T -C user=root,host=localhost,addr=127.0.0.1 2>/dev/null || true)"
    if ! sudo sshd -t ||
       ! printf '%s\n' "$effective" | grep -qx 'passwordauthentication no' ||
       ! printf '%s\n' "$effective" | grep -qx 'kbdinteractiveauthentication no' ||
       ! printf '%s\n' "$effective" | grep -qx 'pubkeyauthentication yes' ||
       ! printf '%s\n' "$effective" | grep -Eq '^permitrootlogin (prohibit-password|without-password)$'; then
        echo "ERROR: SSH rejected the installed policy; restoring the previous configuration."
        if [ "$had_previous" = true ]; then
            atomic_install_root "$backup" "$target" 0644
        else
            sudo rm -f "$target"
        fi
        rm -f -- "$candidate" "$validation_config" "$backup"
        exit 1
    fi
    if ! sudo systemctl reload ssh 2>/dev/null && ! sudo systemctl reload sshd 2>/dev/null; then
        echo "ERROR: SSH could not reload; restoring the previous configuration."
        if [ "$had_previous" = true ]; then
            atomic_install_root "$backup" "$target" 0644
        else
            sudo rm -f "$target"
        fi
        sudo systemctl reload ssh 2>/dev/null || sudo systemctl reload sshd 2>/dev/null || true
        rm -f -- "$candidate" "$validation_config" "$backup"
        exit 1
    fi
    rm -f -- "$candidate" "$validation_config" "$backup"
    echo "SSH password authentication disabled after staged validation."
}

configure_ssh_hardening

# 1. Install Binary
echo "Installing binary to /usr/local/bin..."

prepare_upgrade_snapshot() {
    local timestamp suffix cron_file cron_files tool_path tool_name
    if [ "$EXISTING_INSTALL" != true ]; then
        return
    fi

    timestamp="$(date -u +%Y%m%dT%H%M%SZ)-$$"
    UPGRADE_BACKUP_DIR="/var/lib/fluxo/upgrades/$timestamp"
    echo "Creating a pre-upgrade rollback snapshot at $UPGRADE_BACKUP_DIR..."
    sudo install -d -m 0700 -o root -g root "$UPGRADE_BACKUP_DIR"
    if [ -x /usr/local/bin/fluxo ]; then
        sudo touch "$UPGRADE_BACKUP_DIR/binary-present"
        sudo cp --preserve=mode,ownership,timestamps -- /usr/local/bin/fluxo "$UPGRADE_BACKUP_DIR/fluxo"
    else
        sudo touch "$UPGRADE_BACKUP_DIR/binary-absent"
    fi
    if [ -f /etc/systemd/system/fluxo.service ]; then
        sudo cp --preserve=mode,ownership,timestamps -- /etc/systemd/system/fluxo.service "$UPGRADE_BACKUP_DIR/fluxo.service"
    fi
    if sudo systemctl is-active --quiet fluxo; then
        UPGRADE_WAS_ACTIVE=true
    fi
    if sudo systemctl is-enabled --quiet fluxo; then
        UPGRADE_WAS_ENABLED=true
    fi
    UPGRADE_SERVICE_STOPPED=true
    sudo systemctl stop fluxo

    for tool_name in composer wp; do
        tool_path="/usr/local/bin/$tool_name"
        if sudo test -L "$tool_path"; then
            echo "ERROR: Refusing to snapshot symlinked managed tool $tool_path."
            return 1
        elif sudo test -f "$tool_path"; then
            sudo touch "$UPGRADE_BACKUP_DIR/${tool_name}-present"
            if [ "$tool_name" = composer ]; then
                sudo /usr/bin/flock -w 120 /run/lock/fluxo-composer.lock \
                    cp --preserve=mode,ownership,timestamps -- "$tool_path" "$UPGRADE_BACKUP_DIR/$tool_name"
            else
                sudo cp --preserve=mode,ownership,timestamps -- "$tool_path" "$UPGRADE_BACKUP_DIR/$tool_name"
            fi
        else
            sudo touch "$UPGRADE_BACKUP_DIR/${tool_name}-absent"
        fi
    done
    sudo install -d -m 0700 -o root -g root "$UPGRADE_BACKUP_DIR/cron.d"
    if ! cron_files="$(sudo find /etc/cron.d -maxdepth 1 -type f -regextype posix-extended \
        -regex '.*/fluxo-cron-[0-9]+' -print)"; then
        echo "ERROR: Could not enumerate Fluxo cron files for the upgrade snapshot."
        return 1
    fi
    if [ -n "$cron_files" ]; then
        while IFS= read -r cron_file; do
            sudo cp --preserve=mode,ownership,timestamps -- "$cron_file" "$UPGRADE_BACKUP_DIR/cron.d/"
        done <<< "$cron_files"
    fi
    sudo touch "$UPGRADE_BACKUP_DIR/cron-files-snapshotted"

    if sudo test -f /var/lib/fluxo/fluxo.db; then
        sudo touch "$UPGRADE_BACKUP_DIR/database-present"
        for suffix in '' '-wal' '-shm'; do
            if sudo test -f "/var/lib/fluxo/fluxo.db${suffix}"; then
                sudo cp --reflink=auto --preserve=mode,ownership,timestamps -- \
                    "/var/lib/fluxo/fluxo.db${suffix}" "$UPGRADE_BACKUP_DIR/fluxo.db${suffix}"
            fi
        done
    else
        sudo touch "$UPGRADE_BACKUP_DIR/database-absent"
    fi
    UPGRADE_ROLLBACK_ARMED=true
}

rollback_upgrade() {
    local suffix staged_binary staged_service db_stage_prefix staged_db cron_file cron_files tool_name tool_path staged_tool
    local restore_failed=false
    echo "ERROR: Upgrade failed. Restoring the previous Fluxo release snapshot..."
    sudo systemctl stop fluxo >/dev/null 2>&1 || true
    if sudo test -f "$UPGRADE_BACKUP_DIR/binary-present" && sudo test -f "$UPGRADE_BACKUP_DIR/fluxo"; then
        staged_binary="/usr/local/bin/.fluxo-rollback.$$"
        if sudo install -m 0755 -o root -g root "$UPGRADE_BACKUP_DIR/fluxo" "$staged_binary" &&
           sudo mv -f "$staged_binary" /usr/local/bin/fluxo; then
            :
        else
            echo "ERROR: Could not restore the previous Fluxo binary."
            sudo rm -f -- "$staged_binary"
            restore_failed=true
        fi
    elif sudo test -f "$UPGRADE_BACKUP_DIR/binary-absent"; then
        if ! sudo rm -f -- /usr/local/bin/fluxo; then
            echo "ERROR: Could not remove the failed candidate binary."
            restore_failed=true
        fi
    fi
    if sudo test -f "$UPGRADE_BACKUP_DIR/fluxo.service"; then
        staged_service="/etc/systemd/system/.fluxo-rollback.$$.service"
        if sudo install -m 0644 -o root -g root "$UPGRADE_BACKUP_DIR/fluxo.service" "$staged_service" &&
           sudo mv -f "$staged_service" /etc/systemd/system/fluxo.service; then
            :
        else
            echo "ERROR: Could not restore the previous Fluxo service definition."
            sudo rm -f -- "$staged_service"
            restore_failed=true
        fi
    fi
    if sudo test -f "$UPGRADE_BACKUP_DIR/database-present"; then
        db_stage_prefix="/var/lib/fluxo/.fluxo-db-rollback.$$"
        for suffix in '' '-wal' '-shm'; do
            if sudo test -f "$UPGRADE_BACKUP_DIR/fluxo.db${suffix}"; then
                staged_db="${db_stage_prefix}${suffix}"
                if ! sudo cp --preserve=mode,ownership,timestamps -- \
                    "$UPGRADE_BACKUP_DIR/fluxo.db${suffix}" "$staged_db"; then
                    echo "ERROR: Could not stage the previous SQLite${suffix:- database} file."
                    restore_failed=true
                fi
            fi
        done
        if [ "$restore_failed" != true ]; then
            for suffix in '' '-wal' '-shm'; do
                if ! sudo rm -f -- "/var/lib/fluxo/fluxo.db${suffix}"; then
                    restore_failed=true
                    break
                fi
                if sudo test -f "$UPGRADE_BACKUP_DIR/fluxo.db${suffix}" &&
                   ! sudo mv -f "${db_stage_prefix}${suffix}" "/var/lib/fluxo/fluxo.db${suffix}"; then
                    restore_failed=true
                    break
                fi
            done
        fi
        sudo rm -f -- "${db_stage_prefix}" "${db_stage_prefix}-wal" "${db_stage_prefix}-shm"
    elif sudo test -f "$UPGRADE_BACKUP_DIR/database-absent"; then
        if ! sudo rm -f -- /var/lib/fluxo/fluxo.db /var/lib/fluxo/fluxo.db-wal /var/lib/fluxo/fluxo.db-shm; then
            echo "ERROR: Could not remove the database created by the failed candidate."
            restore_failed=true
        fi
    fi
    for tool_name in composer wp; do
        tool_path="/usr/local/bin/$tool_name"
        if sudo test -f "$UPGRADE_BACKUP_DIR/${tool_name}-present" && sudo test -f "$UPGRADE_BACKUP_DIR/$tool_name"; then
            staged_tool="/usr/local/bin/.${tool_name}-rollback.$$"
            if ! sudo install -m 0755 -o root -g root "$UPGRADE_BACKUP_DIR/$tool_name" "$staged_tool"; then
                echo "ERROR: Could not stage the previous $tool_name executable."
                sudo rm -f -- "$staged_tool"
                restore_failed=true
            elif [ "$tool_name" = composer ]; then
                if ! sudo /usr/bin/flock -w 120 /run/lock/fluxo-composer.lock mv -f "$staged_tool" "$tool_path"; then
                    echo "ERROR: Could not restore the previous Composer executable."
                    sudo rm -f -- "$staged_tool"
                    restore_failed=true
                fi
            elif ! sudo mv -f "$staged_tool" "$tool_path"; then
                echo "ERROR: Could not restore the previous $tool_name executable."
                sudo rm -f -- "$staged_tool"
                restore_failed=true
            fi
        elif sudo test -f "$UPGRADE_BACKUP_DIR/${tool_name}-absent"; then
            if [ "$tool_name" = composer ]; then
                sudo /usr/bin/flock -w 120 /run/lock/fluxo-composer.lock rm -f -- "$tool_path" || restore_failed=true
            else
                sudo rm -f -- "$tool_path" || restore_failed=true
            fi
        fi
    done
    if sudo test -f "$UPGRADE_BACKUP_DIR/cron-files-snapshotted"; then
        if cron_files="$(sudo find /etc/cron.d -maxdepth 1 -type f -regextype posix-extended \
            -regex '.*/fluxo-cron-[0-9]+' -print)"; then
            if [ -n "$cron_files" ]; then
                while IFS= read -r cron_file; do
                    if ! sudo rm -f -- "$cron_file"; then
                        echo "ERROR: Could not remove a candidate Fluxo cron file during rollback."
                        restore_failed=true
                    fi
                done <<< "$cron_files"
            fi
        else
            echo "ERROR: Could not enumerate candidate Fluxo cron files during rollback."
            restore_failed=true
        fi
        if cron_files="$(sudo find "$UPGRADE_BACKUP_DIR/cron.d" -maxdepth 1 -type f -regextype posix-extended \
            -regex '.*/fluxo-cron-[0-9]+' -print)"; then
            if [ -n "$cron_files" ]; then
                while IFS= read -r cron_file; do
                    if ! sudo install -m 0644 -o root -g root "$cron_file" "/etc/cron.d/$(basename "$cron_file")"; then
                        echo "ERROR: Could not restore a previous Fluxo cron file."
                        restore_failed=true
                    fi
                done <<< "$cron_files"
            fi
        else
            echo "ERROR: Could not enumerate the previous Fluxo cron snapshot."
            restore_failed=true
        fi
    fi
    if ! sudo systemctl daemon-reload; then
        restore_failed=true
    fi
    if [ "$restore_failed" != true ]; then
        if [ "$UPGRADE_WAS_ENABLED" = true ]; then
            sudo systemctl enable fluxo >/dev/null || restore_failed=true
        else
            sudo systemctl disable fluxo >/dev/null || restore_failed=true
        fi
    fi
    if [ "$UPGRADE_RESTART_ALLOWED" != true ]; then
        restore_failed=true
    fi
    if [ "$restore_failed" != true ] && [ "$UPGRADE_WAS_ACTIVE" = true ]; then
        sudo systemctl start fluxo || restore_failed=true
    fi
    UPGRADE_ROLLBACK_ARMED=false
    UPGRADE_SERVICE_STOPPED=false
    if [ "$restore_failed" = true ]; then
        echo "ERROR: Automatic rollback was incomplete. Fluxo was left stopped to protect its data."
        echo "The complete recovery snapshot remains at $UPGRADE_BACKUP_DIR."
        return 1
    fi
    echo "Previous Fluxo release restored. Snapshot retained at $UPGRADE_BACKUP_DIR."
}

node_toolchain_link_names() {
    printf '%s\n' node npm npx corepack pnpm pnpx yarn yarnpkg bun bunx
}

is_managed_node_link() {
    local path="$1" target
    sudo test -L "$path" || return 1
    target="$(sudo readlink "$path")" || return 1
    case "$target" in
        /opt/fluxo/node|/opt/fluxo/node/*|/opt/fluxo/node-toolchain|/opt/fluxo/node-toolchain/*) return 0 ;;
        *) return 1 ;;
    esac
}

snapshot_node_path() {
    local source="$1" label="$2"
    if sudo test -e "$source" || sudo test -L "$source"; then
        sudo touch "$NODE_ROLLBACK_DIR/$label.present"
        sudo cp -a --reflink=auto -- "$source" "$NODE_ROLLBACK_DIR/$label"
    else
        sudo touch "$NODE_ROLLBACK_DIR/$label.absent"
    fi
}

prepare_node_toolchain_snapshot() {
    local timestamp name path
    if [ "$INSTALL_NODE" != true ]; then
        return
    fi
    timestamp="$(date -u +%Y%m%dT%H%M%SZ)-$$"
    NODE_ROLLBACK_DIR="/var/lib/fluxo/node-rollbacks/$timestamp"
    echo "Creating an independent Node.js toolchain rollback snapshot..."
    if ! sudo install -d -m 0700 -o root -g root "$NODE_ROLLBACK_DIR/links" ||
       ! snapshot_node_path /opt/fluxo/node node ||
       ! snapshot_node_path /opt/fluxo/node-toolchain node-toolchain ||
       ! snapshot_node_path /var/lib/fluxo/node-toolchain.json node-toolchain.json ||
       ! snapshot_node_path /home/fluxo/.cache/node/corepack corepack-home; then
        sudo rm -rf -- "$NODE_ROLLBACK_DIR"
        NODE_ROLLBACK_DIR=""
        echo "ERROR: Could not create the Node.js toolchain rollback snapshot."
        return 1
    fi
    while IFS= read -r name; do
        path="/usr/local/bin/$name"
        if sudo test -e "$path" || sudo test -L "$path"; then
            if ! sudo touch "$NODE_ROLLBACK_DIR/links/$name.present" ||
               ! sudo cp -a -- "$path" "$NODE_ROLLBACK_DIR/links/$name"; then
                sudo rm -rf -- "$NODE_ROLLBACK_DIR"
                NODE_ROLLBACK_DIR=""
                echo "ERROR: Could not snapshot Node.js command link $path."
                return 1
            fi
        else
            if ! sudo touch "$NODE_ROLLBACK_DIR/links/$name.absent"; then
                sudo rm -rf -- "$NODE_ROLLBACK_DIR"
                NODE_ROLLBACK_DIR=""
                echo "ERROR: Could not record absent Node.js command link $path."
                return 1
            fi
        fi
    done < <(node_toolchain_link_names)
    NODE_ROLLBACK_ARMED=true
}

restore_node_path() {
    local target="$1" label="$2" parent staged
    if sudo test -f "$NODE_ROLLBACK_DIR/$label.present"; then
        parent="$(dirname "$target")"
        staged="$parent/.fluxo-node-rollback-${label}.$$"
        sudo mkdir -p "$parent" || return 1
        sudo rm -rf -- "$staged"
        if ! sudo cp -a --reflink=auto -- "$NODE_ROLLBACK_DIR/$label" "$staged"; then
            sudo rm -rf -- "$staged"
            return 1
        fi
        if ! sudo rm -rf -- "$target" || ! sudo mv -- "$staged" "$target"; then
            return 1
        fi
        return
    fi
    sudo rm -rf -- "$target"
}

rollback_node_toolchain() {
    local name path restore_failed=false
    if [ "$NODE_ROLLBACK_ARMED" != true ]; then
        return
    fi
    echo "Restoring the previous Fluxo-managed Node.js toolchain..."
    restore_node_path /opt/fluxo/node node || restore_failed=true
    restore_node_path /opt/fluxo/node-toolchain node-toolchain || restore_failed=true
    restore_node_path /var/lib/fluxo/node-toolchain.json node-toolchain.json || restore_failed=true
    restore_node_path /home/fluxo/.cache/node/corepack corepack-home || restore_failed=true

    while IFS= read -r name; do
        path="/usr/local/bin/$name"
        if sudo test -f "$NODE_ROLLBACK_DIR/links/$name.present"; then
            if is_managed_node_link "$path" || { ! sudo test -e "$path" && ! sudo test -L "$path"; }; then
                if ! sudo rm -f -- "$path" || ! sudo cp -a -- "$NODE_ROLLBACK_DIR/links/$name" "$path"; then
                    restore_failed=true
                fi
            elif is_managed_node_link "$NODE_ROLLBACK_DIR/links/$name"; then
                echo "ERROR: Refusing to overwrite externally replaced command $path during Node.js rollback."
                restore_failed=true
            fi
        elif is_managed_node_link "$path"; then
            sudo rm -f -- "$path" || restore_failed=true
        fi
    done < <(node_toolchain_link_names)

    if [ "$restore_failed" = true ]; then
        echo "ERROR: Node.js toolchain rollback was incomplete. Snapshot retained at $NODE_ROLLBACK_DIR."
        return 1
    fi
    NODE_ROLLBACK_ARMED=false
    sudo rm -rf -- "$NODE_ROLLBACK_DIR"
    NODE_ROLLBACK_DIR=""
    echo "Previous Fluxo-managed Node.js toolchain restored."
}

commit_node_toolchain_snapshot() {
    if [ "$NODE_ROLLBACK_ARMED" != true ]; then
        return
    fi
    NODE_ROLLBACK_ARMED=false
    if ! sudo rm -rf -- "$NODE_ROLLBACK_DIR"; then
        echo "WARNING: The successful Node.js rollback snapshot could not be removed: $NODE_ROLLBACK_DIR"
    fi
    NODE_ROLLBACK_DIR=""
}

prune_upgrade_snapshots() {
    local root=/var/lib/fluxo/upgrades
    local -a snapshots=()
    sudo test -d "$root" || return
    mapfile -t snapshots < <(sudo find "$root" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort)
    while [ "${#snapshots[@]}" -gt 3 ]; do
        if ! sudo rm -rf -- "$root/${snapshots[0]}"; then
            echo "WARNING: Could not prune old upgrade snapshot ${snapshots[0]}."
            return
        fi
        snapshots=("${snapshots[@]:1}")
    done
}

install_fluxo_binary() {
    sudo mkdir -p /usr/local/bin
    CANDIDATE_BINARY="$(sudo mktemp /usr/local/bin/.fluxo-install.XXXXXX)"
    sudo install -m 0755 -o root -g root "$PREPARED_BINARY" "$CANDIDATE_BINARY"
    if ! sudo "$CANDIDATE_BINARY" --version >/dev/null 2>&1; then
        echo "ERROR: The validated Fluxo candidate cannot run on this server."
        exit 1
    fi
    if [ "$INSTALL_NODE" = "true" ] && ! sudo "$CANDIDATE_BINARY" --supports-node-toolchain >/dev/null 2>&1; then
        echo "ERROR: The selected Fluxo binary does not support managed Node.js toolchains."
        echo "Install a newer Fluxo release or rerun with --no-node."
        exit 1
    fi
    prepare_upgrade_snapshot
    echo "Installing release-authenticated Composer ${COMPOSER_VERSION} globally..."
    install_composer
    echo "Installing release-authenticated WP-CLI ${WP_CLI_VERSION} globally..."
    install_wp_cli
    if [ "$INSTALL_NODE" = "true" ]; then
        prepare_node_toolchain_snapshot
        echo "Installing and verifying the Node.js toolchain..."
        sudo "$CANDIDATE_BINARY" node-toolchain install
        echo "Node.js toolchain installed successfully."
        echo ""
    fi

    sudo mv -f "$CANDIDATE_BINARY" /usr/local/bin/fluxo
    CANDIDATE_BINARY=""
}

install_fluxo_binary

installed_version_output="$(sudo /usr/local/bin/fluxo --version 2>/dev/null || true)"
case "$installed_version_output" in
    "fluxo version "*) INSTALLED_FLUXO_VERSION="${installed_version_output#fluxo version }" ;;
    *)                 INSTALLED_FLUXO_VERSION="$INSTALL_VERSION_LABEL" ;;
esac

# 4. Setup Systemd Service
echo "Configuring systemd service..."
SYSTEMD_CANDIDATE_DIR="$(mktemp -d)"
SYSTEMD_CANDIDATE="$SYSTEMD_CANDIDATE_DIR/fluxo.service"
cat > "$SYSTEMD_CANDIDATE" <<EOF
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
chmod 0644 "$SYSTEMD_CANDIDATE"
if command -v systemd-analyze >/dev/null 2>&1; then
    sudo systemd-analyze verify "$SYSTEMD_CANDIDATE"
fi
atomic_install_root "$SYSTEMD_CANDIDATE" /etc/systemd/system/fluxo.service 0644
rm -rf -- "$SYSTEMD_CANDIDATE_DIR"

configure_fluxo_fail2ban() {
    local temp_dir filter_candidate jail_candidate filter_backup jail_backup
    local filter_existed=false jail_existed=false
    echo "Configuring Fail2Ban protection for Fluxo login attempts..."
    temp_dir="$(mktemp -d)"
    filter_candidate="$temp_dir/fluxo-auth-filter.conf"
    jail_candidate="$temp_dir/fluxo-auth-jail.conf"
    filter_backup="$temp_dir/fluxo-auth-filter.backup"
    jail_backup="$temp_dir/fluxo-auth-jail.backup"
    cat > "$filter_candidate" <<'EOF'
[Definition]
failregex = ^.*fluxo_auth_failed remote=<HOST> username=.*$
ignoreregex =

[Init]
journalmatch = _SYSTEMD_UNIT=fluxo.service
EOF
    cat > "$jail_candidate" <<'EOF'
[fluxo-auth]
enabled = true
filter = fluxo-auth
backend = systemd
port = 9595
protocol = tcp
maxretry = 5
findtime = 15m
bantime = 1h
EOF
    if [ -f /etc/fail2ban/filter.d/fluxo-auth.conf ]; then
        sudo cp -- /etc/fail2ban/filter.d/fluxo-auth.conf "$filter_backup"
        filter_existed=true
    fi
    if [ -f /etc/fail2ban/jail.d/fluxo-auth.conf ]; then
        sudo cp -- /etc/fail2ban/jail.d/fluxo-auth.conf "$jail_backup"
        jail_existed=true
    fi
    if ! atomic_install_root "$filter_candidate" /etc/fail2ban/filter.d/fluxo-auth.conf 0644 ||
       ! atomic_install_root "$jail_candidate" /etc/fail2ban/jail.d/fluxo-auth.conf 0644; then
        echo "ERROR: Could not install Fluxo's Fail2Ban policy; restoring its previous configuration."
        if [ "$filter_existed" = true ]; then
            atomic_install_root "$filter_backup" /etc/fail2ban/filter.d/fluxo-auth.conf 0644
        else
            sudo rm -f /etc/fail2ban/filter.d/fluxo-auth.conf
        fi
        if [ "$jail_existed" = true ]; then
            atomic_install_root "$jail_backup" /etc/fail2ban/jail.d/fluxo-auth.conf 0644
        else
            sudo rm -f /etc/fail2ban/jail.d/fluxo-auth.conf
        fi
        rm -rf -- "$temp_dir"
        exit 1
    fi
    if ! sudo fail2ban-client -t; then
        echo "ERROR: Fail2Ban rejected the Fluxo jail; restoring its previous configuration."
        if [ "$filter_existed" = true ]; then
            atomic_install_root "$filter_backup" /etc/fail2ban/filter.d/fluxo-auth.conf 0644
        else
            sudo rm -f /etc/fail2ban/filter.d/fluxo-auth.conf
        fi
        if [ "$jail_existed" = true ]; then
            atomic_install_root "$jail_backup" /etc/fail2ban/jail.d/fluxo-auth.conf 0644
        else
            sudo rm -f /etc/fail2ban/jail.d/fluxo-auth.conf
        fi
        rm -rf -- "$temp_dir"
        exit 1
    fi
    rm -rf -- "$temp_dir"
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
    health_response="$(curl --fail --silent --show-error --insecure --connect-timeout 2 --max-time 5 https://localhost:9595/api/v1/health 2>/dev/null || true)"
    version_response="$(curl --fail --silent --show-error --insecure --connect-timeout 2 --max-time 5 https://localhost:9595/api/v1/version 2>/dev/null || true)"
    if python3 -c 'import json, sys
health = json.loads(sys.argv[1])
version = json.loads(sys.argv[2])
raise SystemExit(0 if health == {"status": "ok"} and version == {"version": sys.argv[3]} else 1)' \
        "$health_response" "$version_response" "$INSTALLED_FLUXO_VERSION" 2>/dev/null; then
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

# The candidate is now healthy. Keep its rollback snapshot for manual recovery,
# but stop arming automatic restoration for later non-critical output steps.
UPGRADE_ROLLBACK_ARMED=false
UPGRADE_SERVICE_STOPPED=false
commit_node_toolchain_snapshot
commit_upgrade_config_snapshot
prune_upgrade_snapshots

# Retry the first-install token read; upgrades must not reveal an existing token.
bootstrap_token=""
if [ "$EXISTING_INSTALL" = false ]; then
    for i in $(seq 1 3); do
        bootstrap_candidate="$(sudo awk -F': ' '/^Fluxo bootstrap token/{value=$2} END{print value}' "${CREDS_FILE}" 2>/dev/null || true)"
        if [[ "$bootstrap_candidate" =~ ^[0-9a-f]{32}$ ]]; then
            bootstrap_token="$bootstrap_candidate"
            break
        fi
        sleep 1
    done
fi

echo "========================================="
echo "Fluxo ${INSTALLED_FLUXO_VERSION} installed successfully!"
echo "========================================="
echo ""
echo "Access the Fluxo panel at:"
ips="$(hostname -I 2>/dev/null || true)"
for ip in $ips; do
    echo "  https://${ip}:9595"
done
echo ""
echo "The dashboard uses a self-signed TLS certificate."
echo "The certificate is auto-generated by the daemon on first boot (no install-script step needed)."
echo "Accept the browser warning to proceed."
echo ""
if [ -n "$bootstrap_token" ]; then
    echo "========================================================="
    echo "FIRST LOGIN CREDENTIALS"
    echo "Bootstrap token: ${bootstrap_token}"
    echo "Use this token as the password for your first login."
    echo "Store it securely. The installer will not display it again."
    echo "A recovery copy is stored in ${CREDS_FILE} with root-only permissions."
    echo "========================================================="
elif [ "$EXISTING_INSTALL" = false ]; then
    echo "WARNING: The bootstrap token could not be displayed safely."
    echo "Generate a replacement with: sudo fluxo --reset-token"
fi
echo ""
