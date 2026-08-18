package site

import (
	"strings"
	"testing"
)

func TestApplicationEnvironmentsNeverFallbackToControlPlaneCredentials(t *testing.T) {
	laravelEnv := new(LaravelApp).DefaultEnv(ProvisionRequest{Domain: "app.example.com"})
	if strings.Contains(laravelEnv, "DB_USERNAME=fluxo") || strings.Contains(laravelEnv, "DB_PASSWORD=secret") {
		t.Fatalf("Laravel environment contains fallback control-plane credentials: %s", laravelEnv)
	}
	if phpEnv := new(PHPApp).DefaultEnv(ProvisionRequest{Domain: "app.example.com"}); phpEnv != "" {
		t.Fatalf("PHP site without a database should not receive database configuration: %s", phpEnv)
	}
	nodeEnv := new(NodeApp).DefaultEnv(ProvisionRequest{
		Domain:           "app.example.com",
		AppPort:          3000,
		DatabaseName:     "app_db",
		DatabaseUser:     "app_user",
		DatabasePassword: "dedicated-secret",
	})
	if !strings.Contains(nodeEnv, "DB_USERNAME=app_user") || !strings.Contains(nodeEnv, "DB_PASSWORD='dedicated-secret'") {
		t.Fatalf("Node environment did not preserve dedicated credentials: %s", nodeEnv)
	}
}

func TestDatabasePasswordsAreQuotedInGeneratedDotEnv(t *testing.T) {
	req := ProvisionRequest{
		Domain:           "app.example.com",
		DatabaseName:     "app_db",
		DatabaseUser:     "app_user",
		DatabasePassword: `space # dollar $ backslash \\ value`,
	}
	want := `DB_PASSWORD='space # dollar $ backslash \\ value'`
	if env := new(LaravelApp).DefaultEnv(req); !strings.Contains(env, want) {
		t.Fatalf("Laravel environment password was not safely quoted: %s", env)
	}
	if env := new(PHPApp).DefaultEnv(req); !strings.Contains(env, want) {
		t.Fatalf("PHP environment password was not safely quoted: %s", env)
	}
}
