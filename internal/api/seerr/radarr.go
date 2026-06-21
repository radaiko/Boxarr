package seerr

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/radaiko/boxarr/internal/catalog"
)

// movieLookup answers GET /radarr/api/v3/movie/lookup?term=tmdb:{id}.
func (h *Handler) movieLookup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tmdbID := parseTermID(r.URL.Query().Get("term"), "tmdb")
	if tmdbID == 0 {
		h.writeJSON(w, http.StatusOK, []any{})
		return
	}
	d, err := h.deps.Settings.TMDB().MovieDetails(ctx, tmdbID)
	if err != nil {
		h.writeJSON(w, http.StatusOK, []any{})
		return
	}
	obj := map[string]any{
		"title": d.Title, "tmdbId": d.ID, "imdbId": d.IMDBID, "year": yearOf(d.ReleaseDate),
		"titleSlug": slug(d.Title) + "-" + strconv.Itoa(d.ID), "isAvailable": false,
		"monitored": false, "hasFile": false, "id": 0,
		"images": []map[string]any{{"coverType": "poster", "url": h.deps.Settings.TMDB().ImageURL("w500", d.PosterPath)}},
	}
	if m, _ := h.deps.Store.GetMovieByTMDB(ctx, int64(d.ID)); m != nil {
		obj["id"] = m.ID
		obj["monitored"] = m.Monitored
		obj["hasFile"] = m.HasFile
	}
	h.writeJSON(w, http.StatusOK, []any{obj})
}

// addMovie answers POST /radarr/api/v3/movie.
func (h *Handler) addMovie(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		TMDBID     int    `json:"tmdbId"`
		Title      string `json:"title"`
		AddOptions struct {
			SearchForMovie bool `json:"searchForMovie"`
		} `json:"addOptions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TMDBID == 0 {
		h.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "tmdbId required"})
		return
	}
	m, err := h.deps.Catalog.AddMovie(ctx, int64(body.TMDBID), true)
	if err != nil && !errors.Is(err, catalog.ErrAlreadyExists) {
		h.deps.Logger.Error("seerr: add movie", "error", err)
		h.writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	if body.AddOptions.SearchForMovie {
		mid := m.ID
		h.searchOnAdd(ctx, func(c contextT) error { return h.deps.Catalog.SearchWantedForMovie(c, mid) })
	}
	h.writeJSON(w, http.StatusCreated, map[string]any{
		"id": m.ID, "title": m.Title, "tmdbId": m.TMDBID, "imdbId": m.IMDBID,
		"monitored": true, "hasFile": false, "isAvailable": false,
		"titleSlug":      slug(m.Title) + "-" + strconv.FormatInt(m.TMDBID, 10),
		"rootFolderPath": m.RootFolderPath, "minimumAvailability": m.MinimumAvailability,
		"folderName": m.LibraryPath, "added": rfc3339(m.AddedAt),
	})
}

// --- shared helpers ---

func yearOf(date string) int {
	if len(date) >= 4 {
		if y, err := strconv.Atoi(date[:4]); err == nil {
			return y
		}
	}
	return 0
}

func slug(title string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
