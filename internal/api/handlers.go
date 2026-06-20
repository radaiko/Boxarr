// Package api serves Boxarr's HTTP surface. Phase 0a exposes only the /healthz
// liveness/readiness probe; the /api/v1 REST API (consumed by the React SPA)
// and the Sonarr/Radarr v3 Seerr-emulation surfaces are mounted in Phase 0c.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/store"
)

// Checker reports service health. api.Health satisfies it.
type Checker interface {
	Check(ctx context.Context) error
}

// HealReporter exposes the healer's schedule; consumed by the status surface
// added in Phase 0c.
type HealReporter interface {
	HealRunInfo() (last, next time.Time)
}

// Server holds dependencies for Boxarr's HTTP API. store/cfg/logger are wired at
// startup and consumed by the /api/v1 + Seerr surfaces added in Phase 0c.
type Server struct {
	store        *store.Store
	cfg          *config.Config
	logger       *slog.Logger
	health       Checker
	healReporter HealReporter
	v1           http.Handler
	sonarr       http.Handler
	radarr       http.Handler
	spa          http.Handler
}

// NewServer constructs a Server.
func NewServer(st *store.Store, cfg *config.Config, logger *slog.Logger) *Server {
	return &Server{store: st, cfg: cfg, logger: logger}
}

// SetHealth attaches a health checker for the /healthz endpoint.
func (s *Server) SetHealth(c Checker) { s.health = c }

// SetHealReporter attaches the healer's status source (used by Phase 0c).
func (s *Server) SetHealReporter(r HealReporter) { s.healReporter = r }

// SetV1Router attaches the /api/v1 REST surface (internal/api/v1) to mount.
func (s *Server) SetV1Router(h http.Handler) { s.v1 = h }

// SetSeerr attaches the Sonarr v3 + Radarr v3 emulation surfaces.
func (s *Server) SetSeerr(sonarr, radarr http.Handler) { s.sonarr, s.radarr = sonarr, radarr }

// SetSPA attaches the embedded React SPA handler, mounted last as the catch-all.
func (s *Server) SetSPA(h http.Handler) { s.spa = h }

// Router builds the chi router: /healthz, /api/v1 (when attached), and the
// embedded SPA as the catch-all. The Sonarr/Radarr v3 emulation mounts in Phase 3.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", s.handleHealthz)
	if s.v1 != nil {
		r.Mount("/api/v1", s.v1)
	}
	if s.sonarr != nil {
		r.Mount("/sonarr/api/v3", s.sonarr)
	}
	if s.radarr != nil {
		r.Mount("/radarr/api/v3", s.radarr)
	}
	if s.spa != nil {
		r.Handle("/*", s.spa) // last: API/health namespaces win first
	}
	return r
}

// handleHealthz answers the /healthz probe: 200 "ok" when healthy (or no checker
// is attached), 503 "unhealthy: <err>" otherwise.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if s.health == nil {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		return
	}
	if err := s.health.Check(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("unhealthy: " + err.Error()))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
