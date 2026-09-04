package site

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const DefaultPythonPort = 8000

var pythonEntrypointPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*:[A-Za-z_][A-Za-z0-9_.]*$`)
var appDirectoryPattern = regexp.MustCompile(`^(?:[A-Za-z0-9._-]+/)*[A-Za-z0-9._-]+$`)

func NormalizePythonPreset(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "django", "flask", "fastapi", "generic":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "generic"
	}
}

func NormalizePythonPackageManager(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "uv":
		return "uv"
	default:
		return "pip"
	}
}

func DefaultPythonEntrypoint(preset string) string {
	switch NormalizePythonPreset(preset) {
	case "django":
		return "config.wsgi:application"
	case "flask":
		return "app:app"
	case "fastapi":
		return "main:app"
	default:
		return ""
	}
}

func ValidatePythonEntrypoint(value string) bool {
	return pythonEntrypointPattern.MatchString(strings.TrimSpace(value))
}

func NormalizeAppDirectory(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return ".", nil
	}
	if filepath.IsAbs(value) || strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("application directory must be a relative path")
	}
	clean := filepath.Clean(value)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("application directory cannot leave the site root")
	}
	if !appDirectoryPattern.MatchString(clean) {
		return "", fmt.Errorf("application directory may contain only letters, numbers, dots, dashes, underscores, and slashes")
	}
	return clean, nil
}

func DefaultPythonStartCommand(preset, entrypoint string) string {
	preset = NormalizePythonPreset(preset)
	entrypoint = strings.TrimSpace(entrypoint)
	if entrypoint == "" {
		entrypoint = DefaultPythonEntrypoint(preset)
	}
	switch preset {
	case "django", "flask":
		return ".venv/bin/gunicorn " + entrypoint + " --bind 127.0.0.1:$FLUXO_APP_PORT"
	case "fastapi":
		return ".venv/bin/uvicorn " + entrypoint + " --host 127.0.0.1 --port $FLUXO_APP_PORT"
	default:
		if entrypoint != "" {
			return ".venv/bin/gunicorn " + entrypoint + " --bind 127.0.0.1:$FLUXO_APP_PORT"
		}
		return ""
	}
}

func DefaultPythonBuildCommand(preset string) string {
	if NormalizePythonPreset(preset) != "django" {
		return ""
	}
	return `if [ -f manage.py ]; then .venv/bin/python manage.py collectstatic --noinput; fi`
}

func RenderPythonStartCommand(command string, appPort int) string {
	port := strconv.Itoa(appPort)
	command = strings.ReplaceAll(command, "${FLUXO_APP_PORT}", port)
	return strings.ReplaceAll(command, "$FLUXO_APP_PORT", port)
}

func PythonInstallCommand(packageManager string) string {
	if NormalizePythonPackageManager(packageManager) == "uv" {
		return `if [ -f uv.lock ] && [ -f pyproject.toml ]; then
  uv sync --frozen
elif [ -f requirements.txt ]; then
  if [ ! -x .venv/bin/python ]; then
    uv venv .venv
  fi
  uv pip install --python .venv/bin/python --requirement requirements.txt
elif [ -f pyproject.toml ]; then
  uv sync
else
  python3 -m venv .venv
fi`
	}
	return `python3 -m venv .venv
if [ -f requirements.txt ]; then
  .venv/bin/python -m pip install --requirement requirements.txt
elif [ -f pyproject.toml ]; then
  .venv/bin/python -m pip install .
fi`
}
