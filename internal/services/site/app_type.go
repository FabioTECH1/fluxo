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
	Domain           string
	PHPVersion       string
	WebRoot          string
	AppType          string
	AppPort          int
	DatabaseName     string
	DatabaseUser     string
	DatabasePassword string
	DatabaseEngine   string
	Repository       string
	Branch           string
	SSHKeyPath       string
}

type AppProvisioner interface {
	Provision(ctx context.Context, req ProvisionRequest) error
	DefaultWebRoot() string
	DefaultDeployScript(domain, branch, phpVersion string) string
	DefaultEnv(req ProvisionRequest) string
	LogSources(domain, phpVersion string) []LogSource
}

func Resolve(appType string) AppProvisioner {
	switch appType {
	case "laravel":
		return &LaravelApp{}
	case "php":
		return &PHPApp{}
	case "html":
		return &HTMLApp{}
	default:
		return &PHPApp{} // Fallback
	}
}
