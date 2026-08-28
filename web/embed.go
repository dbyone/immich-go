// Package webui embeds the compiled Immich web application (SvelteKit
// adapter-static output in web/build) and serves it as a single-binary
// SPA: real files straight from the embedded tree, everything else
// falling back to index.html so client-side routing works on deep links.
//
// The build directory ships with the repository; regenerate it with
// `corepack pnpm run build` inside web/ after pulling upstream changes.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:build
var buildFS embed.FS

// dist is the embedded build tree rooted at build/.
func dist() fs.FS {
	sub, err := fs.Sub(buildFS, "build")
	if err != nil {
		panic("webui: embedded build tree is missing: " + err.Error())
	}
	return sub
}

// HasIndex reports whether the embedded tree carries the SPA entry
// point (it always does when the package compiled with a real build).
func HasIndex() bool {
	_, err := fs.Stat(dist(), "index.html")
	return err == nil
}

// cachePolicy: hashed assets under _app/immutable are immutable;
// everything else (index.html, service worker, manifests) revalidates.
func cachePolicy(name string) string {
	if strings.HasPrefix(name, "_app/immutable/") {
		return "public, max-age=31536000, immutable"
	}
	return "no-cache"
}

// serveFile writes one embedded file, preferring the precompressed
// .br / .gz siblings the SvelteKit adapter ships next to every asset.
func serveFile(w http.ResponseWriter, r *http.Request, name string) {
	enc := ""
	data, err := fs.ReadFile(dist(), name)
	if err == nil && r.Method != http.MethodHead {
		if strings.Contains(r.Header.Get("Accept-Encoding"), "br") {
			if b, e := fs.ReadFile(dist(), name+".br"); e == nil {
				data, enc = b, "br"
			}
		}
		if enc == "" && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			if g, e := fs.ReadFile(dist(), name+".gz"); e == nil {
				data, enc = g, "gzip"
			}
		}
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if ct := mimeByExt(name); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if enc != "" {
		w.Header().Set("Content-Encoding", enc)
	}
	w.Header().Set("Cache-Control", cachePolicy(name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Vary", "Accept-Encoding")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}

// Handler serves the embedded SPA: exact files first, then index.html
// for client-side routes.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "." {
			serveFile(w, r, "index.html")
			return
		}
		if stat, err := fs.Stat(dist(), name); err == nil && !stat.IsDir() {
			serveFile(w, r, name)
			return
		}
		// SPA fallback: client-side routing handles the path.
		serveFile(w, r, "index.html")
	})
}

// ServeIndex writes the SPA entry point (used by the router's NotFound
// fallback for non-API deep links reached with methods chi routes miss).
func ServeIndex(w http.ResponseWriter, r *http.Request) {
	serveFile(w, r, "index.html")
}

func mimeByExt(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".avif":
		return "image/avif"
	case ".ico":
		return "image/x-icon"
	case ".woff2":
		return "font/woff2"
	case ".wasm":
		return "application/wasm"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".webmanifest":
		return "application/manifest+json"
	default:
		return ""
	}
}
