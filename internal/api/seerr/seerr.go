// Package seerr emulates Sonarr v3 and Radarr v3 well enough for Overseerr and
// Jellyseerr to add series and movies to Boxarr (docs/specs/05). It exposes two
// surfaces mounted at /sonarr/api/v3 and /radarr/api/v3.
package seerr

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/radaiko/boxarr/internal/catalog"
	"github.com/radaiko/boxarr/internal/settings"
	"github.com/radaiko/boxarr/internal/store"
)

// Kind selects which Servarr flavor a router emulates.
type Kind string

const (
	KindSonarr Kind = "sonarr"
	KindRadarr Kind = "radarr"

	sonarrVersion = "3.0.10.1567" // a 3.x string keeps both Overseerr + Jellyseerr happy (05 §2.1)
	radarrVersion = "3.0.0.0"
)

// Deps are the dependencies the emulation needs.
type Deps struct {
	Store    *store.Store
	Settings *settings.Store
	Catalog  *catalog.Service
	Logger   *slog.Logger
}

// Handler serves one Servarr-flavored surface.
type Handler struct {
	kind Kind
	deps Deps
}

// NewRouter builds the chi subrouter for the given kind, mounted at
// /sonarr/api/v3 or /radarr/api/v3 by the parent server.
func NewRouter(kind Kind, deps Deps) http.Handler {
	h := &Handler{kind: kind, deps: deps}
	r := chi.NewRouter()
	r.Use(h.logRequest)  // first: log every arriving request (diagnose Overseerr reachability)
	r.Use(lowercasePath) // Seerr uses /qualityProfile; we register lowercase
	r.Use(h.auth)
	r.Get("/system/status", h.systemStatus)
	r.Get("/qualityprofile", h.qualityProfiles)
	r.Get("/rootfolder", h.rootFolders)
	r.Get("/tag", h.tags)
	r.Get("/languageprofile", h.languageProfiles) // Sonarr surface; harmless on Radarr
	r.Post("/command", h.command)
	if kind == KindSonarr {
		r.Get("/series/lookup", h.seriesLookup)
		r.Post("/series", h.addSeries)
	} else {
		r.Get("/movie/lookup", h.movieLookup)
		r.Post("/movie", h.addMovie)
	}
	return r
}

// logRequest logs every request reaching the Radarr/Sonarr emulation (visible in
// the Logs view), so you can tell whether Overseerr/Jellyseerr is reaching Boxarr
// at all (no log = a network/URL problem) and whether its API key matched.
func (h *Handler) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-Api-Key")
		if key == "" {
			key = r.URL.Query().Get("apikey")
		}
		slog.Default().Info("seerr request",
			"kind", string(h.kind), "method", r.Method, "path", r.URL.Path,
			"keyProvided", key != "", "keyValid", h.keyMatches(key), "remote", r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

// lowercasePath lower-cases the request path so Overseerr/Jellyseerr's camelCase
// endpoints (e.g. /qualityProfile, /rootFolder, /languageProfile) resolve to the
// lowercase routes we register. This router is MOUNTED, so chi matches sub-routes
// against the RouteContext's RoutePath — not r.URL.Path — so we must lower-case
// RoutePath too, or the camelCase paths 404 ("Failed to retrieve profiles").
func lowercasePath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = toLowerASCII(r.URL.Path)
		if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePath != "" {
			rctx.RoutePath = toLowerASCII(rctx.RoutePath)
		}
		next.ServeHTTP(w, r)
	})
}

func toLowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// auth accepts the api key via X-Api-Key header OR ?apikey= query (constant-time,
// no early-return across keys). Fail-closed when no keys are configured.
func (h *Handler) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Api-Key")
		if got == "" {
			got = r.URL.Query().Get("apikey")
		}
		if !h.keyMatches(got) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) keyMatches(got string) bool {
	matched := false
	for _, k := range h.deps.Settings.SeerrAPIKeys() {
		if k != "" && subtle.ConstantTimeCompare([]byte(got), []byte(k)) == 1 {
			matched = true // no early return: don't leak which/how-many keys
		}
	}
	return matched
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.deps.Logger.Error("seerr: encoding response", "error", err)
	}
}

// systemStatus advertises a Sonarr 3.x / Radarr v3 status (05 §2.1/§3.1).
func (h *Handler) systemStatus(w http.ResponseWriter, _ *http.Request) {
	app, version, urlBase := "Sonarr", sonarrVersion, "/sonarr"
	if h.kind == KindRadarr {
		app, version, urlBase = "Radarr", radarrVersion, "/radarr"
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"appName": app, "instanceName": app, "version": version,
		"buildTime": "2021-10-10T00:00:00Z", "isProduction": true, "isDocker": true,
		"authentication": "apikey", "urlBase": urlBase, "runtimeName": "netcore",
		"packageUpdateMechanism": "docker", "startTime": "2021-10-10T00:00:00Z",
	})
}

func (h *Handler) qualityProfiles(w http.ResponseWriter, r *http.Request) {
	profs, _ := h.deps.Store.ListQualityProfiles(r.Context())
	out := make([]map[string]any, 0, len(profs))
	for _, p := range profs {
		out = append(out, map[string]any{"id": p.ID, "name": p.Name})
	}
	if len(out) == 0 {
		out = append(out, map[string]any{"id": 1, "name": "Any"})
	}
	h.writeJSON(w, http.StatusOK, out)
}

func (h *Handler) rootFolders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var paths []string
	seen := map[string]bool{}
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	if h.kind == KindRadarr {
		folders, _ := h.deps.Store.ListRootFolders(ctx, "movie")
		for _, f := range folders {
			add(f.Path)
		}
		add(h.deps.Settings.MovieLibraryRoot())
	} else {
		folders, _ := h.deps.Store.ListRootFolders(ctx, "tv")
		for _, f := range folders {
			add(f.Path)
		}
		add(h.deps.Settings.TVLibraryRoot())
		// Expose the anime library root too, so Overseerr/Jellyseerr can configure a
		// separate anime root folder and route anime requests there.
		add(h.deps.Settings.AnimeLibraryRoot())
	}
	out := make([]map[string]any, 0, len(paths))
	for i, p := range paths {
		out = append(out, rootFolderShape(int64(i+1), p))
	}
	h.writeJSON(w, http.StatusOK, out)
}

func rootFolderShape(id int64, path string) map[string]any {
	free, total, accessible := diskStats(path)
	return map[string]any{
		"id": id, "path": path, "accessible": accessible,
		"freeSpace": free, "totalSpace": total, "unmappedFolders": []any{},
	}
}

func (h *Handler) tags(w http.ResponseWriter, r *http.Request) {
	tags, _ := h.deps.Store.ListTags(r.Context())
	out := make([]map[string]any, 0, len(tags))
	for _, t := range tags {
		out = append(out, map[string]any{"id": t.ID, "label": t.Label})
	}
	h.writeJSON(w, http.StatusOK, out)
}

func (h *Handler) languageProfiles(w http.ResponseWriter, _ *http.Request) {
	h.writeJSON(w, http.StatusOK, []map[string]any{{"id": 1, "name": "English"}})
}

// command is a fire-and-forget no-op (the actual search is triggered by addOptions).
func (h *Handler) command(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	name, _ := body["name"].(string)
	h.writeJSON(w, http.StatusCreated, map[string]any{"id": 1, "name": name, "status": "queued"})
}

// contextT aliases context.Context for the search-on-add callbacks.
type contextT = context.Context

// searchOnAdd kicks off a wanted search asynchronously (the HTTP add returns
// immediately, matching real Sonarr/Radarr).
func (h *Handler) searchOnAdd(ctx context.Context, fn func(contextT) error) {
	bg := context.WithoutCancel(ctx)
	go func() {
		if err := fn(bg); err != nil {
			h.deps.Logger.Warn("seerr: search-on-add failed", "error", err)
		}
	}()
}
