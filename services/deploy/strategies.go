package deploy

import (
	"fmt"
	"time"
)

func GenerateDeployScript(strategy string, domain string, repository string, branch string, phpVersion string) string {
	timestamp := time.Now().Format("20060102150405")

	if strategy == "zero-downtime" {
		return fmt.Sprintf(`#!/bin/bash
set -e

# Zero-Downtime Deployment
DOMAIN="%s"
REPO="%s"
BRANCH="%s"
TIMESTAMP="%s"

echo "Starting Zero-Downtime Deployment for $DOMAIN..."

RELEASE_DIR="/var/www/$DOMAIN/releases/$TIMESTAMP"
CURRENT_DIR="/var/www/$DOMAIN/current"

# 1. Clone repository
echo "Cloning repository..."
git clone -b $BRANCH $REPO $RELEASE_DIR

# 2. Shared persistence (symlink .env and storage)
echo "Setting up shared persistence..."
ln -sfn /var/www/$DOMAIN/.env $RELEASE_DIR/.env
rm -rf $RELEASE_DIR/storage/app
ln -sfn /var/www/$DOMAIN/storage/app $RELEASE_DIR/storage/app

# 3. Install dependencies
echo "Installing dependencies..."
cd $RELEASE_DIR
composer install --no-interaction --prefer-dist --optimize-autoloader

# 4. Run migrations
echo "Running migrations..."
php artisan migrate --force

# 5. Atomic Symlink Swap
echo "Swapping symlink..."
ln -sfn $RELEASE_DIR $CURRENT_DIR

# 6. Reload PHP-FPM
echo "Reloading PHP-FPM..."
systemctl reload php%s-fpm

echo "Deployment Successful!"
`, domain, repository, branch, timestamp, phpVersion)

	} else if strategy == "octane" {
		return fmt.Sprintf(`#!/bin/bash
set -e

DOMAIN="%s"
BRANCH="%s"

echo "Starting Octane Deployment for $DOMAIN..."
cd /var/www/$DOMAIN

git fetch origin
git checkout $BRANCH
git pull origin $BRANCH

composer install --no-interaction --prefer-dist --optimize-autoloader
php artisan migrate --force

echo "Reloading Octane..."
php artisan octane:reload

echo "Deployment Successful!"
`, domain, branch)

	} else {
		// Standard
		return fmt.Sprintf(`#!/bin/bash
set -e

DOMAIN="%s"
BRANCH="%s"

echo "Starting Standard Deployment for $DOMAIN..."
cd /var/www/$DOMAIN

git fetch origin
git checkout $BRANCH
git pull origin $BRANCH

composer install --no-interaction --prefer-dist --optimize-autoloader
php artisan migrate --force

echo "Deployment Successful!"
`, domain, branch)
	}
}
