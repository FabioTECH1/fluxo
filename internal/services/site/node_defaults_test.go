package site

import "testing"

func TestBunNodeDefaults(t *testing.T) {
	if got := NormalizePackageManager(" BUN "); got != "bun" {
		t.Fatalf("NormalizePackageManager() = %q, want bun", got)
	}
	if got := PackageInstallCommand("bun"); got != "bun install --frozen-lockfile || bun install" {
		t.Fatalf("PackageInstallCommand() = %q", got)
	}
	if got := DefaultNodeBuildCommand("next", "bun"); got != "bun run build" {
		t.Fatalf("DefaultNodeBuildCommand() = %q", got)
	}
	if got := DefaultNodeStartCommand("next", "bun"); got != "/usr/bin/env PORT=$FLUXO_APP_PORT HOST=127.0.0.1 bun run start -p $FLUXO_APP_PORT -H 127.0.0.1" {
		t.Fatalf("DefaultNodeStartCommand() = %q", got)
	}
}

func TestYarnInstallSupportsModernAndClassicReleases(t *testing.T) {
	want := "yarn install --immutable || yarn install --frozen-lockfile || yarn install"
	if got := PackageInstallCommand("yarn"); got != want {
		t.Fatalf("PackageInstallCommand(yarn) = %q, want %q", got, want)
	}
}

func TestNoPackageManagerDoesNotDefaultToNPM(t *testing.T) {
	if got := PackageInstallCommand("none"); got != "" {
		t.Fatalf("PackageInstallCommand(none) = %q", got)
	}
	if got := DefaultNodeBuildCommand("generic", "none"); got != "" {
		t.Fatalf("DefaultNodeBuildCommand(none) = %q", got)
	}
	if got := DefaultNodeStartCommand("generic", "none"); got != "/usr/bin/env PORT=$FLUXO_APP_PORT HOST=127.0.0.1 node server.js" {
		t.Fatalf("DefaultNodeStartCommand(generic, none) = %q", got)
	}
	if got := DefaultNodeStartCommand("next", "none"); got != "/usr/bin/env PORT=$FLUXO_APP_PORT HOST=127.0.0.1 node node_modules/next/dist/bin/next start -p $FLUXO_APP_PORT -H 127.0.0.1" {
		t.Fatalf("DefaultNodeStartCommand(next, none) = %q", got)
	}
}
