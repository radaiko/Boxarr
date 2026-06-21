package v1

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/radaiko/boxarr/internal/prowlarr"
	"github.com/radaiko/boxarr/internal/release"
	"github.com/radaiko/boxarr/internal/selection"
)

// grabRef is the opaque payload encoded into releaseId so a grab needs no
// re-search (it carries everything the grab pipeline needs).
type grabRef struct {
	IndexerID   int    `json:"i"`
	GUID        string `json:"g"`
	Protocol    string `json:"p"`
	DownloadURL string `json:"d"`
	MagnetURL   string `json:"m"`
	InfoHash    string `json:"h"`
	Title       string `json:"t"`
}

func encodeReleaseID(g grabRef) string {
	b, _ := json.Marshal(g)
	return "rel_" + base64.RawURLEncoding.EncodeToString(b)
}

func decodeReleaseID(id string) (grabRef, error) {
	var g grabRef
	raw, ok := strings.CutPrefix(id, "rel_")
	if !ok {
		return g, fmt.Errorf("bad releaseId")
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return g, err
	}
	return g, json.Unmarshal(b, &g)
}

type releaseDTO struct {
	ReleaseID       string `json:"releaseId"`
	Title           string `json:"title"`
	Indexer         string `json:"indexer"`
	IndexerID       int    `json:"indexerId"`
	Protocol        string `json:"protocol"`
	Size            int64  `json:"size"`
	Quality         string `json:"quality,omitempty"`
	Resolution      string `json:"resolution,omitempty"`
	Seeders         *int   `json:"seeders,omitempty"`
	Leechers        *int   `json:"leechers,omitempty"`
	Grabs           *int   `json:"grabs,omitempty"`
	Cached          *bool  `json:"cached"`
	Score           int    `json:"score"`
	Rejected        bool   `json:"rejected"`
	RejectionReason string `json:"rejectionReason,omitempty"`
	HasMagnet       bool   `json:"hasMagnet"`
	PublishDate     string `json:"publishDate,omitempty"`
}

func (h *Handler) searchMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	m, err := h.deps.Store.GetMovie(r.Context(), id)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "not_found", "movie not found")
		return
	}
	q := m.Title
	if m.Year > 0 {
		q = fmt.Sprintf("%s %d", m.Title, m.Year)
	}
	h.runSearch(w, r, prowlarr.SearchParams{Query: q, Type: "movie", Categories: []int{2000}})
}

func (h *Handler) freeSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		h.writeError(w, http.StatusBadRequest, "bad_request", "q is required")
		return
	}
	typ := r.URL.Query().Get("type")
	if typ == "" {
		typ = "search"
	}
	p := prowlarr.SearchParams{Query: q, Type: typ}
	switch typ {
	case "movie":
		p.Categories = []int{2000}
	case "tvsearch":
		p.Categories = []int{5000}
	}
	h.runSearch(w, r, p)
}

func (h *Handler) runSearch(w http.ResponseWriter, r *http.Request, p prowlarr.SearchParams) {
	ctx := r.Context()
	results, err := h.deps.Settings.Prowlarr().Search(ctx, p)
	if err != nil {
		h.writeError(w, http.StatusBadGateway, "upstream_unavailable", "prowlarr: "+err.Error())
		return
	}
	cached := h.cachedSet(ctx, results)
	cfg := h.deps.Settings.SelectionConfig()

	rels := make([]selection.Release, len(results))
	parsedRes := make([]string, len(results))
	for i, rr := range results {
		parsed, _ := release.ParseRelease(rr.Title)
		res, qual := "", ""
		if parsed != nil {
			res, qual = parsed.Resolution, parsed.Quality
		}
		parsedRes[i] = res
		rels[i] = selection.Release{
			Title: rr.Title, Protocol: rr.Protocol, Size: rr.Size,
			Seeders: rr.Seeders, Grabs: rr.Grabs, Resolution: res, Quality: qual,
			IndexerFlags: rr.IndexerFlags, Cached: rr.Protocol == "torrent" && cached[strings.ToLower(rr.InfoHash)],
		}
	}

	// Index releases by Title to recover the original ReleaseResource after ranking.
	byTitle := map[string]prowlarr.ReleaseResource{}
	resByTitle := map[string]string{}
	for i, rr := range results {
		byTitle[rr.Title] = rr
		resByTitle[rr.Title] = parsedRes[i]
	}

	ranked := cfg.Rank(rels)
	items := make([]releaseDTO, 0, len(ranked))
	for _, sc := range ranked {
		rr := byTitle[sc.Release.Title]
		items = append(items, toReleaseDTO(rr, sc, resByTitle[rr.Title]))
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

// cachedSet batches torrent info-hashes through TorBox checkcached and returns
// the set of cached (lowercase) hashes.
func (h *Handler) cachedSet(ctx context.Context, results []prowlarr.ReleaseResource) map[string]bool {
	var hashes []string
	for _, rr := range results {
		if rr.Protocol == "torrent" && rr.InfoHash != "" {
			hashes = append(hashes, strings.ToLower(rr.InfoHash))
		}
	}
	out := map[string]bool{}
	if len(hashes) == 0 || h.deps.Settings.TorBox() == nil {
		return out
	}
	checks, err := h.deps.Settings.TorBox().CheckCached(ctx, hashes)
	if err != nil {
		h.deps.Logger.Warn("checkcached failed", "error", err)
		return out
	}
	for _, c := range checks {
		out[strings.ToLower(c.Hash)] = true
	}
	return out
}

func toReleaseDTO(rr prowlarr.ReleaseResource, sc selection.Scored, resolution string) releaseDTO {
	d := releaseDTO{
		ReleaseID: encodeReleaseID(grabRef{
			IndexerID: rr.IndexerID, GUID: rr.GUID, Protocol: rr.Protocol,
			DownloadURL: rr.DownloadURL, MagnetURL: rr.MagnetURL, InfoHash: rr.InfoHash, Title: rr.Title,
		}),
		Title: rr.Title, Indexer: rr.Indexer, IndexerID: rr.IndexerID, Protocol: rr.Protocol,
		Size: rr.Size, Quality: sc.Release.Quality, Resolution: resolution,
		Score: sc.Score, Rejected: sc.Score == math.MinInt, HasMagnet: rr.MagnetURL != "",
		PublishDate: rr.PublishDate,
	}
	if rr.Protocol == "torrent" {
		s, l := rr.Seeders, rr.Leechers
		d.Seeders, d.Leechers = &s, &l
		c := sc.Release.Cached
		d.Cached = &c
	} else {
		g := rr.Grabs
		d.Grabs = &g
	}
	if d.Rejected {
		d.RejectionReason = "failed a hard filter (size/seeders/blocked/allow-list)"
	}
	return d
}
