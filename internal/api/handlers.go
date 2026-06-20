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
}

// NewServer constructs a Server.
func NewServer(st *store.Store, cfg *config.Config, logger *slog.Logger) *Server {
	return &Server{store: st, cfg: cfg, logger: logger}
}

// SetHealth attaches a health checker for the /healthz endpoint.
func (s *Server) SetHealth(c Checker) { s.health = c }

// SetHealReporter attaches the healer's status source (used by Phase 0c).
func (s *Server) SetHealReporter(r HealReporter) { s.healReporter = r }

// Router builds the chi router. Phase 0a serves only /healthz; /api/v1 and the
// Sonarr/Radarr v3 emulation are mounted in Phase 0c.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", s.handleHealthz)
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
