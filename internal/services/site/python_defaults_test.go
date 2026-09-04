package site

import (
	"strings"
	"testing"

	"fluxo/internal/safeinput"
)

func TestPythonDefaults(t *testing.T) {
	if got := NormalizePythonPreset("FastAPI"); got != "fastapi" {
		t.Fatalf("preset = %q", got)
	}
	if got := DefaultPythonStartCommand("django", "project.wsgi:application"); got != ".venv/bin/gunicorn project.wsgi:application --bind 127.0.0.1:$FLUXO_APP_PORT" {
		t.Fatalf("Django command = %q", got)
	}
	if got := RenderPythonStartCommand("server --port ${FLUXO_APP_PORT}", 8123); got != "server --port 8123" {
		t.Fatalf("rendered command = %q", got)
	}
	if command := DefaultPythonBuildCommand("django"); command == "" || safeinput.HasControlChars(command) {
		t.Fatalf("Django build command must be nonempty and single-line: %q", command)
	}
	if command := DefaultPythonStartCommand("generic", ""); command != "" {
		t.Fatalf("generic start command without an entrypoint = %q", command)
	}
	if command := PythonInstallCommand("uv"); !strings.Contains(command, "uv sync --frozen") {
		t.Fatalf("uv install command does not respect the lockfile: %q", command)
	}
}

func TestPythonEntrypointValidation(t *testing.T) {
	for _, value := range []string{"config.wsgi:application", "app:app", "src.api.main:application"} {
		if !ValidatePythonEntrypoint(value) {
			t.Fatalf("expected %q to be valid", value)
		}
	}
	for _, value := range []string{"", "app", "app:app; rm -rf /", "../app:app"} {
		if ValidatePythonEntrypoint(value) {
			t.Fatalf("expected %q to be invalid", value)
		}
	}
}

func TestNormalizeAppDirectory(t *testing.T) {
	for input, want := range map[string]string{"": ".", ".": ".", "backend/api": "backend/api", "backend/../api": "api"} {
		got, err := NormalizeAppDirectory(input)
		if err != nil || got != want {
			t.Fatalf("NormalizeAppDirectory(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"/srv/app", "../secret", "backend/../../secret", "bad\npath", "backend;touch-pwned", "backend app"} {
		if _, err := NormalizeAppDirectory(input); err == nil {
			t.Fatalf("expected %q to be rejected", input)
		}
	}
}
