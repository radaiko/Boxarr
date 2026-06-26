package v1

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

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

// heavyAnalysisHints flags per-library prefs whose work reads the WHOLE media
// file — ruinous for TorBox-streamed content, which would be pulled in full
// through the rclone mount just to be analyzed.
var heavyAnalysisHints = []string{
	"bif",              // generate video preview thumbnails
	"markergeneration", // intro / credits / ad markers
	"introdetection", "creditsdetection", "addetection",
	"voiceactivity", // voice-activity analysis
	"loudness",      // audio loudness analysis
}

func isHeavyAnalysisPref(id string) bool {
	id = strings.ToLower(id)
	for _, h := range heavyAnalysisHints {
		if strings.Contains(id, h) {
			return true
		}
	}
	return false
}

// plexLibraryCheck validates each mapped Plex library for streamed-media use:
// it is mapped, its type matches, its scan path matches the Boxarr library root,
// and (the key check) expensive per-library analysis is turned off.
func (h *Handler) plexLibraryCheck(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if url == "" {
		url = h.deps.Settings.PlexURL()
	}
	token := h.deps.Settings.PlexToken()
	if url == "" || token == "" {
		h.writeError(w, http.StatusBadRequest, "bad_request", "a Plex server URL and sign-in are required")
		return
	}
	cl := plex.New(url, token)
	secs, err := cl.Sections(r.Context())
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "upstream_unavailable", "plex: "+err.Error())
		return
	}
	byKey := make(map[string]plex.Section, len(secs))
	for _, s := range secs {
		byKey[s.Key] = s
	}

	type cat struct {
		name, sectionID, wantType, root string
	}
	cats := []cat{
		{"Movies", h.deps.Settings.PlexMovieSection(), "movie", h.deps.Settings.MovieLibraryRoot()},
		{"Series", h.deps.Settings.PlexTVSection(), "show", h.deps.Settings.TVLibraryRoot()},
		{"Anime", h.deps.Settings.PlexAnimeSection(), "show", h.deps.Settings.AnimeLibraryRoot()},
	}

	results := make([]map[string]any, 0, len(cats))
	for _, c := range cats {
		res := map[string]any{"category": c.name}
		warnings := []string{}
		infos := []string{}
		if c.sectionID == "" {
			if c.name == "Anime" {
				res["status"] = "info"
				res["section"] = "—"
				res["warnings"] = []string{"No dedicated Anime library mapped; anime is filed into the Series library."}
				results = append(results, res)
				continue
			}
			res["status"] = "warn"
			res["section"] = "—"
			res["warnings"] = []string{"No Plex library mapped for this category."}
			results = append(results, res)
			continue
		}
		sec, ok := byKey[c.sectionID]
		if !ok {
			res["status"] = "warn"
			res["section"] = c.sectionID
			res["warnings"] = []string{"Mapped library no longer exists on this Plex server."}
			results = append(results, res)
			continue
		}
		res["section"] = sec.Title
		if sec.Type != c.wantType {
			warnings = append(warnings, fmt.Sprintf("Library type is %q but should be %q for this category.", sec.Type, c.wantType))
		}
		if c.root != "" && !plexLocationMatches(sec.Locations, c.root) {
			paths := make([]string, 0, len(sec.Locations))
			for _, l := range sec.Locations {
				paths = append(paths, l.Path)
			}
			if plexRemapsCleanly(sec.Locations, c.root) {
				// Different mount paths for the same storage — Boxarr auto-remaps the
				// post-import scan onto Plex's path, so this is informational, not a fault.
				infos = append(infos, fmt.Sprintf("Plex scans %s; Boxarr writes %q (same storage, different mount) and auto-remaps post-import scans to Plex's path.", strings.Join(paths, ", "), c.root))
			} else {
				warnings = append(warnings, fmt.Sprintf("Library scans %s but Boxarr writes to %q and can't map it unambiguously — Boxarr falls back to a full Plex section scan after each import (still works, just heavier).", strings.Join(paths, ", "), c.root))
			}
		}
		// The key check: expensive per-library analysis on streamed media.
		if prefs, perr := cl.SectionPrefs(r.Context(), c.sectionID); perr == nil {
			for _, p := range prefs {
				if isHeavyAnalysisPref(p.ID) && p.Truthy() {
					label := p.Label
					if label == "" {
						label = p.ID
					}
					warnings = append(warnings, fmt.Sprintf("\"%s\" is ON — it scans whole files (costly for streamed media); turn it off.", label))
				}
			}
		} else {
			warnings = append(warnings, "Could not read library settings: "+perr.Error())
		}
		switch {
		case len(warnings) > 0:
			res["status"] = "warn"
		case len(infos) > 0:
			res["status"] = "info"
		default:
			res["status"] = "ok"
		}
		res["warnings"] = warnings
		if len(infos) > 0 {
			res["infos"] = infos
		}
		results = append(results, res)
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// plexRemapsCleanly reports whether Boxarr's post-import scan can remap root onto
// one of the section's Plex locations (mirrors the worker's plexScanTarget rules:
// exact path, matching folder name, or a single location). When true, a path
// mismatch is handled automatically and is informational rather than a fault.
func plexRemapsCleanly(locs []plex.Location, root string) bool {
	root = strings.TrimRight(filepath.Clean(root), "/")
	if root == "" || len(locs) == 0 {
		return false
	}
	base := filepath.Base(root)
	for _, l := range locs {
		p := strings.TrimRight(filepath.Clean(l.Path), "/")
		if p == root || filepath.Base(p) == base {
			return true
		}
	}
	return len(locs) == 1
}

// plexLocationMatches reports whether any Plex scan location lines up with the
// Boxarr library root (exact, or one containing the other — tolerant of trailing
// slashes and nested roots).
func plexLocationMatches(locs []plex.Location, root string) bool {
	root = strings.TrimRight(filepath.Clean(root), "/")
	for _, l := range locs {
		p := strings.TrimRight(filepath.Clean(l.Path), "/")
		if p == root || strings.HasPrefix(root+"/", p+"/") || strings.HasPrefix(p+"/", root+"/") {
			return true
		}
	}
	return false
}
