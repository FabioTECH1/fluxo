package site

import (
	"strconv"
	"strings"
)

func NormalizeNodePreset(preset string) string {
	switch strings.ToLower(strings.TrimSpace(preset)) {
	case "next", "nuxt", "generic":
		return strings.ToLower(strings.TrimSpace(preset))
	default:
		return "generic"
	}
}

func NormalizeNodeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "server", "static":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "server"
	}
}

func NormalizePackageManager(pm string) string {
	switch strings.ToLower(strings.TrimSpace(pm)) {
	case "none", "npm", "pnpm", "yarn":
		return strings.ToLower(strings.TrimSpace(pm))
	default:
		return "npm"
	}
}

func PackageInstallCommand(pm string) string {
	switch NormalizePackageManager(pm) {
	case "none":
		return ""
	case "pnpm":
		return "pnpm install --frozen-lockfile || pnpm install"
	case "yarn":
		return "yarn install --frozen-lockfile || yarn install"
	default:
		return "npm ci || npm install"
	}
}

func DefaultNodeBuildCommand(preset, pm string) string {
	switch NormalizePackageManager(pm) {
	case "none":
		return ""
	case "pnpm":
		return "pnpm build"
	case "yarn":
		return "yarn build"
	default:
		return "npm run build"
	}
}

func DefaultNodeStartCommand(preset, pm string) string {
	switch NormalizeNodePreset(preset) {
	case "nuxt":
		return "/usr/bin/env PORT=$FLUXO_APP_PORT HOST=127.0.0.1 node .output/server/index.mjs"
	case "next":
		switch NormalizePackageManager(pm) {
		case "pnpm":
			return "/usr/bin/env PORT=$FLUXO_APP_PORT HOST=127.0.0.1 pnpm start -- -p $FLUXO_APP_PORT -H 127.0.0.1"
		case "yarn":
			return "/usr/bin/env PORT=$FLUXO_APP_PORT HOST=127.0.0.1 yarn start -p $FLUXO_APP_PORT -H 127.0.0.1"
		default:
			return "/usr/bin/env PORT=$FLUXO_APP_PORT HOST=127.0.0.1 npm run start -- -p $FLUXO_APP_PORT -H 127.0.0.1"
		}
	default:
		switch NormalizePackageManager(pm) {
		case "pnpm":
			return "/usr/bin/env PORT=$FLUXO_APP_PORT HOST=127.0.0.1 pnpm start"
		case "yarn":
			return "/usr/bin/env PORT=$FLUXO_APP_PORT HOST=127.0.0.1 yarn start"
		default:
			return "/usr/bin/env PORT=$FLUXO_APP_PORT HOST=127.0.0.1 npm run start"
		}
	}
}

func NormalizeStaticOutputDir(preset, dir string) string {
	dir = strings.TrimSpace(dir)
	if dir != "" {
		return dir
	}
	switch NormalizeNodePreset(preset) {
	case "nuxt":
		return ".output/public"
	case "generic":
		return "dist"
	default:
		return "out"
	}
}

func RenderNodeStartCommand(command string, appPort int) string {
	port := strconv.Itoa(appPort)
	replacer := strings.NewReplacer(
		"$FLUXO_APP_PORT", port,
		"${FLUXO_APP_PORT}", port,
		"$PORT", port,
		"${PORT}", port,
	)
	return strings.TrimSpace(replacer.Replace(command))
}
