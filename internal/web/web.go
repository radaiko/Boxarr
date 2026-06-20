// Package web embeds the built React SPA (web/dist, copied to dist/ at build
// time) and serves it with an index.html fallback so client-side routes resolve.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// SPAHandler serves the embedded Vite build. Any path that is not a real asset
// falls back to index.html so deep links (e.g. /movies/123) load the SPA shell.
// Mount it last in the router so the API/health namespaces win first.
func SPAHandler() http.Handler {
	sub, _ := fs.Sub(distFS, "dist") // dist is guaranteed to exist by go:embed
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" || p == "." {
			p = "index.html"
		}
		if _, err := fs.Stat(sub, p); err != nil {
			r.URL.Path = "/" // unknown path -> SPA shell
		}
		fileServer.ServeHTTP(w, r)
	})
}
