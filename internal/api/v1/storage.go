package v1

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/release"
	"github.com/radaiko/boxarr/internal/webdav"
)

// planSlots maps a TorBox plan tier to its concurrent active-download allowance
// (derived — /user/me does not return it; 00 §9 runtime-verify). Fallback 1.
var planSlots = map[int]int{0: 1, 1: 3, 2: 10, 3: 5}

var planNames = map[int]string{0: "Free", 1: "Essential", 2: "Pro", 3: "Standard"}

// storage reports total used bytes + TorBox plan/usage (FR-ST-1/2, FR-LIM-4).
func (h *Handler) storage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	used, _ := h.deps.Store.WebDAVUsageBytes(ctx)
	active, _ := h.deps.Store.CountJobsByState(ctx,
		job.StateSubmitting, job.StateQueued, job.StateDownloading, job.StateSeeding)

	byCat, _ := h.deps.Store.WebDAVUsageByCategory(ctx)
	resp := map[string]any{
		"usedBytes":  used,
		"byCategory": byCat,
		"downloads":  map[string]any{"active": active},
	}
	if h.deps.Settings.TorBox() != nil {
		if u, err := h.deps.Settings.TorBox().UserMe(ctx); err == nil {
			tier := int(u.Plan)
			slots, ok := planSlots[tier]
			if !ok {
				slots = 1
			}
			resp["plan"] = map[string]any{
				"tier": tier, "tierName": planNames[tier], "concurrentSlots": slots,
				"isSubscribed": u.IsSubscribed, "premiumExpiresAt": u.PremiumExpiresAt,
			}
			resp["usage"] = map[string]any{
				"monthlyDownloadedBytes": u.TotalDownloaded,
				"cooldownUntil":          u.CooldownUntil,
				"inCooldown":             u.CooldownUntil != "",
			}
		} else {
			h.deps.Logger.Warn("storage: /user/me failed", "error", err)
		}
	}
	h.writeJSON(w, http.StatusOK, resp)
}

type webdavItemDTO struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	RemotePath string `json:"remotePath"`
	Size       int64  `json:"size"`
	Category   string `json:"category"`
	Known      bool   `json:"known"`
	JobID      int64  `json:"jobId,omitempty"`
	IsBroken   bool   `json:"isBroken"`
	FirstSeen  string `json:"firstSeen"`
	LastSeen   string `json:"lastSeen"`
	// Parsed from the folder name so the UI can group by title (kind = movie |
	// series | anime | unknown).
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Season  int    `json:"season,omitempty"`
	Episode int    `json:"episode,omitempty"`
}

func toWebDAVDTO(it *webdav.WebDAVItem) webdavItemDTO {
	kind, title, season, episode := classifyRelease(it.Name)
	return webdavItemDTO{
		ID: it.ID, Name: it.Name, RemotePath: it.RemotePath, Size: it.Size,
		Category: it.Category, Known: it.Known, JobID: it.JobID, IsBroken: it.IsBroken,
		FirstSeen: rfc3339(it.FirstSeen), LastSeen: rfc3339(it.LastSeen),
		Kind: kind, Title: title, Season: season, Episode: episode,
	}
}

// classifyRelease parses a mount folder name into a grouping kind + a display
// title (so the UI can cluster episodes under their show). Heuristics mirror the
// reconciler's guessCategory, plus anime detection.
func classifyRelease(name string) (kind, title string, season, episode int) {
	p, err := release.ParseRelease(name)
	if err != nil || p == nil {
		return "unknown", name, 0, 0
	}
	title = strings.TrimSpace(p.Title)
	if title == "" {
		title = name
	}
	switch {
	case p.IsAnime || len(p.AbsoluteEpisodes) > 0:
		return "anime", title, p.SeasonNumber, p.EpisodeStart
	case p.SeasonNumber > 0 || p.EpisodeStart > 0 || p.IsSeasonPack || p.AirDate != "":
		return "series", title, p.SeasonNumber, p.EpisodeStart
	case p.Year > 0:
		return "movie", title, 0, 0
	default:
		return "unknown", title, 0, 0
	}
}

// refreshWebDAV triggers an out-of-band reconcile sweep (FR-WD-3) so the mount
// view + unknown-content detection update without waiting for the 15-min tick.
func (h *Handler) refreshWebDAV(w http.ResponseWriter, r *http.Request) {
	if h.deps.Reconciler == nil {
		h.writeError(w, http.StatusServiceUnavailable, "unavailable", "reconciler not wired")
		return
	}
	if err := h.deps.Reconciler.Reconcile(r.Context()); err != nil {
		h.writeError(w, http.StatusBadGateway, "upstream_unavailable", err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// deleteWebDAV removes one or more mount items (by id): for each it deletes the
// matching TorBox download when present, removes the release folder from the
// mount, and drops the row. The TorBox mylists are fetched once for the whole
// batch so deleting a whole show is a couple of calls, not one per episode.
func (h *Handler) deleteWebDAV(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.IDs) == 0 {
		h.writeError(w, http.StatusBadRequest, "bad_request", "ids is required")
		return
	}
	ctx := r.Context()
	all, err := h.deps.Store.ListWebDAVItems(ctx)
	if err != nil {
		h.serverError(w, "listing webdav items", err)
		return
	}
	byID := make(map[int64]*webdav.WebDAVItem, len(all))
	for _, it := range all {
		byID[it.ID] = it
	}
	// Index both TorBox mylists once (name → id, case-insensitive).
	tb := h.deps.Settings.TorBox()
	tIdx, uIdx := map[string]int64{}, map[string]int64{}
	if ts, e := tb.ListTorrents(ctx); e == nil {
		for _, d := range ts {
			tIdx[strings.ToLower(d.Name)] = int64(d.ID)
		}
	}
	if us, e := tb.ListUsenet(ctx); e == nil {
		for _, d := range us {
			uIdx[strings.ToLower(d.Name)] = int64(d.ID)
		}
	}
	deleted, failed := 0, 0
	for _, id := range body.IDs {
		it := byID[id]
		if it == nil {
			continue
		}
		// Tracked item: tear down the import (library symlinks + catalog + job)
		// first so we never leave a dangling symlink pointing at the deleted file.
		if it.Known && it.JobID != 0 && h.deps.Deleter != nil {
			h.deps.Deleter.RemoveImport(ctx, it.JobID)
		}
		ln := strings.ToLower(it.Name)
		if did, ok := tIdx[ln]; ok {
			if e := tb.ControlTorrent(ctx, did, "delete"); e != nil {
				h.deps.Logger.Warn("webdav delete (torrent)", "name", it.Name, "error", e)
			}
		} else if uid, ok := uIdx[ln]; ok {
			if e := tb.ControlUsenet(ctx, uid, "delete"); e != nil {
				h.deps.Logger.Warn("webdav delete (usenet)", "name", it.Name, "error", e)
			}
		}
		h.removeMountFolder(it.RemotePath)
		if e := h.deps.Store.DeleteWebDAVItemByPath(ctx, it.RemotePath); e != nil {
			h.deps.Logger.Warn("webdav delete: dropping row", "name", it.Name, "error", e)
			failed++
			continue
		}
		deleted++
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted, "failed": failed})
}

// adoptWebDAV imports an unknown mount item into the library (TMDB match → catalog
// row → symlink → tracked). kind ("movie"|"series"|"anime", or "" to auto-detect)
// usually comes from the section the user adopted from, so anime lands in the
// anime library.
func (h *Handler) adoptWebDAV(w http.ResponseWriter, r *http.Request) {
	if h.deps.Adopter == nil {
		h.writeError(w, http.StatusServiceUnavailable, "unavailable", "adopt not wired")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "bad_request", "invalid item id")
		return
	}
	var body struct {
		Kind   string `json:"kind"`   // movie | series | anime | "" (auto)
		TMDBID int64  `json:"tmdbId"` // 0 = auto-match by name; >0 = the picked match
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	ctx := r.Context()
	all, err := h.deps.Store.ListWebDAVItems(ctx)
	if err != nil {
		h.serverError(w, "listing webdav items", err)
		return
	}
	var item *webdav.WebDAVItem
	for _, it := range all {
		if it.ID == id {
			item = it
			break
		}
	}
	if item == nil {
		h.writeError(w, http.StatusNotFound, "not_found", "mount item not found")
		return
	}
	if item.Known {
		// Already imported — no-op rather than re-running the importer.
		h.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "skipped": "already in library"})
		return
	}
	kind := body.Kind
	if kind == "" {
		kind, _, _, _ = classifyRelease(item.Name)
	}
	if err := h.deps.Adopter.AdoptUnknown(ctx, item.RemotePath, item.Name, kind, body.TMDBID); err != nil {
		// Log it too — otherwise the reason only reaches the browser (it was
		// invisible in the container logs).
		h.deps.Logger.Warn("webdav adopt failed", "name", item.Name, "kind", kind, "tmdbId", body.TMDBID, "error", err)
		h.writeError(w, http.StatusUnprocessableEntity, "unprocessable", err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// listWebDAV lists mount items from the cached table (FR-WD-1/2; never scans the
// live mount per request — that is the reconciler's job, Phase 4).
func (h *Handler) listWebDAV(w http.ResponseWriter, r *http.Request) {
	items, err := h.deps.Store.ListWebDAVItems(r.Context())
	if err != nil {
		h.serverError(w, "listing webdav items", err)
		return
	}
	cat := r.URL.Query().Get("category")
	out := make([]webdavItemDTO, 0, len(items))
	for _, it := range items {
		if it.IsBroken && r.URL.Query().Get("includeBroken") != "true" {
			continue
		}
		if cat != "" && it.Category != cat {
			continue
		}
		out = append(out, toWebDAVDTO(it))
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": len(out)})
}
