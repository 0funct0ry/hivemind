package web

import (
	"embed"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler serves the embedded frontend, falling back to index.html for any
// path that isn't an asset (SPA client-side routing). In dev mode it instead
// reverse-proxies to devProxy so Vite HMR runs against the real API.
func Handler(dev bool, devProxy string) (http.Handler, error) {
	if dev {
		target, err := url.Parse(devProxy)
		if err != nil {
			return nil, err
		}
		return httputil.NewSingleHostReverseProxy(target), nil
	}

	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(sub))
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return nil, err
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			fileServer.ServeHTTP(w, r)
			return
		}
		if _, err := fs.Stat(sub, p); err == nil && p != "" {
			fileServer.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(index)
	}), nil
}
