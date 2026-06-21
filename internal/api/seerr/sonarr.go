package seerr

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/radaiko/boxarr/internal/catalog"
)

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
		TVDBID    int    `json:"tvdbId"`
		Title     string `json:"title"`
		Monitored bool   `json:"monitored"`
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
	sr, err := h.deps.Catalog.AddSeries(ctx, tmdbID, true, monitoredSeasons)
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

func parseTermID(term, prefix string) int {
	rest, ok := strings.CutPrefix(strings.TrimSpace(term), prefix+":")
	if !ok {
		return 0
	}
	id, _ := strconv.Atoi(strings.TrimSpace(rest))
	return id
}
