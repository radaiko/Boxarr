package v1

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/radaiko/boxarr/internal/catalog"
	"github.com/radaiko/boxarr/internal/media"
)

// movieDTO is the camelCase wire shape for a movie (04-internal-api.md §4).
type movieDTO struct {
	ID                  int64     `json:"id"`
	TMDBID              int64     `json:"tmdbId"`
	IMDBID              string    `json:"imdbId,omitempty"`
	Title               string    `json:"title"`
	Year                int       `json:"year"`
	Overview            string    `json:"overview,omitempty"`
	Monitored           bool      `json:"monitored"`
	Status              string    `json:"status"`
	HasFile             bool      `json:"hasFile"`
	MinimumAvailability string    `json:"minimumAvailability"`
	ReleaseDate         string    `json:"releaseDate,omitempty"`
	Runtime             int       `json:"runtime,omitempty"`
	QualityProfileID    int64     `json:"qualityProfileId"`
	RootFolderPath      string    `json:"rootFolderPath"`
	LibraryPath         string    `json:"libraryPath,omitempty"`
	PosterPath          string    `json:"posterPath,omitempty"`
	BackdropPath        string    `json:"backdropPath,omitempty"`
	TMDBStatus          string    `json:"tmdbStatus,omitempty"`
	AddedAt             string    `json:"addedAt"`
	File                *fileMeta `json:"file,omitempty"`
}

func toMovieDTO(m *media.Movie) movieDTO {
	return movieDTO{
		ID: m.ID, TMDBID: m.TMDBID, IMDBID: m.IMDBID, Title: m.Title, Year: m.Year,
		Overview: m.Overview, Monitored: m.Monitored, Status: string(m.Status), HasFile: m.HasFile,
		MinimumAvailability: m.MinimumAvailability, ReleaseDate: m.ReleaseDate, Runtime: m.Runtime,
		QualityProfileID: m.QualityProfileID, RootFolderPath: m.RootFolderPath, LibraryPath: m.LibraryPath,
		PosterPath: m.PosterPath, BackdropPath: m.BackdropPath, TMDBStatus: m.TMDBStatus,
		AddedAt: rfc3339(m.AddedAt),
	}
}

func (h *Handler) listMovies(w http.ResponseWriter, r *http.Request) {
	movies, err := h.deps.Store.ListMovies(r.Context())
	if err != nil {
		h.serverError(w, "listing movies", err)
		return
	}
	items := make([]movieDTO, 0, len(movies))
	for _, m := range movies {
		items = append(items, toMovieDTO(m))
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (h *Handler) getMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	m, err := h.deps.Store.GetMovie(r.Context(), id)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "not_found", "movie not found")
		return
	}
	dto := toMovieDTO(m)
	dto.File = fileMetaFor(h.symlinkTargets(r.Context()), m.LibraryPath)
	h.writeJSON(w, http.StatusOK, dto)
}

func (h *Handler) lookupMovies(w http.ResponseWriter, r *http.Request) {
	term := r.URL.Query().Get("term")
	if term == "" {
		h.writeError(w, http.StatusBadRequest, "bad_request", "term is required")
		return
	}
	cands, err := h.deps.Catalog.LookupMovies(r.Context(), term)
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "upstream_unavailable", "tmdb: "+err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"items": cands})
}

func (h *Handler) addMovie(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TMDBID    int64 `json:"tmdbId"`
		Monitored *bool `json:"monitored"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TMDBID == 0 {
		h.writeError(w, http.StatusBadRequest, "bad_request", "tmdbId is required")
		return
	}
	monitored := true
	if body.Monitored != nil {
		monitored = *body.Monitored
	}
	m, err := h.deps.Catalog.AddMovie(r.Context(), body.TMDBID, monitored)
	if errors.Is(err, catalog.ErrAlreadyExists) {
		h.writeJSON(w, http.StatusConflict, map[string]any{
			"error": map[string]any{"code": "conflict", "message": "movie already in catalog",
				"details": map[string]any{"id": m.ID}},
		})
		return
	}
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "upstream_unavailable", err.Error())
		return
	}
	h.writeJSON(w, http.StatusCreated, toMovieDTO(m))
}

func (h *Handler) setMovieMonitored(w http.ResponseWriter, r *http.Request) {
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	var body struct {
		Monitored bool `json:"monitored"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	m, err := h.deps.Store.GetMovie(r.Context(), id)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "not_found", "movie not found")
		return
	}
	m.Monitored = body.Monitored
	if err := h.deps.Store.UpdateMovie(r.Context(), m); err != nil {
		h.writeError(w, http.StatusInternalServerError, "internal", "updating movie")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"id": id, "monitored": body.Monitored})
}

func (h *Handler) deleteMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	m, err := h.deps.Store.GetMovie(ctx, id)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "not_found", "movie not found")
		return
	}
	var jobIDs []int64
	if m.JobID != 0 {
		jobIDs = []int64{m.JobID}
	}
	if err := h.deps.Store.DeleteMovie(ctx, id); err != nil {
		h.writeError(w, http.StatusInternalServerError, "internal", "deleting movie")
		return
	}
	// Remove the download (TorBox + symlinks) as a visible background task.
	h.deleteDownloadsBackground(ctx, m.Title, jobIDs)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) idParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	return id, true
}
