package site

import (
	"context"
	"fluxo/internal/services/nginx"
)

// Provision orchestrates site setup: directory structure, Nginx, PHP pool, .env, and ownership.
func Provision(ctx context.Context, req ProvisionRequest) error {
	nginx.EnsureDirs()

	p := Resolve(req.AppType)
	return p.Provision(ctx, req)
}
