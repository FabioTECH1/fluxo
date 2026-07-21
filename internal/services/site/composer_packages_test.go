package site

import (
	"os"
	"path/filepath"
	"testing"
)

func TestActiveSitePath(t *testing.T) {
	tests := []struct {
		name               string
		deploymentStrategy string
		want               string
	}{
		{name: "standard", deploymentStrategy: "standard", want: "/home/fluxo/example.com"},
		{name: "zero downtime", deploymentStrategy: "zero-downtime", want: filepath.Join("/home/fluxo/example.com", "current")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ActiveSitePath("/home/fluxo/example.com", tt.deploymentStrategy); got != tt.want {
				t.Fatalf("ActiveSitePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectComposerCapabilitiesIncludesHorizon(t *testing.T) {
	tests := []struct {
		name               string
		deploymentStrategy string
	}{
		{name: "standard", deploymentStrategy: "standard"},
		{name: "zero downtime", deploymentStrategy: "zero-downtime"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sitePath := t.TempDir()
			activePath := ActiveSitePath(sitePath, tt.deploymentStrategy)
			if err := os.MkdirAll(activePath, 0755); err != nil {
				t.Fatal(err)
			}
			lock := `{"packages":[{"name":"laravel/framework","version":"v12.0.0"},{"name":"laravel/horizon","version":"v5.35.0"}]}`
			if err := os.WriteFile(filepath.Join(activePath, "composer.lock"), []byte(lock), 0644); err != nil {
				t.Fatal(err)
			}

			capabilities, err := DetectComposerCapabilities(sitePath, tt.deploymentStrategy)
			if err != nil {
				t.Fatal(err)
			}
			if !capabilities.Horizon || capabilities.HorizonVersion != "v5.35.0" {
				t.Fatalf("Horizon detection = (%v, %q), want (true, %q)", capabilities.Horizon, capabilities.HorizonVersion, "v5.35.0")
			}
		})
	}
}
