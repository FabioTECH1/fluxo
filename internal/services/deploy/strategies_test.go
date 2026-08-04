package deploy

import (
	"os/exec"
	"strings"
	"testing"
)

func TestGeneratedLegacyStrategiesUseStableSitePath(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{name: "php standard deploy", script: GenerateDeployScript("standard", "php")},
		{name: "laravel zero downtime deploy", script: GenerateDeployScript("zero-downtime", "laravel")},
		{name: "laravel octane deploy", script: GenerateDeployScript("octane", "laravel")},
		{name: "static zero downtime deploy", script: GenerateDeployScript("zero-downtime", "html")},
		{name: "node zero downtime deploy", script: GenerateDeployScript("zero-downtime", "node")},
		{name: "php standard rollback", script: GenerateRollbackScript("standard", "php")},
		{name: "laravel zero downtime rollback", script: GenerateRollbackScript("zero-downtime", "laravel")},
		{name: "laravel octane rollback", script: GenerateRollbackScript("octane", "laravel")},
		{name: "static zero downtime rollback", script: GenerateRollbackScript("zero-downtime", "html")},
		{name: "node zero downtime rollback", script: GenerateRollbackScript("zero-downtime", "node")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if strings.Contains(test.script, "/home/fluxo/$DOMAIN") {
				t.Fatalf("script derives its site path from the mutable domain:\n%s", test.script)
			}
			if !strings.Contains(test.script, "$FLUXO_SITE_PATH") {
				t.Fatalf("script does not use the stable site path:\n%s", test.script)
			}
		})
	}
}

func TestGeneratedLegacyNodeStrategiesRequirePackageManifest(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{name: "standard deploy", script: GenerateNodeDeployScript("standard")},
		{name: "zero downtime deploy", script: GenerateNodeDeployScript("zero-downtime")},
		{name: "standard rollback", script: GenerateNodeRollbackScript("standard")},
		{name: "zero downtime rollback", script: GenerateNodeRollbackScript("zero-downtime")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := exec.Command("bash", "-n")
			cmd.Stdin = strings.NewReader(test.script)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("Node strategy has invalid Bash syntax: %v\n%s", err, output)
			}

			guard := strings.Index(test.script, `if [ -f package.json ]; then`)
			install := strings.Index(test.script, `bash -lc "$FLUXO_NODE_INSTALL_COMMAND"`)
			build := strings.Index(test.script, `bash -lc "$FLUXO_NODE_BUILD_COMMAND"`)
			if guard < 0 || install < guard || build < guard {
				t.Fatalf("Node strategy is not protected by a package.json guard:\n%s", test.script)
			}
		})
	}
}
