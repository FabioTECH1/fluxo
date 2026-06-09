// Package deploy handles site deployment: generating bash scripts for
// different strategies, executing them with real-time log broadcasting,
// and storing deployment records.
package deploy

import (
	"fmt"
	"time"
)

// GenerateDeployScript returns a bash deployment script for the given
// strategy. The script is written to a temp file and executed by RunScript
// with real-time log streaming via WebSocket.
//
// Three strategies are supported:
//
//	standard     — git pull in-place, run composer/npm/artisan
//	zero-downtime — git clone to a timestamped release directory,
//	                symlink shared files, swap "current" symlink
//	octane       — git pull + "php artisan octane:reload" (no downtime)
func GenerateDeployScript(strategy string, domain string, repository string, branch string, phpVersion string, appType string) string {
	timestamp := time.Now().Format("20060102150405")

	if strategy == "zero-downtime" {
		return fmt.Sprintf(`#!/bin/bash
set -e

DOMAIN="%s"
REPO="%s"
BRANCH="%s"
TIMESTAMP="%s"

echo "Starting Zero-Downtime Deployment for $DOMAIN..."

RELEASE_DIR="/var/www/$DOMAIN/releases/$TIMESTAMP"
CURRENT_DIR="/var/www/$DOMAIN/current"

echo "Cloning repository..."
git clone -b $BRANCH $REPO $RELEASE_DIR

echo "Setting up shared persistence..."
ln -sfn /var/www/$DOMAIN/.env $RELEASE_DIR/.env
rm -rf $RELEASE_DIR/storage/app
ln -sfn /var/www/$DOMAIN/storage/app $RELEASE_DIR/storage/app

cd $RELEASE_DIR

[ -f composer.json ] && composer install --no-interaction --prefer-dist --optimize-autoloader
[ -f package.json ] && npm install && npm run build
[ -f artisan ] && php artisan key:generate --force && php artisan migrate --force

echo "Swapping symlink..."
ln -sfn $RELEASE_DIR $CURRENT_DIR

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

[ -f composer.json ] && composer install --no-interaction --prefer-dist --optimize-autoloader
[ -f package.json ] && npm install && npm run build
[ -f artisan ] && php artisan migrate --force

echo "Reloading Octane..."
php artisan octane:reload

echo "Deployment Successful!"
`, domain, branch)

	}

	// Default: standard in-place deployment.
	return fmt.Sprintf(`#!/bin/bash
set -e

DOMAIN="%s"
BRANCH="%s"

echo "Starting Standard Deployment for $DOMAIN..."
cd /var/www/$DOMAIN

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
`, domain, branch)
}
