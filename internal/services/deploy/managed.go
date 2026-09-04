package deploy

import (
	"database/sql"
	"fmt"
	"strings"
)

const (
	ScriptModeManaged = "managed"
	ScriptModeLegacy  = "legacy"

	legacyNodeApplicationCommands = `if [ -n "$FLUXO_NODE_INSTALL_COMMAND" ]; then
  bash -lc "$FLUXO_NODE_INSTALL_COMMAND"
fi

if [ -n "$FLUXO_NODE_BUILD_COMMAND" ]; then
  bash -lc "$FLUXO_NODE_BUILD_COMMAND"
fi`
)

// GenerateApplicationCommands returns the editable, application-specific part
// of a deployment. Repository synchronization, release activation, service
// restarts, and release cleanup are owned by Fluxo's managed lifecycle.
func GenerateApplicationCommands(appType string, hasDatabase bool) string {
	switch appType {
	case "laravel":
		commands := `if [ -f composer.json ]; then
  $FLUXO_COMPOSER install --no-dev --no-interaction --prefer-dist --optimize-autoloader
fi

if [ -f package.json ]; then
  (npm ci || npm install)
  npm run --if-present build
fi

if [ -f artisan ]; then
  $FLUXO_PHP artisan optimize:clear
  $FLUXO_PHP artisan storage:link`
		if hasDatabase {
			commands += "\n  $FLUXO_PHP artisan migrate --force"
		}
		return commands + "\nfi"
	case "php":
		return `if [ -f composer.json ]; then
  $FLUXO_COMPOSER install --no-dev --no-interaction --prefer-dist --optimize-autoloader
fi

if [ -f package.json ]; then
  (npm ci || npm install)
  npm run --if-present build
fi`
	case "html":
		return `if [ -f package.json ]; then
  (npm ci || npm install)
  npm run --if-present build
fi`
	case "node":
		return `if [ -f package.json ]; then
  if [ -n "$FLUXO_NODE_INSTALL_COMMAND" ]; then
    bash -lc "$FLUXO_NODE_INSTALL_COMMAND"
  fi

  if [ -n "$FLUXO_NODE_BUILD_COMMAND" ]; then
    bash -lc "$FLUXO_NODE_BUILD_COMMAND"
  fi
fi`
	case "python":
		return `cd "$FLUXO_APP_DIRECTORY"

if [ "$FLUXO_APP_DIRECTORY" != "." ] && [ ! -e .env ] && [ -f "$FLUXO_SITE_PATH/.env" ]; then
  ln -s "$FLUXO_SITE_PATH/.env" .env
fi

bash -lc "$FLUXO_PYTHON_INSTALL_COMMAND"

if [ -n "$FLUXO_PYTHON_BUILD_COMMAND" ]; then
  bash -lc "$FLUXO_PYTHON_BUILD_COMMAND"
fi`
	default:
		return ""
	}
}

// NormalizeApplicationCommands upgrades untouched platform defaults while
// preserving scripts that a site owner has customized.
func NormalizeApplicationCommands(appType, commands string, hasDatabase bool) string {
	if appType == "node" && strings.TrimSpace(commands) == strings.TrimSpace(legacyNodeApplicationCommands) {
		return GenerateApplicationCommands(appType, false)
	}
	if appType == "laravel" && !hasDatabase && strings.TrimSpace(commands) == strings.TrimSpace(GenerateApplicationCommands(appType, true)) {
		return GenerateApplicationCommands(appType, false)
	}
	return commands
}

// MigrateApplicationCommandDefaults updates only recognizable old platform
// defaults, before a user has a chance to customize them in the UI.
func MigrateApplicationCommandDefaults(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("migrate application command defaults: database is nil")
	}
	rows, err := db.Query(`SELECT s.id, COALESCE(s.app_type, ''), COALESCE(s.deploy_script, ''),
		EXISTS(SELECT 1 FROM databases d WHERE d.site_id = s.id)
		FROM sites s
		WHERE s.app_type IN ('node', 'laravel') AND COALESCE(s.deploy_script_mode, 'legacy') = ?`, ScriptModeManaged)
	if err != nil {
		return fmt.Errorf("query old application command defaults: %w", err)
	}

	type scriptUpdate struct {
		id       int
		appType  string
		previous string
		current  string
		hasDB    bool
	}
	updates := make([]scriptUpdate, 0)
	for rows.Next() {
		var update scriptUpdate
		if err := rows.Scan(&update.id, &update.appType, &update.previous, &update.hasDB); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan old application command default: %w", err)
		}
		update.current = NormalizeApplicationCommands(update.appType, update.previous, update.hasDB)
		if update.current != update.previous {
			updates = append(updates, update)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("read old application command defaults: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close old application command defaults: %w", err)
	}

	for _, update := range updates {
		if _, err := db.Exec(`UPDATE sites SET deploy_script = ?
			WHERE id = ? AND deploy_script = ? AND COALESCE(deploy_script_mode, 'legacy') = ?`,
			update.current, update.id, update.previous, ScriptModeManaged); err != nil {
			return fmt.Errorf("migrate application command default for site %d: %w", update.id, err)
		}
	}
	return nil
}

// GenerateManagedLifecycle wraps editable application commands in the
// platform-owned standard or zero-downtime deployment lifecycle.
func GenerateManagedLifecycle(strategy string) string {
	if strategy == "zero-downtime" {
		return managedZDDLifecycle()
	}
	return managedStandardLifecycle()
}

func managedStandardLifecycle() string {
	return `#!/bin/bash
set -Eeuo pipefail

echo "Preparing standard deployment for $FLUXO_DOMAIN..."

if [ -n "$FLUXO_REPO" ]; then
  cd "$FLUXO_SITE_PATH"
  if [ ! -d .git ]; then
    git init
  fi
  if git remote get-url origin >/dev/null 2>&1; then
    git remote set-url origin "$FLUXO_REPO"
  else
    git remote add origin "$FLUXO_REPO"
  fi

  git fetch origin

  # A standard deployment owns tracked application files. Clear edits left in
  # the live checkout before selecting the requested branch or rollback commit.
  # Untracked persistent files such as .env are deliberately preserved.
  if git rev-parse --verify HEAD >/dev/null 2>&1; then
    git reset --hard HEAD
  fi

  if [ -n "${FLUXO_TARGET_COMMIT:-}" ]; then
    git checkout --detach "$FLUXO_TARGET_COMMIT"
  else
    if git show-ref --verify --quiet "refs/heads/$FLUXO_BRANCH"; then
      git checkout "$FLUXO_BRANCH"
    else
      git checkout -b "$FLUXO_BRANCH" "origin/$FLUXO_BRANCH"
    fi
    git reset --hard "origin/$FLUXO_BRANCH"
  fi
fi

export FLUXO_DEPLOY_PATH="$FLUXO_SITE_PATH"

echo "Running application commands..."
(
  cd "$FLUXO_DEPLOY_PATH"
  if [ -s "${FLUXO_APPLICATION_SCRIPT:-}" ]; then
    bash -Eeuo pipefail "$FLUXO_APPLICATION_SCRIPT"
  fi
)

echo "Application commands completed."
`
}

func managedZDDLifecycle() string {
	return `#!/bin/bash
set -Eeuo pipefail

echo "Preparing zero-downtime release for $FLUXO_DOMAIN..."

RELEASES_DIR="$FLUXO_SITE_PATH/releases"
RELEASE_DIR="$RELEASES_DIR/$FLUXO_RELEASE_ID"
CURRENT_DIR="$FLUXO_SITE_PATH/current"
TEMP_CURRENT="$FLUXO_SITE_PATH/.current-$FLUXO_RELEASE_ID"
ACTIVATED=0

copy_missing_storage_items() {
  local source_dir="$1"
  local target_dir="$2"

  [ -d "$source_dir" ] || return 0
  if [ "$(readlink -f "$source_dir")" = "$(readlink -f "$target_dir")" ]; then
    return 0
  fi

  shopt -s dotglob nullglob
  local item name
  for item in "$source_dir"/*; do
    name="${item##*/}"
    if [ ! -e "$target_dir/$name" ] && [ ! -L "$target_dir/$name" ]; then
      cp -a "$item" "$target_dir/$name"
    elif [ -d "$item" ] && [ ! -L "$item" ] && [ -d "$target_dir/$name" ] && [ ! -L "$target_dir/$name" ]; then
      copy_missing_storage_items "$item" "$target_dir/$name"
    fi
  done
  shopt -u dotglob nullglob
}

cleanup_failed_release() {
  rm -f "$TEMP_CURRENT"
  if [ "$ACTIVATED" -eq 0 ] && [ -d "$RELEASE_DIR" ]; then
    rm -rf "$RELEASE_DIR"
  fi
}
trap cleanup_failed_release ERR
trap 'cleanup_failed_release; exit 130' INT TERM

mkdir -p "$RELEASES_DIR"
if [ -e "$RELEASE_DIR" ]; then
  echo "Release directory already exists: $RELEASE_DIR" >&2
  exit 1
fi

if [ -n "${FLUXO_TARGET_COMMIT:-}" ]; then
  git clone "$FLUXO_REPO" "$RELEASE_DIR"
  git -C "$RELEASE_DIR" checkout "$FLUXO_TARGET_COMMIT"
else
  git clone --branch "$FLUXO_BRANCH" "$FLUXO_REPO" "$RELEASE_DIR"
fi

if [ -f "$FLUXO_SITE_PATH/.env" ]; then
  ln -sfn "$FLUXO_SITE_PATH/.env" "$RELEASE_DIR/.env"
fi

if [ "$FLUXO_APP_TYPE" = "laravel" ]; then
  SHARED_STORAGE="$FLUXO_SITE_PATH/storage"
  mkdir -p "$SHARED_STORAGE"

  # Preserve state from sites that previously shared only storage/app, then
  # seed any missing framework directories from the freshly cloned release.
  if [ -e "$CURRENT_DIR/storage" ]; then
    copy_missing_storage_items "$CURRENT_DIR/storage" "$SHARED_STORAGE"
  fi
  copy_missing_storage_items "$RELEASE_DIR/storage" "$SHARED_STORAGE"

  mkdir -p \
    "$SHARED_STORAGE/app/public" \
    "$SHARED_STORAGE/framework/cache/data" \
    "$SHARED_STORAGE/framework/sessions" \
    "$SHARED_STORAGE/framework/views" \
    "$SHARED_STORAGE/logs"
  rm -rf "$RELEASE_DIR/storage"
  ln -s "$SHARED_STORAGE" "$RELEASE_DIR/storage"
fi

export FLUXO_RELEASE_DIRECTORY="$RELEASE_DIR"
export FLUXO_DEPLOY_PATH="$RELEASE_DIR"

echo "Running application commands..."
(
  cd "$FLUXO_DEPLOY_PATH"
  if [ -s "${FLUXO_APPLICATION_SCRIPT:-}" ]; then
    bash -Eeuo pipefail "$FLUXO_APPLICATION_SCRIPT"
  fi
)

echo "Activating release..."
ln -s "$RELEASE_DIR" "$TEMP_CURRENT"
mv -Tf "$TEMP_CURRENT" "$CURRENT_DIR"
ACTIVATED=1
trap - ERR INT TERM

echo "Release activated."
`
}
