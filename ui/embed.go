package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
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
			fileServer.ServeHTTP(w, r)
			return
		}

		// Fallback to index.html for Vue SPA routing
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
