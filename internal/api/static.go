package api

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// RegisterStaticFiles serves the embedded Astro frontend from the Go binary.
// Handles Astro's static output format (path/index.html) and SPA fallback.
func RegisterStaticFiles(router chi.Router, distFS embed.FS) {
	sub, err := fs.Sub(distFS, "web/dist")
	if err != nil {
		return
	}

	entries, err := fs.ReadDir(sub, ".")
	if err != nil || len(entries) == 0 {
		return
	}

	router.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Skip API and OpenAPI paths
		if strings.HasPrefix(path, "/api/") ||
			strings.HasPrefix(path, "/openapi") ||
			path == "/docs" {
			http.NotFound(w, r)
			return
		}

		// Normalize: strip trailing slash, remove leading slash
		cleanPath := strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/")
		if cleanPath == "" {
			cleanPath = "index.html"
		}

		// Try in order: exact file, path/index.html, path.html, SPA fallback
		for _, candidate := range []string{
			cleanPath,
			cleanPath + "/index.html",
			cleanPath + ".html",
			"index.html", // SPA fallback
		} {
			data, err := fs.ReadFile(sub, candidate)
			if err != nil {
				continue
			}

			setContentType(w, candidate)

			// Cache static assets with content hashes aggressively
			if strings.HasPrefix(candidate, "_astro/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}

			w.WriteHeader(http.StatusOK)
			w.Write(data)
			return
		}

		http.NotFound(w, r)
	})
}

func setContentType(w http.ResponseWriter, name string) {
	switch {
	case strings.HasSuffix(name, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(name, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(name, ".js"):
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case strings.HasSuffix(name, ".json"):
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	case strings.HasSuffix(name, ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
	case strings.HasSuffix(name, ".png"):
		w.Header().Set("Content-Type", "image/png")
	case strings.HasSuffix(name, ".woff2"):
		w.Header().Set("Content-Type", "font/woff2")
	case strings.HasSuffix(name, ".woff"):
		w.Header().Set("Content-Type", "font/woff")
	}
}
