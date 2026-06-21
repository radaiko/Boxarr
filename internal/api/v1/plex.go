package v1

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/radaiko/boxarr/internal/plex"
	"github.com/radaiko/boxarr/internal/settings"
)

// plexClientID returns the stable per-install plex.tv client identifier,
// generating and persisting one on first use.
func (h *Handler) plexClientID(r *http.Request) string {
	cid := h.deps.Settings.PlexClientID()
	if cid == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		cid = "boxarr-" + hex.EncodeToString(b)
		if err := h.deps.Settings.Set(r.Context(), settings.KeyPlexClientID, cid); err != nil {
			h.deps.Logger.Error("plex: persisting client id", "error", err)
		}
	}
	return cid
}

// plexPin starts the official Plex login: returns a code + the app.plex.tv URL
// the user opens to authorize, plus the pin id the SPA then polls.
func (h *Handler) plexPin(w http.ResponseWriter, r *http.Request) {
	tv := plex.NewTV(h.plexClientID(r))
	pin, err := tv.CreatePin(r.Context())
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "upstream_unavailable", "plex.tv: "+err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"id": pin.ID, "code": pin.Code, "authUrl": tv.AuthURL(pin.Code)})
}

// plexPinCheck polls a login PIN; once authorized it saves the Plex token.
func (h *Handler) plexPinCheck(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "bad_request", "invalid pin id")
		return
	}
	token, err := plex.NewTV(h.plexClientID(r)).CheckPin(r.Context(), id)
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "upstream_unavailable", "plex.tv: "+err.Error())
		return
	}
	if token == "" {
		h.writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	if err := h.deps.Settings.Set(r.Context(), settings.KeyPlexToken, token); err != nil {
		h.serverError(w, "saving plex token", err)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"authenticated": true})
}

// plexServers lists the Plex Media Servers reachable with the saved token.
func (h *Handler) plexServers(w http.ResponseWriter, r *http.Request) {
	token := h.deps.Settings.PlexToken()
	if token == "" {
		h.writeError(w, http.StatusBadRequest, "bad_request", "sign in to Plex first")
		return
	}
	servers, err := plex.NewTV(h.plexClientID(r)).Servers(r.Context(), token)
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "upstream_unavailable", "plex.tv: "+err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"servers": servers})
}

// plexSections lists a server's libraries so the user can map movie/tv/anime.
// Uses ?url= (the chosen server) when provided, else the saved Plex URL.
func (h *Handler) plexSections(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if url == "" {
		url = h.deps.Settings.PlexURL()
	}
	token := h.deps.Settings.PlexToken()
	if url == "" || token == "" {
		h.writeError(w, http.StatusBadRequest, "bad_request", "a Plex server URL and sign-in are required")
		return
	}
	secs, err := plex.New(url, token).Sections(r.Context())
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "upstream_unavailable", "plex: "+err.Error())
		return
	}
	out := make([]map[string]any, 0, len(secs))
	for _, s := range secs {
		out = append(out, map[string]any{"key": s.Key, "title": s.Title, "type": s.Type})
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"sections": out})
}
