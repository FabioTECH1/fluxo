package site

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"fluxo/internal/services/nginx"
	"fluxo/internal/syscmd"
)

type PythonApp struct{}

func (p *PythonApp) DefaultWebRoot() string { return "/" }

func (p *PythonApp) DefaultDeployScript(domain, branch, pythonVersion string) string {
	return `set -e

cd $FLUXO_SITE_PATH

if [ ! -d .git ]; then
  git init
  git remote add origin $FLUXO_REPO
  git fetch origin
  git checkout -f $FLUXO_BRANCH
else
  git pull origin $FLUXO_BRANCH
fi

bash -lc "$FLUXO_PYTHON_INSTALL_COMMAND"
if [ -n "$FLUXO_PYTHON_BUILD_COMMAND" ]; then
  bash -lc "$FLUXO_PYTHON_BUILD_COMMAND"
fi`
}

func (p *PythonApp) DefaultEnv(req ProvisionRequest) string {
	lines := []string{
		"PYTHONUNBUFFERED=1",
		fmt.Sprintf("PORT=%d", req.AppPort),
		"HOST=127.0.0.1",
	}
	if req.PythonPreset == "django" {
		lines = append(lines, "ALLOWED_HOSTS="+req.Domain)
	}
	if req.DatabaseName != "" {
		replacements := databaseDotEnvReplacements(req)
		for _, key := range []string{"DB_CONNECTION", "DB_HOST", "DB_PORT", "DB_DATABASE", "DB_USERNAME", "DB_PASSWORD"} {
			lines = append(lines, key+"="+replacements[key])
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func (p *PythonApp) LogSources(domain, sitePath, pythonVersion string) []LogSource {
	return []LogSource{
		{ID: "site-nginx-error", Label: "Nginx Error (" + domain + ")", Path: fmt.Sprintf("/var/log/nginx/%s.error.log", domain)},
		{ID: "site-nginx-access", Label: "Nginx Access (" + domain + ")", Path: fmt.Sprintf("/var/log/nginx/%s.access.log", domain)},
	}
}

func (p *PythonApp) Provision(ctx context.Context, req ProvisionRequest) error {
	if _, err := exec.LookPath("python3"); err != nil {
		return fmt.Errorf("Python application support is not installed. Install it from Runtime > Python before creating a Python site")
	}
	packageManager := NormalizePythonPackageManager(req.PackageManager)
	if packageManager == "uv" {
		if _, err := exec.LookPath("uv"); err != nil {
			return fmt.Errorf("uv is not installed. Repair Python application support from Runtime > Python")
		}
	}
	appDirectory, err := NormalizeAppDirectory(req.AppDirectory)
	if err != nil {
		return err
	}

	actLog := func(typ, summary string) {
		if req.ActivityLog != nil {
			req.ActivityLog(req.SiteID, typ, summary)
		}
	}

	siteDir := filepath.Join("/home/fluxo", req.Domain)
	if err := os.MkdirAll(siteDir, 0755); err != nil {
		return fmt.Errorf("failed to create site directory: %w", err)
	}
	if err := ensureSiteOwnedByFluxo(ctx, siteDir); err != nil {
		return err
	}

	workingDir := siteDir
	currentSymlink := ""
	if req.DeploymentStrategy == "zero-downtime" {
		workingDir, currentSymlink, err = PrepareZDDDirectory(ctx, req)
		if err != nil {
			return err
		}
	} else if req.Repository != "" {
		actLog("provision", "Cloning Git repository")
		repoURL := "git@github.com:" + req.Repository + ".git"
		gitEnv := []string{"GIT_SSH_COMMAND=ssh -o StrictHostKeyChecking=no -i " + req.SSHKeyPath}
		if _, statErr := os.Stat(filepath.Join(workingDir, ".git")); statErr == nil {
			existingRemote, remoteErr := syscmd.RunAsUserInDir(ctx, 10*time.Second, "fluxo", workingDir, "git", "remote", "get-url", "origin")
			if remoteErr != nil || strings.TrimSpace(existingRemote) != repoURL {
				return fmt.Errorf("site directory already contains a different Git repository")
			}
			for _, args := range [][]string{{"-C", workingDir, "fetch", "origin", req.Branch}, {"-C", workingDir, "checkout", "-f", "-B", req.Branch, "origin/" + req.Branch}} {
				if out, runErr := syscmd.RunEnvAsUser(ctx, 120*time.Second, "fluxo", gitEnv, "git", args...); runErr != nil {
					return fmt.Errorf("failed to refresh repository: %s %w", out, runErr)
				}
			}
		} else if os.IsNotExist(statErr) {
			if out, cloneErr := syscmd.RunEnvAsUser(ctx, 120*time.Second, "fluxo", gitEnv, "git", "clone", "-b", req.Branch, repoURL, workingDir); cloneErr != nil {
				return fmt.Errorf("failed to clone repository: %s %w", out, cloneErr)
			}
		} else {
			return fmt.Errorf("failed to inspect existing repository: %w", statErr)
		}
	}

	applicationDir := filepath.Join(workingDir, appDirectory)
	if err := os.MkdirAll(applicationDir, 0755); err != nil {
		return fmt.Errorf("failed to create Python application directory: %w", err)
	}
	if req.Repository == "" {
		actLog("provision", "Creating Python starter application")
		if err := writePythonStarter(applicationDir, req); err != nil {
			return err
		}
	}
	if err := ensureSiteOwnedByFluxo(ctx, siteDir); err != nil {
		return err
	}

	envPath := filepath.Join(siteDir, ".env")
	if _, statErr := os.Stat(envPath); os.IsNotExist(statErr) {
		envContent := p.DefaultEnv(req)
		for _, examplePath := range []string{filepath.Join(applicationDir, ".env.example"), filepath.Join(workingDir, ".env.example")} {
			if data, readErr := os.ReadFile(examplePath); readErr == nil {
				envContent = string(data)
				if req.DatabaseName != "" {
					envContent = mergeDotEnvValues(envContent, databaseDotEnvReplacements(req))
				}
				break
			}
		}
		if err := os.WriteFile(envPath, []byte(envContent), 0640); err != nil {
			return fmt.Errorf("failed to create Python environment file: %w", err)
		}
	}
	applicationEnv := filepath.Join(applicationDir, ".env")
	if applicationEnv != envPath {
		if info, statErr := os.Lstat(applicationEnv); statErr == nil && info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("refusing to replace the application's existing .env file")
		}
		_ = os.Remove(applicationEnv)
		if err := os.Symlink(envPath, applicationEnv); err != nil {
			return fmt.Errorf("failed to link Python environment file: %w", err)
		}
	}

	actLog("provision", "Creating Python virtual environment")
	installCommand := PythonInstallCommand(packageManager)
	if out, runErr := syscmd.RunAsUserInDir(ctx, 10*time.Minute, "fluxo", applicationDir, "bash", "-lc", installCommand); runErr != nil {
		return fmt.Errorf("failed to install Python dependencies: %s %w", out, runErr)
	}
	if buildCommand := strings.TrimSpace(req.BuildCommand); buildCommand != "" {
		actLog("provision", "Running Python build commands")
		if out, runErr := syscmd.RunAsUserInDir(ctx, 10*time.Minute, "fluxo", applicationDir, "bash", "-lc", buildCommand); runErr != nil {
			return fmt.Errorf("failed to build Python application: %s %w", out, runErr)
		}
	}

	if req.DeploymentStrategy == "zero-downtime" {
		_ = os.Remove(currentSymlink)
		if err := os.Symlink(workingDir, currentSymlink); err != nil {
			return fmt.Errorf("failed to activate Python release: %w", err)
		}
	}
	if err := ensureSiteOwnedByFluxo(ctx, siteDir); err != nil {
		return err
	}

	actLog("provision", "Configuring Nginx")
	webRoot := siteDir
	if req.DeploymentStrategy == "zero-downtime" {
		webRoot = filepath.Join(siteDir, "current")
	}
	nginxAppType := "python"
	if NormalizePythonPreset(req.PythonPreset) == "django" {
		nginxAppType = "python-django"
		webRoot = filepath.Join(webRoot, appDirectory)
	}
	if err := nginx.GenerateConfig(req.Domain, webRoot, "", nginxAppType, req.AppPort, "", ""); err != nil {
		return fmt.Errorf("failed to configure Nginx: %w", err)
	}
	return nil
}

func writePythonStarter(dir string, req ProvisionRequest) error {
	preset := NormalizePythonPreset(req.PythonPreset)
	requirements := "gunicorn>=23,<24\n"
	files := map[string]string{}
	switch preset {
	case "django":
		requirements = "Django>=5.2,<6.0\ngunicorn>=23,<24\n"
		djangoInit := ""
		if strings.EqualFold(req.DatabaseEngine, "mysql") {
			requirements += "PyMySQL>=1.1,<2\n"
			djangoInit = djangoPyMySQLInit
		} else if strings.EqualFold(req.DatabaseEngine, "postgres") || strings.EqualFold(req.DatabaseEngine, "pgsql") {
			requirements += "psycopg[binary]>=3.2,<4\n"
		}
		secret, err := pythonSecret()
		if err != nil {
			return err
		}
		files["manage.py"] = djangoManage
		files["config/__init__.py"] = djangoInit
		files["config/settings.py"] = fmt.Sprintf(djangoSettings, secret)
		files["config/urls.py"] = djangoURLs
		files["config/wsgi.py"] = djangoWSGI
	case "flask":
		requirements = "Flask>=3.1,<4\ngunicorn>=23,<24\n"
		files["app.py"] = flaskStarter
	case "fastapi":
		requirements = "fastapi>=0.115,<1\nuvicorn[standard]>=0.34,<1\n"
		files["main.py"] = fastAPIStarter
	default:
		files["app.py"] = genericWSGIStarter
	}
	files["requirements.txt"] = requirements
	for relative, content := range files {
		path := filepath.Join(dir, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		mode := os.FileMode(0644)
		if relative == "manage.py" {
			mode = 0755
		}
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			return fmt.Errorf("failed to create %s: %w", relative, err)
		}
	}
	return nil
}

func pythonSecret() (string, error) {
	value := make([]byte, 48)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate Django secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

const djangoManage = `#!/usr/bin/env python3
import os
import sys

if __name__ == "__main__":
    os.environ.setdefault("DJANGO_SETTINGS_MODULE", "config.settings")
    from django.core.management import execute_from_command_line
    execute_from_command_line(sys.argv)
`

const djangoPyMySQLInit = `import pymysql

pymysql.install_as_MySQLdb()
`

const djangoSettings = `import os
from pathlib import Path

BASE_DIR = Path(__file__).resolve().parent.parent
SECRET_KEY = %q
DEBUG = False
ALLOWED_HOSTS = [host.strip() for host in os.getenv("ALLOWED_HOSTS", "localhost").split(",") if host.strip()]
ROOT_URLCONF = "config.urls"
MIDDLEWARE = ["django.middleware.security.SecurityMiddleware", "django.middleware.common.CommonMiddleware"]
INSTALLED_APPS = ["django.contrib.staticfiles"]
TEMPLATES = []
WSGI_APPLICATION = "config.wsgi.application"
STATIC_URL = "/static/"
STATIC_ROOT = BASE_DIR / "staticfiles"
DEFAULT_AUTO_FIELD = "django.db.models.BigAutoField"

engine = os.getenv("DB_CONNECTION", "sqlite")
if engine == "mysql":
    DATABASES = {"default": {"ENGINE": "django.db.backends.mysql", "HOST": os.getenv("DB_HOST", "127.0.0.1"), "PORT": os.getenv("DB_PORT", "3306"), "NAME": os.getenv("DB_DATABASE", ""), "USER": os.getenv("DB_USERNAME", ""), "PASSWORD": os.getenv("DB_PASSWORD", "")}}
elif engine in {"pgsql", "postgres", "postgresql"}:
    DATABASES = {"default": {"ENGINE": "django.db.backends.postgresql", "HOST": os.getenv("DB_HOST", "127.0.0.1"), "PORT": os.getenv("DB_PORT", "5432"), "NAME": os.getenv("DB_DATABASE", ""), "USER": os.getenv("DB_USERNAME", ""), "PASSWORD": os.getenv("DB_PASSWORD", "")}}
else:
    DATABASES = {"default": {"ENGINE": "django.db.backends.sqlite3", "NAME": BASE_DIR / "db.sqlite3"}}
`

const djangoURLs = `from django.http import HttpResponse
from django.urls import path

urlpatterns = [path("", lambda request: HttpResponse("Fluxo Django application is running.\n", content_type="text/plain"))]
`

const djangoWSGI = `import os
from django.core.wsgi import get_wsgi_application

os.environ.setdefault("DJANGO_SETTINGS_MODULE", "config.settings")
application = get_wsgi_application()
`

const flaskStarter = `from flask import Flask

app = Flask(__name__)

@app.get("/")
def index():
    return "Fluxo Flask application is running.\n"
`

const fastAPIStarter = `from fastapi import FastAPI

app = FastAPI()

@app.get("/")
def index():
    return {"status": "ok", "platform": "Fluxo"}
`

const genericWSGIStarter = `def application(environ, start_response):
    body = b"Fluxo Python application is running.\n"
    start_response("200 OK", [("Content-Type", "text/plain; charset=utf-8"), ("Content-Length", str(len(body)))])
    return [body]
`
