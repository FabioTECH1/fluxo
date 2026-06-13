package site

import (
	"context"
	"fluxo/internal/services/nginx"
)

// Provision sets up the directory structure, Nginx configuration, PHP pool,
// database credentials in .env, and sets proper ownership.
func Provision(ctx context.Context, req ProvisionRequest) error {
	nginx.EnsureDirs()

	p := Resolve(req.AppType)
	return p.Provision(ctx, req)
}
