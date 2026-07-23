package site

import (
	"context"
)

type LogSource struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

type ProvisionRequest struct {
	Domain             string
	PHPVersion         string
	WebRoot            string
	AppType            string
	AppPort            int
	DatabaseName       string
	DatabaseUser       string
	DatabasePassword   string
	DatabaseEngine     string
	Repository         string
	Branch             string
	SSHKeyPath         string
	InstallComposer    bool
	DeploymentStrategy string
	NodePreset         string
	NodeMode           string
	PackageManager     string
	BuildCommand       string
	StartCommand       string
	StaticOutputDir    string
	SiteID             int
	ActivityLog        func(siteID int, typ, summary string)
}

type AppProvisioner interface {
	Provision(ctx context.Context, req ProvisionRequest) error
	DefaultWebRoot() string
	DefaultDeployScript(domain, branch, phpVersion string) string
	DefaultEnv(req ProvisionRequest) string
	LogSources(domain, sitePath, phpVersion string) []LogSource
}

// Resolve returns the AppProvisioner implementation for the given app type.
func Resolve(appType string) AppProvisioner {
	switch appType {
	case "laravel":
		return &LaravelApp{}
	case "php":
		return &PHPApp{}
	case "html":
		return &HTMLApp{}
	case "node":
		return &NodeApp{}
	case "wordpress":
		return &WordPressApp{}
	default:
		return &PHPApp{} // Fallback
	}
}
