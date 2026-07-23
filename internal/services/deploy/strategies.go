// Package deploy handles site deployment: script generation, execution, and record storage.
package deploy

// GenerateDeployScript returns a bash deployment script for the given strategy (standard, zero-downtime, octane).
func GenerateDeployScript(strategy string, appType string) string {
	if appType == "wordpress" {
		return ""
	}
	if appType == "node" {
		return GenerateNodeDeployScript(strategy)
	}
	if appType == "html" {
		if strategy == "zero-downtime" {
			return GenerateStaticDeployScript()
		}
		return generateStaticInPlaceDeployScript()
	}
	if strategy == "zero-downtime" {
		artisanCmds := ""
		if appType == "laravel" {
			artisanCmds = "\n[ -f artisan ] && php artisan key:generate --force && php artisan migrate --force && php artisan storage:link --force"
		}
		return `#!/bin/bash
set -e

DOMAIN="$FLUXO_DOMAIN"
REPO="$FLUXO_REPO"
BRANCH="$FLUXO_BRANCH"
TIMESTAMP=$(date +"%Y%m%d%H%M%S")

echo "Starting Zero-Downtime Deployment for $DOMAIN..."

RELEASE_DIR="$FLUXO_SITE_PATH/releases/$TIMESTAMP"
CURRENT_DIR="$FLUXO_SITE_PATH/current"

echo "Cloning repository..."
git clone -b $BRANCH $REPO $RELEASE_DIR

echo "Setting up shared persistence..."
ln -sfn "$FLUXO_SITE_PATH/.env" "$RELEASE_DIR/.env"
rm -rf $RELEASE_DIR/storage/app
ln -sfn "$FLUXO_SITE_PATH/storage/app" "$RELEASE_DIR/storage/app"

cd $RELEASE_DIR

[ -f composer.json ] && composer install --no-interaction --prefer-dist --optimize-autoloader
[ -f package.json ] && (npm ci || npm install) && npm run --if-present build` + artisanCmds + `

echo "Swapping symlink..."
ln -sfn $RELEASE_DIR $CURRENT_DIR

echo "Cleaning up old releases (keeping last 5)..."
cd "$FLUXO_SITE_PATH/releases"
ls -1t | tail -n +6 | while read old_release; do
  rm -rf "$FLUXO_SITE_PATH/releases/$old_release"
done

echo "Deployment Successful!"
`

	} else if strategy == "octane" && appType == "laravel" {
		return `#!/bin/bash
set -e

DOMAIN="$FLUXO_DOMAIN"
BRANCH="$FLUXO_BRANCH"

echo "Starting Octane Deployment for $DOMAIN..."
cd "$FLUXO_SITE_PATH"

git fetch origin
git checkout $BRANCH
git pull origin $BRANCH

[ -f composer.json ] && composer install --no-interaction --prefer-dist --optimize-autoloader
[ -f package.json ] && (npm ci || npm install) && npm run --if-present build
[ -f artisan ] && php artisan migrate --force

echo "Reloading Octane..."
php artisan octane:reload

echo "Deployment Successful!"
`

	}

	// Default: standard in-place deployment. Artisan belongs to Laravel only.
	artisanCmds := ""
	if appType == "laravel" {
		artisanCmds = `
if [ -f artisan ]; then
  php artisan key:generate --force
  php artisan migrate --force
fi
`
	}
	return `#!/bin/bash
set -e

DOMAIN="$FLUXO_DOMAIN"
BRANCH="$FLUXO_BRANCH"

echo "Starting Standard Deployment for $DOMAIN..."
cd "$FLUXO_SITE_PATH"

git fetch origin
git checkout $BRANCH
git pull origin $BRANCH

[ -f composer.json ] && composer install --no-interaction --prefer-dist --optimize-autoloader
[ -f package.json ] && (npm ci || npm install) && npm run --if-present build
` + artisanCmds + `
echo "Deployment Successful!"
`
}

func generateStaticInPlaceDeployScript() string {
	return `#!/bin/bash
set -e

echo "Starting static site deployment for $FLUXO_DOMAIN..."
cd "$FLUXO_SITE_PATH"

if [ ! -d .git ]; then
  git init
  git remote add origin "$FLUXO_REPO"
  git fetch origin
  git checkout -f "$FLUXO_BRANCH"
else
  git fetch origin
  git checkout "$FLUXO_BRANCH"
  git pull origin "$FLUXO_BRANCH"
fi

if [ -f package.json ]; then
  (npm ci || npm install)
  npm run --if-present build
fi

echo "Deployment Successful!"
`
}

// GenerateStaticDeployScript returns a zero-downtime deploy script for static sites.
func GenerateStaticDeployScript() string {
	return `#!/bin/bash
set -e

DOMAIN="$FLUXO_DOMAIN"
REPO="$FLUXO_REPO"
BRANCH="$FLUXO_BRANCH"
TIMESTAMP=$(date +"%Y%m%d%H%M%S")

echo "Starting static site release deployment for $DOMAIN..."

RELEASE_DIR="$FLUXO_SITE_PATH/releases/$TIMESTAMP"
CURRENT_DIR="$FLUXO_SITE_PATH/current"

echo "Cloning repository..."
git clone -b "$BRANCH" "$REPO" "$RELEASE_DIR"

cd "$RELEASE_DIR"

if [ -f package.json ]; then
  echo "Building static assets..."
  (npm ci || npm install)
  npm run --if-present build
fi

echo "Activating release..."
ln -sfn "$RELEASE_DIR" "$CURRENT_DIR"

echo "Cleaning up old releases (keeping last 5)..."
cd "$FLUXO_SITE_PATH/releases"
ls -1t | tail -n +6 | while read old_release; do
  rm -rf "$FLUXO_SITE_PATH/releases/$old_release"
done

echo "Deployment Successful!"
`
}

// GenerateNodeDeployScript returns a bash deployment script for Node.js sites.
func GenerateNodeDeployScript(strategy string) string {
	if strategy == "zero-downtime" {
		return `#!/bin/bash
set -e

DOMAIN="$FLUXO_DOMAIN"
REPO="$FLUXO_REPO"
BRANCH="$FLUXO_BRANCH"
TIMESTAMP=$(date +"%Y%m%d%H%M%S")

echo "Starting Node.js release deployment for $DOMAIN..."

RELEASE_DIR="$FLUXO_SITE_PATH/releases/$TIMESTAMP"
CURRENT_DIR="$FLUXO_SITE_PATH/current"

echo "Cloning repository..."
git clone -b "$BRANCH" "$REPO" "$RELEASE_DIR"

if [ -f "$FLUXO_SITE_PATH/.env" ]; then
  ln -sfn "$FLUXO_SITE_PATH/.env" "$RELEASE_DIR/.env"
fi

cd "$RELEASE_DIR"

if [ -n "$FLUXO_NODE_INSTALL_COMMAND" ]; then
  echo "Installing dependencies..."
  bash -lc "$FLUXO_NODE_INSTALL_COMMAND"
fi

if [ -n "$FLUXO_NODE_BUILD_COMMAND" ]; then
  echo "Building application..."
  bash -lc "$FLUXO_NODE_BUILD_COMMAND"
fi

echo "Activating release..."
ln -sfn "$RELEASE_DIR" "$CURRENT_DIR"

echo "Cleaning up old releases (keeping last 5)..."
cd "$FLUXO_SITE_PATH/releases"
ls -1t | tail -n +6 | while read old_release; do
  rm -rf "$FLUXO_SITE_PATH/releases/$old_release"
done

echo "Deployment Successful!"
`
	}

	return `#!/bin/bash
set -e

echo "Starting Node.js deployment for $FLUXO_DOMAIN..."
cd "$FLUXO_SITE_PATH"

if [ ! -d .git ]; then
  echo "Initializing Git repository..."
  git init
  git remote add origin "$FLUXO_REPO"
  git fetch origin
  git checkout -f "$FLUXO_BRANCH"
else
  git fetch origin
  git checkout "$FLUXO_BRANCH"
  git pull origin "$FLUXO_BRANCH"
fi

if [ -n "$FLUXO_NODE_INSTALL_COMMAND" ]; then
  echo "Installing dependencies..."
  bash -lc "$FLUXO_NODE_INSTALL_COMMAND"
fi

if [ -n "$FLUXO_NODE_BUILD_COMMAND" ]; then
  echo "Building application..."
  bash -lc "$FLUXO_NODE_BUILD_COMMAND"
fi

echo "Deployment Successful!"
`
}

// GenerateRollbackScript returns a bash deployment script that checks out a specific commit.
func GenerateRollbackScript(strategy string, appType string) string {
	if appType == "wordpress" {
		return ""
	}
	if appType == "node" {
		return GenerateNodeRollbackScript(strategy)
	}
	if appType == "html" {
		if strategy == "zero-downtime" {
			return GenerateStaticRollbackScript()
		}
		return generateStaticInPlaceRollbackScript()
	}
	if strategy == "zero-downtime" {
		artisanCmds := ""
		if appType == "laravel" {
			artisanCmds = "\n[ -f artisan ] && php artisan key:generate --force && php artisan migrate --force && php artisan storage:link --force"
		}
		return `#!/bin/bash
set -e

DOMAIN="$FLUXO_DOMAIN"
REPO="$FLUXO_REPO"
TARGET_COMMIT="$FLUXO_TARGET_COMMIT"
TIMESTAMP=$(date +"%Y%m%d%H%M%S")

echo "Starting Rollback for $DOMAIN to $TARGET_COMMIT..."

RELEASE_DIR="$FLUXO_SITE_PATH/releases/$TIMESTAMP"
CURRENT_DIR="$FLUXO_SITE_PATH/current"

echo "Cloning repository..."
git clone $REPO $RELEASE_DIR
cd $RELEASE_DIR
git checkout $TARGET_COMMIT

echo "Setting up shared persistence..."
ln -sfn "$FLUXO_SITE_PATH/.env" "$RELEASE_DIR/.env"
rm -rf $RELEASE_DIR/storage/app
ln -sfn "$FLUXO_SITE_PATH/storage/app" "$RELEASE_DIR/storage/app"

cd $RELEASE_DIR

[ -f composer.json ] && composer install --no-interaction --prefer-dist --optimize-autoloader
[ -f package.json ] && (npm ci || npm install) && npm run --if-present build` + artisanCmds + `

echo "Swapping symlink..."
ln -sfn $RELEASE_DIR $CURRENT_DIR

echo "Cleaning up old releases (keeping last 5)..."
cd "$FLUXO_SITE_PATH/releases"
ls -1t | tail -n +6 | while read old_release; do
  rm -rf "$FLUXO_SITE_PATH/releases/$old_release"
done

echo "Rollback Successful!"
`

	} else if strategy == "octane" && appType == "laravel" {
		return `#!/bin/bash
set -e

DOMAIN="$FLUXO_DOMAIN"
TARGET_COMMIT="$FLUXO_TARGET_COMMIT"

echo "Starting Rollback for $DOMAIN to $TARGET_COMMIT..."
cd "$FLUXO_SITE_PATH"

git fetch origin
git checkout $TARGET_COMMIT

[ -f composer.json ] && composer install --no-interaction --prefer-dist --optimize-autoloader
[ -f package.json ] && (npm ci || npm install) && npm run --if-present build
[ -f artisan ] && php artisan migrate --force

echo "Reloading Octane..."
php artisan octane:reload

echo "Rollback Successful!"
`

	}

	// Default: standard in-place rollback. Artisan belongs to Laravel only.
	artisanCmds := ""
	if appType == "laravel" {
		artisanCmds = `
if [ -f artisan ]; then
  php artisan key:generate --force
  php artisan migrate --force
fi
`
	}
	return `#!/bin/bash
set -e

DOMAIN="$FLUXO_DOMAIN"
TARGET_COMMIT="$FLUXO_TARGET_COMMIT"

echo "Starting Rollback for $DOMAIN to $TARGET_COMMIT..."
cd "$FLUXO_SITE_PATH"

git fetch origin
git checkout $TARGET_COMMIT

[ -f composer.json ] && composer install --no-interaction --prefer-dist --optimize-autoloader
[ -f package.json ] && (npm ci || npm install) && npm run --if-present build
` + artisanCmds + `
echo "Rollback Successful!"
`
}

func generateStaticInPlaceRollbackScript() string {
	return `#!/bin/bash
set -e

echo "Starting static site rollback for $FLUXO_DOMAIN to $FLUXO_TARGET_COMMIT..."
cd "$FLUXO_SITE_PATH"

git fetch origin
git checkout "$FLUXO_TARGET_COMMIT"

if [ -f package.json ]; then
  (npm ci || npm install)
  npm run --if-present build
fi

echo "Rollback Successful!"
`
}

// GenerateStaticRollbackScript returns a zero-downtime rollback script for static sites.
func GenerateStaticRollbackScript() string {
	return `#!/bin/bash
set -e

DOMAIN="$FLUXO_DOMAIN"
REPO="$FLUXO_REPO"
TARGET_COMMIT="$FLUXO_TARGET_COMMIT"
TIMESTAMP=$(date +"%Y%m%d%H%M%S")

echo "Starting static site rollback for $DOMAIN to $TARGET_COMMIT..."

RELEASE_DIR="$FLUXO_SITE_PATH/releases/$TIMESTAMP"
CURRENT_DIR="$FLUXO_SITE_PATH/current"

git clone "$REPO" "$RELEASE_DIR"
cd "$RELEASE_DIR"
git checkout "$TARGET_COMMIT"

if [ -f package.json ]; then
  echo "Building static assets..."
  (npm ci || npm install)
  npm run --if-present build
fi

echo "Activating rollback release..."
ln -sfn "$RELEASE_DIR" "$CURRENT_DIR"

echo "Cleaning up old releases (keeping last 5)..."
cd "$FLUXO_SITE_PATH/releases"
ls -1t | tail -n +6 | while read old_release; do
  rm -rf "$FLUXO_SITE_PATH/releases/$old_release"
done

echo "Rollback Successful!"
`
}

// GenerateNodeRollbackScript returns a bash rollback script for Node.js sites.
func GenerateNodeRollbackScript(strategy string) string {
	if strategy == "zero-downtime" {
		return `#!/bin/bash
set -e

DOMAIN="$FLUXO_DOMAIN"
REPO="$FLUXO_REPO"
TARGET_COMMIT="$FLUXO_TARGET_COMMIT"
TIMESTAMP=$(date +"%Y%m%d%H%M%S")

echo "Starting Node.js rollback for $DOMAIN to $TARGET_COMMIT..."

RELEASE_DIR="$FLUXO_SITE_PATH/releases/$TIMESTAMP"
CURRENT_DIR="$FLUXO_SITE_PATH/current"

git clone "$REPO" "$RELEASE_DIR"
cd "$RELEASE_DIR"
git checkout "$TARGET_COMMIT"

if [ -f "$FLUXO_SITE_PATH/.env" ]; then
  ln -sfn "$FLUXO_SITE_PATH/.env" "$RELEASE_DIR/.env"
fi

if [ -n "$FLUXO_NODE_INSTALL_COMMAND" ]; then
  echo "Installing dependencies..."
  bash -lc "$FLUXO_NODE_INSTALL_COMMAND"
fi

if [ -n "$FLUXO_NODE_BUILD_COMMAND" ]; then
  echo "Building application..."
  bash -lc "$FLUXO_NODE_BUILD_COMMAND"
fi

echo "Activating rollback release..."
ln -sfn "$RELEASE_DIR" "$CURRENT_DIR"

echo "Cleaning up old releases (keeping last 5)..."
cd "$FLUXO_SITE_PATH/releases"
ls -1t | tail -n +6 | while read old_release; do
  rm -rf "$FLUXO_SITE_PATH/releases/$old_release"
done

echo "Rollback Successful!"
`
	}

	return `#!/bin/bash
set -e

echo "Starting Node.js rollback for $FLUXO_DOMAIN to $FLUXO_TARGET_COMMIT..."
cd "$FLUXO_SITE_PATH"

git fetch origin
git checkout "$FLUXO_TARGET_COMMIT"

if [ -n "$FLUXO_NODE_INSTALL_COMMAND" ]; then
  echo "Installing dependencies..."
  bash -lc "$FLUXO_NODE_INSTALL_COMMAND"
fi

if [ -n "$FLUXO_NODE_BUILD_COMMAND" ]; then
  echo "Building application..."
  bash -lc "$FLUXO_NODE_BUILD_COMMAND"
fi

echo "Rollback Successful!"
`
}
