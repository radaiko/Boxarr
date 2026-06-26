package seerr

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/radaiko/boxarr/internal/catalog"
	"github.com/radaiko/boxarr/internal/media"
)

// movieList answers GET /radarr/api/v3/movie (Overseerr's Radarr sync). It returns
// every catalog movie, or just the one matching ?tmdbId= (the existence check).
func (h *Handler) movieList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	movies, _ := h.deps.Store.ListMovies(ctx)
	tmdbFilter := r.URL.Query().Get("tmdbId")
	out := make([]map[string]any, 0, len(movies))
	for _, m := range movies {
		if tmdbFilter != "" && strconv.FormatInt(m.TMDBID, 10) != tmdbFilter {
			continue
		}
		out = append(out, h.movieResource(m))
	}
	h.writeJSON(w, http.StatusOK, out)
}

// movieByID answers GET /radarr/api/v3/movie/{id}.
func (h *Handler) movieByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	m, err := h.deps.Store.GetMovie(r.Context(), id)
	if err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	h.writeJSON(w, http.StatusOK, h.movieResource(m))
}

// movieResource maps a catalog movie to a Radarr v3 MovieResource (the subset
// Overseerr reads: tmdbId to match, hasFile/isAvailable for availability).
func (h *Handler) movieResource(m *media.Movie) map[string]any {
	res := map[string]any{
		"id": m.ID, "title": m.Title, "tmdbId": m.TMDBID, "imdbId": m.IMDBID,
		"year": m.Year, "monitored": m.Monitored, "hasFile": m.HasFile, "isAvailable": m.HasFile,
		"status": m.TMDBStatus, "qualityProfileId": m.QualityProfileID,
		"rootFolderPath": m.RootFolderPath, "folderName": m.LibraryPath,
		"titleSlug":           slug(m.Title) + "-" + strconv.FormatInt(m.TMDBID, 10),
		"minimumAvailability": m.MinimumAvailability, "added": rfc3339(m.AddedAt), "sizeOnDisk": 0,
		"images": []map[string]any{{"coverType": "poster", "url": h.deps.Settings.TMDB().ImageURL("w500", m.PosterPath)}},
	}
	if m.HasFile {
		res["movieFile"] = map[string]any{"id": m.ID, "relativePath": filepath.Base(m.LibraryPath), "size": 0}
	}
	return res
}

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
