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
	"github.com/radaiko/boxarr/internal/task"
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
	ID             int64     `json:"id"`
	SeasonNumber   int       `json:"seasonNumber"`
	EpisodeNumber  int       `json:"episodeNumber"`
	Title          string    `json:"title"`
	AirDate        string    `json:"airDate,omitempty"`
	Status         string    `json:"status"`
	Monitored      bool      `json:"monitored"`
	HasFile        bool      `json:"hasFile"`
	LangMissing    bool      `json:"langMissing,omitempty"`
	LastError      string    `json:"lastError,omitempty"` // failure reason when status=failed
	AbsoluteNumber int       `json:"absoluteNumber,omitempty"`
	SceneSeason    int       `json:"sceneSeason,omitempty"` // TVDB scene season (0 = from air-date heuristic)
	SceneEpisode   int       `json:"sceneEpisode,omitempty"`
	File           *fileMeta `json:"file,omitempty"`
	LastSearched   string    `json:"lastSearched,omitempty"`
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
	targets := h.symlinkTargets(ctx)
	bySeason := map[int][]episodeDTO{}
	for _, e := range episodes {
		ed := episodeDTO{
			ID: e.ID, SeasonNumber: e.SeasonNumber, EpisodeNumber: e.EpisodeNumber, Title: e.Title,
			AirDate: e.AirDate, Status: string(e.Status), Monitored: e.Monitored, HasFile: e.HasFile,
			LangMissing:    e.LangMissing,
			AbsoluteNumber: e.AbsoluteNumber, SceneSeason: e.SceneSeason, SceneEpisode: e.SceneEpisode,
			File: fileMetaFor(targets, e.LibraryPath), LastSearched: rfc3339ptr(e.LastSearchedAt),
		}
		if e.Status == media.MediaFailed {
			ed.LastError = h.failReason(ctx, e.JobID)
		}
		bySeason[e.SeasonNumber] = append(bySeason[e.SeasonNumber], ed)
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

// setSeriesType converts a series between "standard" and "anime", relocating its
// library files to the matching library root.
func (h *Handler) setSeriesType(w http.ResponseWriter, r *http.Request) {
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	var body struct {
		SeriesType string `json:"seriesType"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil ||
		(body.SeriesType != "anime" && body.SeriesType != "standard") {
		h.writeError(w, http.StatusBadRequest, "bad_request", "seriesType must be 'anime' or 'standard'")
		return
	}
	if h.deps.Converter == nil {
		h.writeError(w, http.StatusServiceUnavailable, "unavailable", "converter not wired")
		return
	}
	if err := h.deps.Converter.ConvertSeriesType(r.Context(), id, body.SeriesType); err != nil {
		h.serverError(w, "converting series type", err)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"id": id, "seriesType": body.SeriesType})
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
// resetEpisode clears a failed episode grab: deletes the failed job, returns it
// to wanted, and re-searches the series so it can re-grab a different release.
func (h *Handler) resetEpisode(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.idParam(w, r); !ok {
		return
	}
	epID, err := strconv.ParseInt(chi.URLParam(r, "episodeId"), 10, 64)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "bad_request", "invalid episode id")
		return
	}
	ctx := r.Context()
	ep, err := h.deps.Store.GetEpisode(ctx, epID)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "not_found", "episode not found")
		return
	}
	if ep.JobID != 0 && h.deps.Deleter != nil {
		h.deps.Deleter.DeleteDownloads(ctx, []int64{ep.JobID}, func(int, int, string) {})
	}
	_ = h.deps.Store.SetEpisodeStatus(ctx, epID, media.MediaWanted)
	if h.deps.Catalog != nil {
		sid := ep.SeriesID
		h.runBackground("search", "Retry episode", func(ctx context.Context) error {
			return h.deps.Catalog.SearchWantedForSeries(ctx, sid)
		})
	}
	h.writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

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
	s, err := h.deps.Store.GetSeries(ctx, id)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "not_found", "series not found")
		return
	}
	episodes, _ := h.deps.Store.ListEpisodes(ctx, id)
	var jobIDs []int64
	for _, e := range episodes {
		if e.JobID != 0 {
			jobIDs = append(jobIDs, e.JobID)
		}
	}
	if err := h.deps.Store.DeleteSeries(ctx, id); err != nil {
		h.writeError(w, http.StatusInternalServerError, "internal", "deleting series")
		return
	}
	h.deleteDownloadsBackground(ctx, s.Title, jobIDs)
	w.WriteHeader(http.StatusNoContent)
}

// deleteDownloadsBackground removes the given downloads (TorBox + symlinks + job
// rows) as a visible background task. Falls back to marking them StateDeleted for
// the deleter worker when no task runner / deleter is wired (tests).
func (h *Handler) deleteDownloadsBackground(ctx context.Context, label string, jobIDs []int64) {
	if len(jobIDs) == 0 {
		return
	}
	if h.deps.Tasks != nil && h.deps.Deleter != nil {
		h.deps.Tasks.Enqueue("delete", label, func(tctx context.Context, run *task.Run) error {
			h.deps.Deleter.DeleteDownloads(tctx, jobIDs, func(done, total int, name string) {
				run.Progress(done, total)
				if name != "" {
					run.Detail(name)
				}
			})
			return nil
		})
		return
	}
	for _, jid := range jobIDs {
		if jb, jerr := h.deps.Store.GetJob(ctx, jid); jerr == nil && jb.State.CanTransitionTo(job.StateDeleted) {
			jb.State = job.StateDeleted
			_ = h.deps.Store.UpdateJob(ctx, jb)
		}
	}
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
	_ = h.deps.Store.MarkEpisodesSearched(ctx, ep.ID)
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
	// Flag the targeted episode queued (a job is now on TorBox).
	if mediaType == "episode" {
		if ep, eerr := h.deps.Store.GetEpisode(ctx, mediaRef); eerr == nil {
			if ep.Status != media.MediaAvailable {
				_ = h.deps.Store.SetEpisodeStatus(ctx, ep.ID, media.MediaQueued)
			}
		}
	}
	h.writeJSON(w, http.StatusAccepted, map[string]any{
		"jobId": j.ID, "state": string(j.State), "deduped": deduped,
	})
}
