package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/radaiko/boxarr/internal/metadata/tmdb"
	"github.com/radaiko/boxarr/internal/metadata/tvdb"
	"github.com/radaiko/boxarr/internal/plex"
	"github.com/radaiko/boxarr/internal/prowlarr"
	"github.com/radaiko/boxarr/internal/torbox"
)

// testConnection probes one upstream with posted-or-saved credentials and a
// read-only call. A failing connection is a normal result (HTTP 200, ok:false),
// not a transport error (04 §10 C3–C8).
func (h *Handler) testConnection(w http.ResponseWriter, r *http.Request) {
	svc := chi.URLParam(r, "service")
	var body map[string]string
	_ = json.NewDecoder(r.Body).Decode(&body)
	get := func(k, saved string) string {
		if v := body[k]; v != "" {
			return v
		}
		return saved
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	s := h.deps.Settings

	var detail string
	var err error
	switch svc {
	case "torbox":
		c := torbox.New(get("token", s.TorBoxToken()))
		if base := s.TorBoxBaseURL(); base != "" {
			c = torbox.NewWithBaseURL(get("token", s.TorBoxToken()), base)
		}
		var u *torbox.User
		if u, err = c.UserMe(ctx); err == nil {
			detail = fmt.Sprintf("authenticated (plan %d)", int(u.Plan))
		}
	case "prowlarr":
		c := prowlarr.New(get("url", s.ProwlarrURL()), get("apiKey", s.ProwlarrAPIKey()))
		var ix []prowlarr.IndexerResource
		if ix, err = c.Indexers(ctx); err == nil {
			detail = fmt.Sprintf("%d indexers reachable", len(ix))
		}
	case "tmdb":
		tok := get("token", s.TMDBToken())
		c := tmdb.New(tok)
		if base := s.TMDBBaseURL(); base != "" {
			c = tmdb.NewWithBaseURL(tok, base)
		}
		if _, err = c.Configuration(ctx); err == nil {
			detail = "configuration fetched"
		}
	case "tvdb":
		key, pin := get("apiKey", s.TVDBAPIKey()), get("pin", s.TVDBPin())
		c := tvdb.New(key, pin)
		if base := s.TVDBBaseURL(); base != "" {
			c = tvdb.NewWithBaseURL(key, pin, base)
		}
		var types []tvdb.SeasonType
		if types, err = c.SeasonTypes(ctx); err == nil {
			detail = fmt.Sprintf("logged in (%d season types)", len(types))
		}
	case "plex":
		c := plex.New(get("url", s.PlexURL()), get("token", s.PlexToken()))
		var secs []plex.Section
		if secs, err = c.Sections(ctx); err == nil {
			detail = fmt.Sprintf("%d libraries", len(secs))
		}
	default:
		h.writeError(w, http.StatusBadRequest, "bad_request", "unknown service: "+svc)
		return
	}

	if err != nil {
		h.writeJSON(w, http.StatusOK, map[string]any{"ok": false, "service": svc, "detail": err.Error()})
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": svc, "detail": detail})
}
