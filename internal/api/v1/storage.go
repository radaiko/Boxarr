package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/release"
	"github.com/radaiko/boxarr/internal/store"
	"github.com/radaiko/boxarr/internal/task"
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
	// "active" = jobs actually downloading (the number comparable to slots);
	// queued/seeding are reported separately so the stat isn't misleading.
	active, _ := h.deps.Store.CountJobsByState(ctx, job.StateDownloading)
	queued, _ := h.deps.Store.CountJobsByState(ctx, job.StateSubmitting, job.StateQueued)
	seeding, _ := h.deps.Store.CountJobsByState(ctx, job.StateSeeding)

	byCat, _ := h.deps.Store.WebDAVUsageByCategory(ctx)
	// Learned limits: what Boxarr inferred + remembers from TorBox throttling.
	now := time.Now().UTC()
	startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	usedToday, _ := h.deps.Store.CountJobsSubmittedSince(ctx, startToday)
	events, _ := h.deps.Store.ListLimitEvents(ctx, 50)
	if events == nil {
		events = []store.LimitEvent{}
	}
	cooldown := ""
	if cu := h.deps.Settings.TorBoxCooldownUntil(); !cu.IsZero() {
		cooldown = cu.UTC().Format(time.RFC3339)
	}
	resp := map[string]any{
		"usedBytes":  used,
		"byCategory": byCat,
		"downloads":  map[string]any{"active": active, "queued": queued, "seeding": seeding},
		"limits": map[string]any{
			"dailyCap":      h.deps.Settings.TorBoxDailyCap(),
			"usedToday":     usedToday,
			"cooldownUntil": cooldown,
			"events":        events,
		},
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
			// Surface a TorBox API failure (esp. an expired/invalid token) so the UI
			// can warn prominently instead of it only appearing in the logs.
			resp["torboxError"] = err.Error()
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
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	Season     int    `json:"season,omitempty"`
	Episode    int    `json:"episode,omitempty"`
	PosterPath string `json:"posterPath,omitempty"` // catalog poster for tracked items
	CatalogID  int64  `json:"catalogId,omitempty"`  // movie/series id for tracked items (jump-to)
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

var titleNormRe = regexp.MustCompile(`[^a-z0-9]+`)

// normTitle normalizes a title for fuzzy catalog matching: lowercased, with
// punctuation stripped and whitespace collapsed — so a parsed release title like
// "Avengers Endgame" matches the catalog's "Avengers: Endgame".
// libraryMountTargets returns every mount path a library item points to. It reads
// the actual on-disk library symlinks (movie + episode LibraryPath) — ground truth
// that survives job reaping (which empties imported_symlinks) — plus any table
// records as a fallback.
func (h *Handler) libraryMountTargets(ctx context.Context) []string {
	var out []string
	readlink := func(p string) {
		if p == "" {
			return
		}
		t, err := os.Readlink(p)
		if err != nil {
			return
		}
		if !filepath.IsAbs(t) {
			t = filepath.Join(filepath.Dir(p), t)
		}
		out = append(out, t)
	}
	if movies, err := h.deps.Store.ListMovies(ctx); err == nil {
		for _, m := range movies {
			readlink(m.LibraryPath)
		}
	}
	if series, err := h.deps.Store.ListSeries(ctx); err == nil {
		for _, s := range series {
			if eps, e := h.deps.Store.ListEpisodes(ctx, s.ID); e == nil {
				for _, ep := range eps {
					readlink(ep.LibraryPath)
				}
			}
		}
	}
	if syms, err := h.deps.Store.ListImportedSymlinks(ctx); err == nil {
		for _, s := range syms {
			out = append(out, s.TargetPath)
		}
	}
	return out
}

// folderHasTarget reports whether any target path lives inside remotePath.
func folderHasTarget(remotePath string, targets []string) bool {
	prefix := strings.TrimRight(remotePath, "/") + "/"
	for _, t := range targets {
		if t == remotePath || strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}

func normTitle(s string) string {
	// Normalize "&" to "and" so a release named "Fast.and.Furious.6" matches the
	// catalog "Fast & Furious 6".
	s = strings.ReplaceAll(strings.ToLower(s), "&", " and ")
	return strings.TrimSpace(titleNormRe.ReplaceAllString(s, " "))
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
	ids := body.IDs
	// Background it so a multi-item delete survives the user navigating away.
	if h.deps.Tasks != nil {
		label := plural(len(ids), "item", "items")
		id := h.deps.Tasks.Enqueue("delete", label, func(ctx context.Context, run *task.Run) error {
			h.runDelete(ctx, ids, run)
			return nil
		})
		h.writeJSON(w, http.StatusAccepted, map[string]any{"taskId": id, "queued": true})
		return
	}
	deleted, failed := h.runDelete(r.Context(), ids, nil)
	h.writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted, "failed": failed})
}

// adoptWebDAV / runDelete report via *task.Run when backgrounded (nil when inline).

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// runDelete removes the given mount items: matching TorBox download (mylists
// fetched once), the mount folder, and the row — tearing down tracked imports
// first so no library symlink dangles.
func (h *Handler) runDelete(ctx context.Context, ids []int64, run *task.Run) (deleted, failed int) {
	if run != nil {
		run.Progress(0, len(ids))
	}
	all, err := h.deps.Store.ListWebDAVItems(ctx)
	if err != nil {
		h.deps.Logger.Warn("delete: listing webdav items", "error", err)
		return 0, len(ids)
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
	for i, id := range ids {
		it := byID[id]
		if it == nil {
			if run != nil {
				run.Progress(i+1, len(ids))
			}
			continue
		}
		// Tracked item: tear down the import (library symlinks + catalog + job)
		// first so we never leave a dangling symlink pointing at the deleted file.
		if it.Known && it.JobID != 0 && h.deps.Deleter != nil {
			h.deps.Deleter.RemoveImport(ctx, it.JobID)
		}
		ln := strings.ToLower(it.Name)
		matched := false
		if did, ok := tIdx[ln]; ok {
			matched = true
			if e := tb.ControlTorrent(ctx, did, "delete"); e != nil {
				h.deps.Logger.Warn("webdav delete (torrent)", "name", it.Name, "error", e)
			}
		} else if uid, ok := uIdx[ln]; ok {
			matched = true
			if e := tb.ControlUsenet(ctx, uid, "delete"); e != nil {
				h.deps.Logger.Warn("webdav delete (usenet)", "name", it.Name, "error", e)
			}
		}
		// Only fall back to a filesystem unlink when the item wasn't in a mylist
		// (TorBox's API delete already removes matched downloads, and many rclone
		// mounts reject unlink with EIO — so don't churn/spam on the common path).
		if !matched {
			h.removeMountFolder(it.RemotePath)
		}
		if e := h.deps.Store.DeleteWebDAVItemByPath(ctx, it.RemotePath); e != nil {
			h.deps.Logger.Warn("webdav delete: dropping row", "name", it.Name, "error", e)
			failed++
		} else {
			// Tombstone the path so the reconciler doesn't re-add it from a stale
			// rclone listing before the mount notices the TorBox deletion.
			if e := h.deps.Store.AddDeletedPath(ctx, it.RemotePath); e != nil {
				h.deps.Logger.Warn("webdav delete: tombstoning path", "name", it.Name, "error", e)
			}
			deleted++
			if run != nil {
				run.Detail(it.Name)
			}
		}
		if run != nil {
			run.Progress(i+1, len(ids))
		}
	}
	return deleted, failed
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
		// Skip only if it genuinely resolves to a catalog entry. An orphaned
		// known item (job present, catalog row gone) should be re-adoptable.
		byTitle, _, _ := h.catalogIndex(ctx)
		_, title, _, _ := classifyRelease(item.Name)
		if _, ok := byTitle[normTitle(title)]; ok {
			h.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "skipped": "already in library"})
			return
		}
	}
	kind := body.Kind
	if kind == "" {
		kind, _, _, _ = classifyRelease(item.Name)
	}
	remotePath, name := item.RemotePath, item.Name
	tmdbID := body.TMDBID
	adopt := func(ctx context.Context) error {
		if err := h.deps.Adopter.AdoptUnknown(ctx, remotePath, name, kind, tmdbID); err != nil {
			h.deps.Logger.Warn("webdav adopt failed", "name", name, "kind", kind, "tmdbId", tmdbID, "error", err)
			return err
		}
		return nil
	}
	// Run adopts in the background so they survive the user navigating away (the
	// import can take a while); fall back to inline when no task runner is wired.
	if h.deps.Tasks != nil {
		id := h.deps.Tasks.Enqueue("adopt", name, func(ctx context.Context, run *task.Run) error {
			err := adopt(ctx)
			if err == nil {
				run.Detail(name)
			}
			return err
		})
		h.writeJSON(w, http.StatusAccepted, map[string]any{"taskId": id, "queued": true})
		return
	}
	if err := adopt(ctx); err != nil {
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
	byTitle, movieByID, seriesByID := h.catalogIndex(r.Context())
	jobMedia, _ := h.deps.Store.JobMediaIndex(r.Context())
	// Mount folders backed by a library item — the durable, language- and
	// job-independent link. Resolve the actual on-disk library symlinks (ground
	// truth: survives job reaping that empties imported_symlinks) plus any table
	// records. A German-titled folder that can't title-match still matches here.
	targets := h.libraryMountTargets(r.Context())
	cat := r.URL.Query().Get("category")
	out := make([]webdavItemDTO, 0, len(items))
	for _, it := range items {
		if it.IsBroken && r.URL.Query().Get("includeBroken") != "true" {
			continue
		}
		if cat != "" && it.Category != cat {
			continue
		}
		dto := toWebDAVDTO(it)
		// Resolve the item against the library by title first, then by the linked
		// job's media. A match means this folder IS a tracked item, so mark it
		// known + enrich it (poster/type/jump link). Deriving "known" from the
		// catalog — not only the reconciler/adopt DB flag — keeps adopted items
		// tracked even after an rclone re-list recreates the row and clears the
		// flag (there is no job for the reconciler to re-match them by). This only
		// upgrades unknown→known on a real match; it never downgrades.
		c, ok := byTitle[normTitle(dto.Title)]
		if !ok && dto.JobID != 0 {
			if jm, has := jobMedia[dto.JobID]; has {
				switch jm.MediaType {
				case "movie":
					c, ok = movieByID[jm.MediaRef]
				case "series", "season":
					c, ok = seriesByID[jm.MediaRef]
				}
			}
		}
		if ok {
			dto.Known = true
			dto.PosterPath = c.poster
			dto.CatalogID = c.id
			if c.kind != "" {
				dto.Kind = c.kind
			}
		}
		// Last resort: a library item's file lives inside this folder → tracked.
		if !dto.Known && folderHasTarget(it.RemotePath, targets) {
			dto.Known = true
		}
		out = append(out, dto)
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": len(out)})
}

// activity returns the live download queue (jobs in flight) plus recent
// background tasks (adopt/delete) for the Activity view.
func (h *Handler) activity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	jobs, _ := h.deps.Store.JobsByState(ctx,
		job.StatePending, job.StateSubmitting, job.StateQueued, job.StateDownloading,
		job.StateSeeding, job.StateCompleted, job.StateHealing)
	downloads := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		downloads = append(downloads, map[string]any{
			"id": j.ID, "name": j.NZBName, "state": string(j.State), "mediaType": j.MediaType,
			"progress": j.ProgressPct, "protocol": j.Protocol, "createdAt": rfc3339(j.CreatedAt),
			"size": j.TotalBytes, "downloaded": j.DownloadedBytes, "etaSeconds": j.ETASeconds,
			"release": parseReleaseMeta(j.NZBName),
		})
	}
	tasks := []task.Task{}
	if h.deps.Tasks != nil {
		tasks = h.deps.Tasks.List()
	}
	// Download history: recently-finished jobs (newest first, capped).
	hist, _ := h.deps.Store.JobsByState(ctx,
		job.StateImported, job.StateFailed, job.StateHealFailed, job.StateManuallyResolved)
	sort.Slice(hist, func(i, j int) bool { return hist[i].CreatedAt.After(hist[j].CreatedAt) })
	history := make([]map[string]any, 0, len(hist))
	for i, j := range hist {
		if i >= 50 {
			break
		}
		history = append(history, map[string]any{
			"id": j.ID, "name": j.NZBName, "state": string(j.State), "mediaType": j.MediaType,
			"size": j.TotalBytes, "protocol": j.Protocol, "createdAt": rfc3339(j.CreatedAt),
			"error": j.FailMessage, "release": parseReleaseMeta(j.NZBName),
		})
	}
	schedule := []map[string]any{}
	if h.deps.Scheduler != nil {
		schedule = h.deps.Scheduler.LoopSchedule()
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"downloads": downloads, "tasks": tasks, "history": history, "schedule": schedule})
}

type catalogEntry struct {
	poster, kind string
	id           int64
}

// catalogIndex returns catalog lookups for enriching tracked mount items: by
// normalized title, by movie id, and by series id. Built once per WebDAV list.
func (h *Handler) catalogIndex(ctx context.Context) (byTitle map[string]catalogEntry, movieByID, seriesByID map[int64]catalogEntry) {
	byTitle = map[string]catalogEntry{}
	movieByID = map[int64]catalogEntry{}
	seriesByID = map[int64]catalogEntry{}
	if ms, err := h.deps.Store.ListMovies(ctx); err == nil {
		for _, mv := range ms {
			e := catalogEntry{poster: mv.PosterPath, kind: "movie", id: mv.ID}
			byTitle[normTitle(mv.Title)] = e
			for _, alt := range mv.AltTitles { // match German/other-language releases too
				byTitle[normTitle(alt)] = e
			}
			movieByID[mv.ID] = e
		}
	}
	if ss, err := h.deps.Store.ListSeries(ctx); err == nil {
		for _, s := range ss {
			kind := "series"
			if s.SeriesType == "anime" {
				kind = "anime"
			}
			e := catalogEntry{poster: s.PosterPath, kind: kind, id: s.ID}
			byTitle[normTitle(s.Title)] = e
			seriesByID[s.ID] = e
		}
	}
	return byTitle, movieByID, seriesByID
}
