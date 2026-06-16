// Package deploy handles site deployment: script generation, execution, and record storage.
package deploy

// GenerateDeployScript returns a bash deployment script for the given strategy (standard, zero-downtime, octane).
func GenerateDeployScript(strategy string) string {
	if strategy == "zero-downtime" {
		return `#!/bin/bash
set -e

DOMAIN="$FLUXO_DOMAIN"
REPO="$FLUXO_REPO"
BRANCH="$FLUXO_BRANCH"
TIMESTAMP=$(date +"%Y%m%d%H%M%S")

echo "Starting Zero-Downtime Deployment for $DOMAIN..."

RELEASE_DIR="/home/fluxo/$DOMAIN/releases/$TIMESTAMP"
CURRENT_DIR="/home/fluxo/$DOMAIN/current"

echo "Cloning repository..."
git clone -b $BRANCH $REPO $RELEASE_DIR

echo "Setting up shared persistence..."
ln -sfn /home/fluxo/$DOMAIN/.env $RELEASE_DIR/.env
rm -rf $RELEASE_DIR/storage/app
ln -sfn /home/fluxo/$DOMAIN/storage/app $RELEASE_DIR/storage/app

cd $RELEASE_DIR

[ -f composer.json ] && composer install --no-interaction --prefer-dist --optimize-autoloader
[ -f package.json ] && npm install && npm run build
[ -f artisan ] && php artisan key:generate --force && php artisan migrate --force

echo "Swapping symlink..."
ln -sfn $RELEASE_DIR $CURRENT_DIR

echo "Deployment Successful!"
`

	} else if strategy == "octane" {
		return `#!/bin/bash
set -e

DOMAIN="$FLUXO_DOMAIN"
BRANCH="$FLUXO_BRANCH"

echo "Starting Octane Deployment for $DOMAIN..."
cd /home/fluxo/$DOMAIN

git fetch origin
git checkout $BRANCH
git pull origin $BRANCH

[ -f composer.json ] && composer install --no-interaction --prefer-dist --optimize-autoloader
[ -f package.json ] && npm install && npm run build
[ -f artisan ] && php artisan migrate --force

echo "Reloading Octane..."
php artisan octane:reload

echo "Deployment Successful!"
`

	}

	// Default: standard in-place deployment.
	return `#!/bin/bash
set -e

DOMAIN="$FLUXO_DOMAIN"
BRANCH="$FLUXO_BRANCH"

echo "Starting Standard Deployment for $DOMAIN..."
cd /home/fluxo/$DOMAIN

git fetch origin
git checkout $BRANCH
git pull origin $BRANCH

[ -f composer.json ] && composer install --no-interaction --prefer-dist --optimize-autoloader
[ -f package.json ] && npm install && npm run build

if [ -f artisan ]; then
  php artisan key:generate --force
  php artisan migrate --force
fi

echo "Deployment Successful!"
`
}
