package deploy

import (
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
