// Package ui embeds the production Vue 3 frontend build (dist/) and serves
// it with History API fallback for client-side routing.
//
// The dist/ directory is produced by "cd ui && npm run build" and must
// exist before building the Go binary (it's embedded at compile time).
// At runtime, unknown paths are redirected to index.html so Vue Router
// can handle them client-side.
//
// Static assets (JS, CSS, images, fonts) are served with long-lived
// Cache-Control headers since they are content-hashed by Vite at build
// time and never change without a new binary deploy.
package ui

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"
)

//go:embed dist/*
var distFS embed.FS

// DistHandler returns an http.Handler that serves the embedded Vue SPA
// and gracefully falls back to index.html for unknown routes (History API).
func DistHandler() http.Handler {
	fSys, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}

	fileServer := http.FileServer(http.FS(fSys))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := path.Clean(r.URL.Path)

		// Check if the file exists in the embedded filesystem
		if f, err := fSys.Open(strings.TrimPrefix(p, "/")); err == nil {
			f.Close()
			setCacheHeaders(w, p)
			fileServer.ServeHTTP(w, r)
			return
		}

		// Fallback to index.html for Vue SPA routing
		w.Header().Set("Cache-Control", "no-cache")
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

func setCacheHeaders(w http.ResponseWriter, p string) {
	ext := strings.ToLower(filepath.Ext(p))
	switch ext {
	case ".js", ".css", ".woff", ".woff2":
		w.Header().Set("Cache-Control", "public, max-age=604800")
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".svg":
		w.Header().Set("Cache-Control", "public, max-age=86400")
	default:
		w.Header().Set("Cache-Control", "no-cache")
	}
}

func init() {
	// Register common MIME types for the embedded file server.
	mime.AddExtensionType(".js", "application/javascript")
	mime.AddExtensionType(".css", "text/css")
	mime.AddExtensionType(".svg", "image/svg+xml")
}
