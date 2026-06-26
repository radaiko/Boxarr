package seerr

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/radaiko/boxarr/internal/catalog"
	"github.com/radaiko/boxarr/internal/media"
)

// seriesList answers GET /sonarr/api/v3/series (Overseerr's Sonarr sync). Returns
// every catalog series, or just the one matching ?tvdbId= (the existence check).
func (h *Handler) seriesList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	series, _ := h.deps.Store.ListSeries(ctx)
	tvdbFilter := r.URL.Query().Get("tvdbId")
	out := make([]map[string]any, 0, len(series))
	for _, s := range series {
		if tvdbFilter != "" && strconv.FormatInt(s.TVDBID, 10) != tvdbFilter {
			continue
		}
		eps, _ := h.deps.Store.ListEpisodes(ctx, s.ID)
		out = append(out, h.seriesResource(s, eps))
	}
	h.writeJSON(w, http.StatusOK, out)
}

// seriesByID answers GET /sonarr/api/v3/series/{id}.
func (h *Handler) seriesByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	ctx := r.Context()
	s, err := h.deps.Store.GetSeries(ctx, id)
	if err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	eps, _ := h.deps.Store.ListEpisodes(ctx, s.ID)
	h.writeJSON(w, http.StatusOK, h.seriesResource(s, eps))
}

// seriesResource maps a catalog series to a Sonarr v3 SeriesResource. Seasons are
// grouped by the TVDB scene season (when set) so per-season availability matches
// Overseerr's TVDB view; statistics.episodeFileCount drives "available".
func (h *Handler) seriesResource(s *media.Series, eps []*media.Episode) map[string]any {
	type agg struct{ total, withFile int }
	bySeason := map[int]*agg{}
	for _, e := range eps {
		sn := e.SeasonNumber
		if e.SceneSeason > 0 {
			sn = e.SceneSeason
		}
		a := bySeason[sn]
		if a == nil {
			a = &agg{}
			bySeason[sn] = a
		}
		a.total++
		if e.HasFile {
			a.withFile++
		}
	}
	nums := make([]int, 0, len(bySeason))
	for n := range bySeason {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	seasons := make([]map[string]any, 0, len(nums))
	totalEps, totalFiles := 0, 0
	for _, n := range nums {
		a := bySeason[n]
		totalEps += a.total
		totalFiles += a.withFile
		seasons = append(seasons, map[string]any{
			"seasonNumber": n, "monitored": s.Monitored,
			"statistics": map[string]any{
				"episodeCount": a.total, "episodeFileCount": a.withFile,
				"totalEpisodeCount": a.total, "percentOfEpisodes": pct(a.withFile, a.total),
			},
		})
	}
	return map[string]any{
		"id": s.ID, "title": s.Title, "tvdbId": s.TVDBID, "tmdbId": s.TMDBID,
		"monitored": s.Monitored, "seriesType": s.SeriesType, "year": s.Year,
		"status": s.TMDBStatus, "ended": strings.EqualFold(s.TMDBStatus, "ended"),
		"qualityProfileId": s.QualityProfileID, "rootFolderPath": s.RootFolderPath, "seasonFolder": true,
		"titleSlug": slug(s.Title) + "-" + strconv.FormatInt(s.TVDBID, 10),
		"added":     rfc3339(s.AddedAt), "seasons": seasons,
		"statistics": map[string]any{
			"seasonCount": len(seasons), "episodeCount": totalEps,
			"episodeFileCount": totalFiles, "totalEpisodeCount": totalEps,
			"percentOfEpisodes": pct(totalFiles, totalEps),
		},
		"images": []map[string]any{{"coverType": "poster", "url": h.deps.Settings.TMDB().ImageURL("w500", s.PosterPath)}},
	}
}

func pct(part, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

// seriesLookup answers GET /sonarr/api/v3/series/lookup?term=tvdb:{id}. It
// returns one canonical object built from TMDB; the `id` field is present only
// when the series is already in Boxarr's catalog (the add-vs-update switch).
func (h *Handler) seriesLookup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tvdbID := parseTermID(r.URL.Query().Get("term"), "tvdb")
	if tvdbID == 0 {
		h.writeJSON(w, http.StatusOK, []any{})
		return
	}
	found, err := h.deps.Settings.TMDB().FindByTVDB(ctx, tvdbID)
	if err != nil || len(found.TVResults) == 0 {
		h.writeJSON(w, http.StatusOK, []any{}) // genuinely unresolvable → Seerr shows "not found"
		return
	}
	tv := found.TVResults[0]
	d, err := h.deps.Settings.TMDB().TVDetails(ctx, tv.ID)
	if err != nil {
		h.writeJSON(w, http.StatusOK, []any{})
		return
	}
	seasons := make([]map[string]any, 0, len(d.Seasons))
	for _, s := range d.Seasons {
		seasons = append(seasons, map[string]any{"seasonNumber": s.SeasonNumber, "monitored": s.SeasonNumber != 0})
	}
	obj := map[string]any{
		"title": d.Name, "sortTitle": strings.ToLower(d.Name), "status": strings.ToLower(d.Status),
		"overview": d.Overview, "year": yearOf(d.FirstAirDate), "tvdbId": tvdbID,
		"titleSlug": slug(d.Name), "seasons": seasons, "monitored": false, "seasonFolder": true,
		"remotePoster": h.deps.Settings.TMDB().ImageURL("w500", d.PosterPath),
		"images":       []map[string]any{{"coverType": "poster", "url": h.deps.Settings.TMDB().ImageURL("w500", d.PosterPath)}},
	}
	// Include id only when already tracked (drives Seerr to the update path).
	if sr, _ := h.deps.Store.GetSeriesByTMDB(ctx, int64(d.ID)); sr != nil {
		obj["id"] = sr.ID
		obj["monitored"] = sr.Monitored
	}
	h.writeJSON(w, http.StatusOK, []any{obj})
}

// addSeries answers POST /sonarr/api/v3/series — ingests into the catalog and
// echoes the created object with a numeric id (Seerr checks response.data.id).
func (h *Handler) addSeries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		TVDBID         int    `json:"tvdbId"`
		Title          string `json:"title"`
		Monitored      bool   `json:"monitored"`
		SeriesType     string `json:"seriesType"`     // "standard" | "anime" | "daily"
		RootFolderPath string `json:"rootFolderPath"` // Overseerr's chosen root (anime root → anime)
		Seasons        []struct {
			SeasonNumber int  `json:"seasonNumber"`
			Monitored    bool `json:"monitored"`
		} `json:"seasons"`
		AddOptions struct {
			SearchForMissingEpisodes bool `json:"searchForMissingEpisodes"`
		} `json:"addOptions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid body"})
		return
	}
	// Resolve tvdb -> tmdb.
	found, err := h.deps.Settings.TMDB().FindByTVDB(ctx, body.TVDBID)
	if err != nil || len(found.TVResults) == 0 {
		h.writeJSON(w, http.StatusNotFound, map[string]any{"error": "series not found"})
		return
	}
	tmdbID := int64(found.TVResults[0].ID)

	var monitoredSeasons []int
	for _, s := range body.Seasons {
		if s.Monitored {
			monitoredSeasons = append(monitoredSeasons, s.SeasonNumber)
		}
	}
	// Add as anime when Overseerr flags the request as anime — either via
	// seriesType="anime" or by choosing the anime root folder — so it lands in the
	// anime library and uses anime numbering/selection.
	seriesType := "standard"
	animeRoot := h.deps.Settings.AnimeLibraryRoot()
	if strings.EqualFold(body.SeriesType, "anime") ||
		(animeRoot != "" && strings.EqualFold(strings.TrimRight(body.RootFolderPath, "/"), strings.TrimRight(animeRoot, "/"))) {
		seriesType = "anime"
	}
	sr, err := h.deps.Catalog.AddSeries(ctx, tmdbID, true, monitoredSeasons, seriesType)
	if err != nil && !errors.Is(err, catalog.ErrAlreadyExists) {
		h.deps.Logger.Error("seerr: add series", "error", err)
		h.writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	if body.AddOptions.SearchForMissingEpisodes {
		sid := sr.ID
		h.searchOnAdd(ctx, func(c contextT) error { return h.deps.Catalog.SearchWantedForSeries(c, sid) })
	}
	h.writeJSON(w, http.StatusCreated, map[string]any{
		"id": sr.ID, "title": sr.Title, "tvdbId": body.TVDBID, "monitored": true,
		"seasonFolder": true, "rootFolderPath": sr.RootFolderPath, "titleSlug": slug(sr.Title),
		"added": rfc3339(sr.AddedAt), "images": []any{}, "seasons": body.Seasons,
	})
}

// updateSeries answers PUT /sonarr/api/v3/series — Seerr's re-request path for a
// series already in the catalog (e.g. requesting additional seasons). It monitors
// the requested seasons (cascading to their episodes) and returns the series.
func (h *Handler) updateSeries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		ID        int64 `json:"id"`
		TVDBID    int   `json:"tvdbId"`
		Monitored bool  `json:"monitored"`
		Seasons   []struct {
			SeasonNumber int  `json:"seasonNumber"`
			Monitored    bool `json:"monitored"`
		} `json:"seasons"`
		AddOptions struct {
			SearchForMissingEpisodes bool `json:"searchForMissingEpisodes"`
		} `json:"addOptions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid body"})
		return
	}
	var s *media.Series
	if body.ID != 0 {
		s, _ = h.deps.Store.GetSeries(ctx, body.ID)
	}
	if s == nil && body.TVDBID != 0 {
		s, _ = h.deps.Store.GetSeriesByTVDB(ctx, int64(body.TVDBID))
	}
	if s == nil {
		h.writeJSON(w, http.StatusNotFound, map[string]any{"error": "series not found"})
		return
	}
	for _, bs := range body.Seasons {
		if bs.Monitored {
			h.monitorSeason(ctx, s.ID, bs.SeasonNumber)
		}
	}
	if body.AddOptions.SearchForMissingEpisodes {
		sid := s.ID
		h.searchOnAdd(ctx, func(c contextT) error { return h.deps.Catalog.SearchWantedForSeries(c, sid) })
	}
	eps, _ := h.deps.Store.ListEpisodes(ctx, s.ID)
	h.writeJSON(w, http.StatusOK, h.seriesResource(s, eps))
}

// monitorSeason monitors a (scene-aware) season and cascades to its episodes so
// the auto-searcher picks up the newly requested season.
func (h *Handler) monitorSeason(ctx contextT, seriesID int64, seasonNum int) {
	seasons, _ := h.deps.Store.ListSeasons(ctx, seriesID)
	for _, ss := range seasons {
		if ss.SeasonNumber == seasonNum {
			_ = h.deps.Store.SetSeasonMonitored(ctx, ss.ID, true)
		}
	}
	today := time.Now().UTC().Format("2006-01-02")
	eps, _ := h.deps.Store.ListEpisodes(ctx, seriesID)
	for _, e := range eps {
		ds := e.SeasonNumber
		if e.SceneSeason > 0 {
			ds = e.SceneSeason
		}
		if ds != seasonNum {
			continue
		}
		e.Monitored = true
		if !e.HasFile && e.AirDate != "" && e.AirDate <= today {
			e.Status = media.MediaWanted
		}
		_ = h.deps.Store.UpdateEpisode(ctx, e)
	}
}

// episodeList answers GET /sonarr/api/v3/episode?seriesId= (Jellyseerr re-request).
func (h *Handler) episodeList(w http.ResponseWriter, r *http.Request) {
	sid, _ := strconv.ParseInt(r.URL.Query().Get("seriesId"), 10, 64)
	if sid == 0 {
		h.writeJSON(w, http.StatusOK, []any{})
		return
	}
	eps, _ := h.deps.Store.ListEpisodes(r.Context(), sid)
	out := make([]map[string]any, 0, len(eps))
	for _, e := range eps {
		sn, en := e.SeasonNumber, e.EpisodeNumber
		if e.SceneSeason > 0 {
			sn, en = e.SceneSeason, e.SceneEpisode
		}
		out = append(out, map[string]any{
			"id": e.ID, "seriesId": e.SeriesID, "seasonNumber": sn, "episodeNumber": en,
			"title": e.Title, "monitored": e.Monitored, "hasFile": e.HasFile,
		})
	}
	h.writeJSON(w, http.StatusOK, out)
}

// monitorEps answers PUT /sonarr/api/v3/episode/monitor (Jellyseerr re-request).
func (h *Handler) monitorEps(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		EpisodeIDs []int64 `json:"episodeIds"`
		Monitored  bool    `json:"monitored"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid body"})
		return
	}
	for _, id := range body.EpisodeIDs {
		if e, err := h.deps.Store.GetEpisode(ctx, id); err == nil {
			e.Monitored = body.Monitored
			_ = h.deps.Store.UpdateEpisode(ctx, e)
		}
	}
	h.writeJSON(w, http.StatusOK, []any{})
}

// deleteSeries answers DELETE /sonarr/api/v3/series/{id} (Jellyseerr request
// cancellation). Removes the catalog entry; TorBox content is left for the user.
func (h *Handler) deleteSeries(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
		return
	}
	_ = h.deps.Store.DeleteSeries(r.Context(), id)
	h.writeJSON(w, http.StatusOK, map[string]any{})
}

func parseTermID(term, prefix string) int {
	rest, ok := strings.CutPrefix(strings.TrimSpace(term), prefix+":")
	if !ok {
		return 0
	}
	id, _ := strconv.Atoi(strings.TrimSpace(rest))
	return id
}
