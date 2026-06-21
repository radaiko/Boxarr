package v1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/radaiko/boxarr/internal/catalog"
	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/prowlarr"
)

type seriesDTO struct {
	ID             int64       `json:"id"`
	TMDBID         int64       `json:"tmdbId"`
	TVDBID         int64       `json:"tvdbId,omitempty"`
	Title          string      `json:"title"`
	Year           int         `json:"year"`
	Overview       string      `json:"overview,omitempty"`
	Monitored      bool        `json:"monitored"`
	Status         string      `json:"status"`
	SeriesType     string      `json:"seriesType"`
	PosterPath     string      `json:"posterPath,omitempty"`
	RootFolderPath string      `json:"rootFolderPath"`
	LibraryPath    string      `json:"libraryPath,omitempty"`
	Seasons        []seasonDTO `json:"seasons,omitempty"`
}

type seasonDTO struct {
	SeasonNumber int          `json:"seasonNumber"`
	Monitored    bool         `json:"monitored"`
	Status       string       `json:"status"`
	Episodes     []episodeDTO `json:"episodes,omitempty"`
}

type episodeDTO struct {
	ID            int64  `json:"id"`
	SeasonNumber  int    `json:"seasonNumber"`
	EpisodeNumber int    `json:"episodeNumber"`
	Title         string `json:"title"`
	AirDate       string `json:"airDate,omitempty"`
	Status        string `json:"status"`
	Monitored     bool   `json:"monitored"`
	HasFile       bool   `json:"hasFile"`
}

func seriesRollup(seasons []seasonDTO) string {
	// Precedence: downloading > searching > expired_broken > wanted > available > missing.
	order := []string{"downloading", "searching", "expired_broken", "wanted", "available"}
	present := map[string]bool{}
	any := false
	for _, s := range seasons {
		for _, e := range s.Episodes {
			if !e.Monitored {
				continue
			}
			any = true
			present[e.Status] = true
		}
	}
	if !any {
		return "missing"
	}
	for _, st := range order {
		if present[st] {
			if st == "available" && (present["wanted"] || present["missing"]) {
				continue
			}
			return st
		}
	}
	return "missing"
}

func (h *Handler) listSeries(w http.ResponseWriter, r *http.Request) {
	all, err := h.deps.Store.ListSeries(r.Context())
	if err != nil {
		h.serverError(w, "listing series", err)
		return
	}
	ctx := r.Context()
	items := make([]seriesDTO, 0, len(all))
	for _, s := range all {
		// Roll up status from the series' episodes (cheap per-series query).
		eps, _ := h.deps.Store.ListEpisodes(ctx, s.ID)
		one := seasonDTO{}
		for _, e := range eps {
			one.Episodes = append(one.Episodes, episodeDTO{Status: string(e.Status), Monitored: e.Monitored})
		}
		items = append(items, seriesDTO{
			ID: s.ID, TMDBID: s.TMDBID, TVDBID: s.TVDBID, Title: s.Title, Year: s.Year,
			Monitored: s.Monitored, Status: seriesRollup([]seasonDTO{one}), SeriesType: s.SeriesType,
			PosterPath: s.PosterPath, RootFolderPath: s.RootFolderPath, LibraryPath: s.LibraryPath,
		})
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (h *Handler) getSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	s, err := h.deps.Store.GetSeries(ctx, id)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "not_found", "series not found")
		return
	}
	seasons, _ := h.deps.Store.ListSeasons(ctx, id)
	episodes, _ := h.deps.Store.ListEpisodes(ctx, id)
	bySeason := map[int][]episodeDTO{}
	for _, e := range episodes {
		bySeason[e.SeasonNumber] = append(bySeason[e.SeasonNumber], episodeDTO{
			ID: e.ID, SeasonNumber: e.SeasonNumber, EpisodeNumber: e.EpisodeNumber, Title: e.Title,
			AirDate: e.AirDate, Status: string(e.Status), Monitored: e.Monitored, HasFile: e.HasFile,
		})
	}
	dto := seriesDTO{
		ID: s.ID, TMDBID: s.TMDBID, TVDBID: s.TVDBID, Title: s.Title, Year: s.Year,
		Overview: s.Overview, Monitored: s.Monitored, SeriesType: s.SeriesType,
		PosterPath: s.PosterPath, RootFolderPath: s.RootFolderPath, LibraryPath: s.LibraryPath,
	}
	for _, sn := range seasons {
		sd := seasonDTO{SeasonNumber: sn.SeasonNumber, Monitored: sn.Monitored, Episodes: bySeason[sn.SeasonNumber]}
		sd.Status = seriesRollup([]seasonDTO{sd})
		dto.Seasons = append(dto.Seasons, sd)
	}
	dto.Status = seriesRollup(dto.Seasons)
	h.writeJSON(w, http.StatusOK, dto)
}

func (h *Handler) lookupSeries(w http.ResponseWriter, r *http.Request) {
	term := r.URL.Query().Get("term")
	if term == "" {
		h.writeError(w, http.StatusBadRequest, "bad_request", "term is required")
		return
	}
	cands, err := h.deps.Catalog.LookupSeries(r.Context(), term)
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "upstream_unavailable", "metadata: "+err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"items": cands})
}

func (h *Handler) addSeries(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TMDBID           int64  `json:"tmdbId"`
		Monitored        *bool  `json:"monitored"`
		MonitoredSeasons []int  `json:"monitoredSeasons"`
		SeriesType       string `json:"seriesType"` // "anime" | "standard" (default)
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TMDBID == 0 {
		h.writeError(w, http.StatusBadRequest, "bad_request", "tmdbId is required")
		return
	}
	monitored := true
	if body.Monitored != nil {
		monitored = *body.Monitored
	}
	s, err := h.deps.Catalog.AddSeries(r.Context(), body.TMDBID, monitored, body.MonitoredSeasons, body.SeriesType)
	if errors.Is(err, catalog.ErrAlreadyExists) {
		h.writeJSON(w, http.StatusConflict, map[string]any{
			"error": map[string]any{"code": "conflict", "message": "series already in catalog",
				"details": map[string]any{"id": s.ID}},
		})
		return
	}
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "upstream_unavailable", err.Error())
		return
	}
	h.writeJSON(w, http.StatusCreated, seriesDTO{
		ID: s.ID, TMDBID: s.TMDBID, TVDBID: s.TVDBID, Title: s.Title, Year: s.Year,
		Monitored: s.Monitored, Status: "missing", SeriesType: s.SeriesType,
		PosterPath: s.PosterPath, RootFolderPath: s.RootFolderPath,
	})
}

func (h *Handler) setSeriesMonitored(w http.ResponseWriter, r *http.Request) {
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
	s, err := h.deps.Store.GetSeries(r.Context(), id)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "not_found", "series not found")
		return
	}
	s.Monitored = body.Monitored
	if err := h.deps.Store.UpdateSeries(r.Context(), s); err != nil {
		h.writeError(w, http.StatusInternalServerError, "internal", "updating series")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"id": id, "monitored": body.Monitored})
}

// setSeasonMonitored toggles a season's monitored flag and cascades it to the
// season's episodes (re-deriving wanted/missing for aired, file-less ones).
func (h *Handler) setSeasonMonitored(w http.ResponseWriter, r *http.Request) {
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	season, err := strconv.Atoi(chi.URLParam(r, "season"))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "bad_request", "invalid season")
		return
	}
	var body struct {
		Monitored bool `json:"monitored"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	ctx := r.Context()
	seasons, _ := h.deps.Store.ListSeasons(ctx, id)
	var seasonID int64
	for _, s := range seasons {
		if s.SeasonNumber == season {
			seasonID = s.ID
		}
	}
	if seasonID == 0 {
		h.writeError(w, http.StatusNotFound, "not_found", "season not found")
		return
	}
	if err := h.deps.Store.SetSeasonMonitored(ctx, seasonID, body.Monitored); err != nil {
		h.writeError(w, http.StatusInternalServerError, "internal", "updating season")
		return
	}
	episodes, _ := h.deps.Store.ListEpisodes(ctx, id)
	for _, e := range episodes {
		if e.SeasonNumber == season {
			h.applyEpisodeMonitor(ctx, e, body.Monitored)
		}
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"seasonNumber": season, "monitored": body.Monitored})
}

// setEpisodeMonitored toggles one episode's monitored flag.
func (h *Handler) setEpisodeMonitored(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.idParam(w, r); !ok {
		return
	}
	epID, err := strconv.ParseInt(chi.URLParam(r, "episodeId"), 10, 64)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "bad_request", "invalid episode id")
		return
	}
	var body struct {
		Monitored bool `json:"monitored"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	ctx := r.Context()
	ep, err := h.deps.Store.GetEpisode(ctx, epID)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "not_found", "episode not found")
		return
	}
	h.applyEpisodeMonitor(ctx, ep, body.Monitored)
	h.writeJSON(w, http.StatusOK, map[string]any{"id": epID, "monitored": body.Monitored})
}

// applyEpisodeMonitor sets monitored and re-derives status: an aired, file-less,
// now-monitored episode becomes wanted; unmonitoring a non-available one → missing.
func (h *Handler) applyEpisodeMonitor(ctx context.Context, ep *media.Episode, monitored bool) {
	ep.Monitored = monitored
	if !ep.HasFile {
		today := time.Now().UTC().Format("2006-01-02")
		switch {
		case monitored && ep.AirDate != "" && ep.AirDate <= today:
			ep.Status = media.MediaWanted
		case !monitored:
			ep.Status = media.MediaMissing
		}
	}
	if err := h.deps.Store.UpdateEpisode(ctx, ep); err != nil {
		h.deps.Logger.Error("updating episode monitor", "episode_id", ep.ID, "error", err)
	}
}

func (h *Handler) deleteSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	if _, err := h.deps.Store.GetSeries(ctx, id); err != nil {
		h.writeError(w, http.StatusNotFound, "not_found", "series not found")
		return
	}
	// Mark each episode's linked download for deletion (deleter propagates to TorBox).
	episodes, _ := h.deps.Store.ListEpisodes(ctx, id)
	for _, e := range episodes {
		if e.JobID == 0 {
			continue
		}
		if jb, jerr := h.deps.Store.GetJob(ctx, e.JobID); jerr == nil && jb.State.CanTransitionTo(job.StateDeleted) {
			jb.State = job.StateDeleted
			_ = h.deps.Store.UpdateJob(ctx, jb)
		}
	}
	if err := h.deps.Store.DeleteSeries(ctx, id); err != nil {
		h.writeError(w, http.StatusInternalServerError, "internal", "deleting series")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) searchSeason(w http.ResponseWriter, r *http.Request) {
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	season, err := strconv.Atoi(chi.URLParam(r, "season"))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "bad_request", "invalid season")
		return
	}
	s, err := h.deps.Store.GetSeries(r.Context(), id)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "not_found", "series not found")
		return
	}
	q := fmt.Sprintf("%s S%02d", s.Title, season)
	h.runSearch(w, r, prowlarr.SearchParams{Query: q, Type: "tvsearch", Categories: []int{5000}})
}

func (h *Handler) searchEpisode(w http.ResponseWriter, r *http.Request) {
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	epID, err := strconv.ParseInt(chi.URLParam(r, "episodeId"), 10, 64)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "bad_request", "invalid episode id")
		return
	}
	ctx := r.Context()
	s, err := h.deps.Store.GetSeries(ctx, id)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "not_found", "series not found")
		return
	}
	ep, err := h.deps.Store.GetEpisode(ctx, epID)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "not_found", "episode not found")
		return
	}
	q := fmt.Sprintf("%s S%02dE%02d", s.Title, ep.SeasonNumber, ep.EpisodeNumber)
	h.runSearch(w, r, prowlarr.SearchParams{Query: q, Type: "tvsearch", Categories: []int{5000}})
}

// grabSeries grabs a chosen release for a series scope (episode | season | series).
func (h *Handler) grabSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	var body struct {
		ReleaseID string `json:"releaseId"`
		Scope     string `json:"scope"` // "episode" | "season" | "series"
		EpisodeID int64  `json:"episodeId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ReleaseID == "" {
		h.writeError(w, http.StatusBadRequest, "bad_request", "releaseId is required")
		return
	}
	ref, err := decodeReleaseID(body.ReleaseID)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "not_found", "release token expired; re-search")
		return
	}
	ctx := r.Context()
	if _, err := h.deps.Store.GetSeries(ctx, id); err != nil {
		h.writeError(w, http.StatusNotFound, "not_found", "series not found")
		return
	}
	mediaType, mediaRef := "series", id
	if body.Scope == "episode" && body.EpisodeID != 0 {
		mediaType, mediaRef = "episode", body.EpisodeID
	}
	j, deduped, err := h.grabRelease(ctx, ref, mediaType, mediaRef)
	if err != nil {
		h.writeError(w, http.StatusUnprocessableEntity, "unprocessable", err.Error())
		return
	}
	// Flag the targeted episode(s) searching.
	if mediaType == "episode" {
		if ep, eerr := h.deps.Store.GetEpisode(ctx, mediaRef); eerr == nil {
			if ep.Status == media.MediaWanted || ep.Status == media.MediaMissing {
				_ = h.deps.Store.SetEpisodeStatus(ctx, ep.ID, media.MediaSearching)
			}
		}
	}
	h.writeJSON(w, http.StatusAccepted, map[string]any{
		"jobId": j.ID, "state": string(j.State), "deduped": deduped,
	})
}
