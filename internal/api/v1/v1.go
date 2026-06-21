// Package v1 serves Boxarr's own JSON/REST API under /api/v1, consumed by the
// React SPA. Phase 0c implements the chassis (auth, error envelope) plus the
// status, account, and settings endpoints; catalog/search land in Phase 1.
package v1

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/radaiko/boxarr/internal/catalog"
	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/settings"
	"github.com/radaiko/boxarr/internal/store"
)

// HealReporter exposes a worker loop's last/next run times (satisfied by worker.Workers).
type HealReporter interface {
	HealRunInfo() (last, next time.Time)
}

// Reconciler triggers an out-of-band WebDAV/mylist reconcile (satisfied by worker.Workers).
type Reconciler interface {
	Reconcile(ctx context.Context) error
}

// Adopter imports an already-present unknown WebDAV folder into the library
// (satisfied by worker.Workers).
type Adopter interface {
	AdoptUnknown(ctx context.Context, remotePath, name, kind string, tmdbID int64) error
}

// Deleter tears down a tracked download's import (symlinks + catalog + job) so
// deleting it from the mount leaves nothing dangling (satisfied by worker.Workers).
type Deleter interface {
	RemoveImport(ctx context.Context, jobID int64)
}

// Deps are the dependencies the /api/v1 handler needs.
type Deps struct {
	Store      *store.Store
	Settings   *settings.Store
	Catalog    *catalog.Service
	Health     HealReporter
	Reconciler Reconciler
	Adopter    Adopter
	Deleter    Deleter
	Logger     *slog.Logger
	Version    string
}

// Handler serves the /api/v1 surface.
type Handler struct {
	deps Deps
}

// NewHandler constructs the /api/v1 handler.
func NewHandler(d Deps) *Handler { return &Handler{deps: d} }

// Router returns the /api/v1 subrouter (mounted at /api/v1 by the parent server).
func (h *Handler) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(h.auth)
	r.Get("/status", h.status)
	r.Get("/account", h.account)
	r.Get("/settings", h.getSettings)
	r.Put("/settings", h.putSettings)
	r.Post("/settings/test/{service}", h.testConnection)
	r.Post("/plex/pin", h.plexPin)
	r.Get("/plex/pin/{id}", h.plexPinCheck)
	r.Get("/plex/servers", h.plexServers)
	r.Get("/plex/sections", h.plexSections)
	r.Get("/movies", h.listMovies)
	r.Get("/movies/lookup", h.lookupMovies)
	r.Get("/movies/{id}", h.getMovie)
	r.Post("/movies", h.addMovie)
	r.Put("/movies/{id}/monitored", h.setMovieMonitored)
	r.Delete("/movies/{id}", h.deleteMovie)
	r.Get("/movies/{id}/search", h.searchMovie)
	r.Post("/movies/{id}/grab", h.grabMovie)
	r.Get("/search", h.freeSearch)
	r.Get("/storage", h.storage)
	r.Get("/webdav", h.listWebDAV)
	r.Post("/webdav/refresh", h.refreshWebDAV)
	r.Post("/webdav/delete", h.deleteWebDAV)
	r.Post("/webdav/{id}/adopt", h.adoptWebDAV)
	r.Get("/series", h.listSeries)
	r.Get("/series/lookup", h.lookupSeries)
	r.Get("/series/{id}", h.getSeries)
	r.Post("/series", h.addSeries)
	r.Put("/series/{id}/monitored", h.setSeriesMonitored)
	r.Put("/series/{id}/seasons/{season}/monitored", h.setSeasonMonitored)
	r.Put("/series/{id}/episodes/{episodeId}/monitored", h.setEpisodeMonitored)
	r.Delete("/series/{id}", h.deleteSeries)
	r.Get("/series/{id}/seasons/{season}/search", h.searchSeason)
	r.Get("/series/{id}/episodes/{episodeId}/search", h.searchEpisode)
	r.Post("/series/{id}/grab", h.grabSeries)
	r.Get("/notifications", h.listNotifications)
	r.Get("/notifications/unread-count", h.unreadCount)
	r.Put("/notifications/{id}/read", h.markNotificationRead)
	r.Put("/notifications/read-all", h.markAllNotificationsRead)
	r.Post("/notifications/{id}/action", h.notificationAction)
	return r
}

// auth gates /api/v1. When no API key is configured the instance is open — this
// is the just-installed state and keeps the SPA usable over the LAN. Once a key
// is set every request must present it via X-Api-Key (the SPA stores the key and
// sends it). Set a key (and/or front Boxarr with a reverse proxy) before exposing
// it beyond a trusted network.
func (h *Handler) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := h.deps.Settings.APIKey()
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Api-Key")), []byte(key)) != 1 {
			h.writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing api key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// status reports version, worker run times, and catalog/job counts.
func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	series, _ := h.deps.Store.ListSeries(ctx)
	movies, _ := h.deps.Store.ListMovies(ctx)
	unread, _ := h.deps.Store.UnreadCount(ctx)
	active, _ := h.deps.Store.CountJobsByState(ctx,
		job.StateSubmitting, job.StateQueued, job.StateDownloading, job.StateSeeding)
	// Anime is a series subtype; count it as its own library section.
	anime := 0
	for _, s := range series {
		if s.SeriesType == "anime" {
			anime++
		}
	}
	resp := map[string]any{
		"version": h.deps.Version,
		"counts": map[string]any{
			"series":              len(series) - anime,
			"anime":               anime,
			"movies":              len(movies),
			"activeJobs":          active,
			"unreadNotifications": unread,
		},
	}
	if h.deps.Health != nil {
		last, next := h.deps.Health.HealRunInfo()
		resp["healer"] = map[string]any{"lastRun": rfc3339(last), "nextRun": rfc3339(next)}
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// account proxies the TorBox /user/me plan + usage.
func (h *Handler) account(w http.ResponseWriter, r *http.Request) {
	u, err := h.deps.Settings.TorBox().UserMe(r.Context())
	if err != nil {
		h.deps.Logger.Warn("account: torbox /user/me failed", "error", err)
		h.writeError(w, http.StatusBadGateway, "upstream_unavailable", "torbox: "+err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"plan":                   int(u.Plan),
		"isSubscribed":           u.IsSubscribed,
		"monthlyDownloadedBytes": u.TotalDownloaded,
		"cooldownUntil":          u.CooldownUntil,
		"premiumExpiresAt":       u.PremiumExpiresAt,
	})
}

// getSettings returns operator overrides + which secrets are configured.
func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) {
	s := h.deps.Settings
	h.writeJSON(w, http.StatusOK, map[string]any{
		"settings":   s.Redacted(),
		"effective":  s.EffectiveNonSecret(),
		"configured": s.Configured(),
	})
}

// putSettings persists operator overrides (flat key→value) to the settings table.
func (h *Handler) putSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Settings map[string]string `json:"settings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	for k, v := range body.Settings {
		if !settings.Writable(k) {
			h.writeError(w, http.StatusBadRequest, "bad_request", "unknown setting key: "+k)
			return
		}
		if err := h.deps.Settings.Set(r.Context(), k, v); err != nil {
			h.deps.Logger.Error("settings: persist failed", "key", k, "error", err)
			h.writeError(w, http.StatusInternalServerError, "internal", "persisting settings")
			return
		}
	}
	h.getSettings(w, r)
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.deps.Logger.Error("writing response", "error", err)
	}
}

// serverError logs the underlying cause (so 500s are diagnosable from the
// container logs) and returns a generic 500 to the client.
func (h *Handler) serverError(w http.ResponseWriter, msg string, err error) {
	h.deps.Logger.Error(msg, "error", err)
	h.writeError(w, http.StatusInternalServerError, "internal", msg)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, code, msg string) {
	h.writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": msg},
	})
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
