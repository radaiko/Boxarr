// Package v1 serves Boxarr's own JSON/REST API under /api/v1, consumed by the
// React SPA. Phase 0c implements the chassis (auth, error envelope) plus the
// status, account, and settings endpoints; catalog/search land in Phase 1.
package v1

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/radaiko/boxarr/internal/catalog"
	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/prowlarr"
	"github.com/radaiko/boxarr/internal/store"
	"github.com/radaiko/boxarr/internal/torbox"
)

// HealReporter exposes a worker loop's last/next run times (satisfied by worker.Workers).
type HealReporter interface {
	HealRunInfo() (last, next time.Time)
}

// Deps are the dependencies the /api/v1 handler needs.
type Deps struct {
	Store    *store.Store
	Cfg      *config.Config
	TorBox   *torbox.Client
	Prowlarr *prowlarr.Client
	Catalog  *catalog.Service
	Health   HealReporter
	Logger   *slog.Logger
	Version  string
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
	r.Get("/movies", h.listMovies)
	r.Get("/movies/lookup", h.lookupMovies)
	r.Get("/movies/{id}", h.getMovie)
	r.Post("/movies", h.addMovie)
	r.Put("/movies/{id}/monitored", h.setMovieMonitored)
	r.Delete("/movies/{id}", h.deleteMovie)
	r.Get("/movies/{id}/search", h.searchMovie)
	r.Get("/search", h.freeSearch)
	return r
}

// auth enforces the X-Api-Key. When no key is configured, loopback requests are
// allowed unauthenticated (single-operator local-first); once a key is set, every
// client must present it.
func (h *Handler) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := h.deps.Cfg.APIKey
		if key == "" {
			if isLoopback(r.RemoteAddr) {
				next.ServeHTTP(w, r)
				return
			}
			h.writeError(w, http.StatusUnauthorized, "unauthorized", "api key required for non-loopback clients")
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Api-Key")), []byte(key)) != 1 {
			h.writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or missing api key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// status reports version, worker run times, and catalog/job counts.
func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	series, _ := h.deps.Store.ListSeries(ctx)
	movies, _ := h.deps.Store.ListMovies(ctx)
	unread, _ := h.deps.Store.UnreadCount(ctx)
	active, _ := h.deps.Store.CountJobsByState(ctx,
		job.StateSubmitting, job.StateQueued, job.StateDownloading, job.StateSeeding)
	resp := map[string]any{
		"version": h.deps.Version,
		"counts": map[string]any{
			"series":              len(series),
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
	u, err := h.deps.TorBox.UserMe(r.Context())
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
	all, err := h.deps.Store.AllSettings(r.Context())
	if err != nil {
		h.deps.Logger.Error("settings: load failed", "error", err)
		h.writeError(w, http.StatusInternalServerError, "internal", "loading settings")
		return
	}
	c := h.deps.Cfg
	h.writeJSON(w, http.StatusOK, map[string]any{
		"settings": all,
		"configured": map[string]bool{
			"torbox":   c.TorBoxAPIToken != "",
			"prowlarr": c.ProwlarrAPIKey != "",
			"tmdb":     c.TMDBAPIKey != "",
			"tvdb":     c.TVDBEnabled(),
			"plex":     c.PlexEnabled(),
			"seerr":    c.SeerrEnabled(),
		},
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
		if err := h.deps.Store.SetSetting(r.Context(), k, v); err != nil {
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
