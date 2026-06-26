package v1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/radaiko/boxarr/internal/logbuf"
	"github.com/radaiko/boxarr/internal/settings"
	"github.com/radaiko/boxarr/internal/task"
)

// logs returns the recent in-memory log tail (newest first) for the Logs view,
// filterable by minimum level and a substring query.
func (h *Handler) logs(w http.ResponseWriter, r *http.Request) {
	if h.deps.Logs == nil {
		h.writeJSON(w, http.StatusOK, map[string]any{"entries": []any{}})
		return
	}
	limit := 500
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 5000 {
			limit = n
		}
	}
	entries := h.deps.Logs.Entries(limit, logbuf.ParseLevel(r.URL.Query().Get("level")), r.URL.Query().Get("q"))
	h.writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

// triggerUpgradeSearch runs the language/quality upgrade search now (in the
// background so the request returns immediately).
func (h *Handler) triggerUpgradeSearch(w http.ResponseWriter, r *http.Request) {
	if h.deps.Catalog == nil {
		h.writeError(w, http.StatusServiceUnavailable, "unavailable", "catalog not wired")
		return
	}
	h.runBackground("upgrade", "Search for upgrades", func(ctx context.Context) error {
		return h.deps.Catalog.UpgradeNow(ctx) // manual = force, ignore the per-item cadence
	})
	h.writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// triggerPlexLanguage runs the Plex auto-language sweep now (background).
func (h *Handler) triggerPlexLanguage(w http.ResponseWriter, r *http.Request) {
	if h.deps.PlexLang == nil {
		h.writeError(w, http.StatusServiceUnavailable, "unavailable", "plex language not wired")
		return
	}
	h.runBackground("plex-language", "Update Plex languages", func(ctx context.Context) error {
		return h.deps.PlexLang.PlexLanguageSweep(ctx)
	})
	h.writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// triggerSearchMissing force-searches every monitored, released/aired, file-less
// item now and records each outcome as task detail lines (visible in Activity →
// Tasks), so the user can see exactly what was searched/grabbed/skipped.
func (h *Handler) triggerSearchMissing(w http.ResponseWriter, r *http.Request) {
	if h.deps.Catalog == nil {
		h.writeError(w, http.StatusServiceUnavailable, "unavailable", "catalog not wired")
		return
	}
	if h.deps.Tasks != nil {
		h.deps.Tasks.Enqueue("search", "Search all missing", func(ctx context.Context, run *task.Run) error {
			g, n, err := h.deps.Catalog.SearchAllMissing(ctx, func(line string) { run.Detail(line) })
			run.Detail(fmt.Sprintf("done — searched %d, grabbed %d", n, g))
			return err
		})
	} else {
		go func() { _, _, _ = h.deps.Catalog.SearchAllMissing(context.Background(), nil) }()
	}
	h.writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// triggerTVDBRefresh updates every series' episode numbering from TheTVDB, in the
// background, with per-series progress in the task detail.
func (h *Handler) triggerTVDBRefresh(w http.ResponseWriter, r *http.Request) {
	if h.deps.Catalog == nil {
		h.writeError(w, http.StatusServiceUnavailable, "unavailable", "catalog not wired")
		return
	}
	if h.deps.Tasks != nil {
		h.deps.Tasks.Enqueue("refresh", "Refresh from TVDB", func(ctx context.Context, run *task.Run) error {
			return h.deps.Catalog.RefreshAllFromTVDB(ctx, func(line string) { run.Detail(line) })
		})
	} else {
		go func() { _ = h.deps.Catalog.RefreshAllFromTVDB(context.Background(), nil) }()
	}
	h.writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// triggerLibraryRefresh refreshes movie alternative titles (TMDB), reconciles the
// TorBox/WebDAV mount, and re-runs the Plex stream check, in the background. The
// title refresh runs first so the reconcile recognizes cross-language folders.
func (h *Handler) triggerLibraryRefresh(w http.ResponseWriter, r *http.Request) {
	h.runBackground("refresh", "Refresh from TorBox + Plex", func(ctx context.Context) error {
		if h.deps.Catalog != nil {
			_ = h.deps.Catalog.RefreshMovieTitles(ctx, nil)
		}
		if h.deps.Reconciler != nil {
			if err := h.deps.Reconciler.Reconcile(ctx); err != nil {
				return err
			}
		}
		if h.deps.PlexLang != nil {
			return h.deps.PlexLang.PlexLanguageSweep(ctx)
		}
		return nil
	})
	h.writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// groupLangStatDTO is a release group's reliability for one favorited language.
type groupLangStatDTO struct {
	Group   string  `json:"group"`
	Total   int     `json:"total"`
	InLang  int     `json:"inLang"`
	Ratio   float64 `json:"ratio"`
	Trusted bool    `json:"trusted"` // meets the scoring bar (≥90% over ≥3 releases)
}

// releaseLanguages returns the verified release→language knowledge base plus, per
// favorited language, each release group's reliability at shipping it.
func (h *Handler) releaseLanguages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := h.deps.Store.ListReleaseLangs(ctx, 1000)
	if err != nil {
		h.serverError(w, "listing release languages", err)
		return
	}
	// Per favorited language, which groups reliably ship it (counts + ratio). The
	// "trusted" groups are exactly the ones scoring rewards with a likelihood bonus.
	favs := favoriteLanguages(h.deps.Settings)
	groupStats := map[string][]groupLangStatDTO{}
	for _, lang := range favs {
		gs, gerr := h.deps.Store.GroupLanguageStats(ctx, lang)
		if gerr != nil {
			continue
		}
		list := make([]groupLangStatDTO, 0, len(gs))
		for _, g := range gs {
			list = append(list, groupLangStatDTO{
				Group: g.Group, Total: g.Total, InLang: g.InLang, Ratio: g.Ratio,
				Trusted: g.Total >= settings.LikelyGroupMinSample && g.Ratio >= settings.LikelyGroupMinRatio,
			})
		}
		groupStats[lang] = list
	}
	// Failed-to-download releases (the grab blocklist) — shown in the same DB view.
	blocklist, _ := h.deps.Store.ListBlocklistedGrabs(ctx, 500)
	h.writeJSON(w, http.StatusOK, map[string]any{
		"items": rows, "total": len(rows), "favoriteLangs": favs, "groupStats": groupStats,
		"blocklist": blocklist,
	})
}

// removeBlocklistedGrab un-blocklists a release (DELETE /releases/blocklist?name=)
// so selection may grab it again. Accepts the name via query param or JSON body.
func (h *Handler) removeBlocklistedGrab(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		var body struct {
			ReleaseName string `json:"releaseName"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		name = body.ReleaseName
	}
	if name == "" {
		h.writeError(w, http.StatusBadRequest, "bad_request", "release name is required")
		return
	}
	if err := h.deps.Store.RemoveBlocklistedGrab(r.Context(), name); err != nil {
		h.serverError(w, "removing blocklist entry", err)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// favoriteLanguages is the de-duplicated union of required + preferred languages
// across all media kinds — every language the user cares about getting (a required
// language like DE is the strongest preference). Required first, so the user's
// must-have language leads the Languages view. Lower-cased to match the KB.
func favoriteLanguages(set *settings.Store) []string {
	seen := map[string]bool{}
	var out []string
	for _, kind := range []string{"movie", "series", "anime"} {
		cfg := set.SelectionConfigFor(kind)
		for _, list := range [][]string{cfg.RequiredLanguages, cfg.PreferredLanguages} {
			for _, l := range list {
				l = strings.ToLower(strings.TrimSpace(l))
				if l != "" && !seen[l] {
					seen[l] = true
					out = append(out, l)
				}
			}
		}
	}
	return out
}

// runBackground runs fn on the task manager (so it survives the request), or
// inline in a goroutine when no task manager is wired.
func (h *Handler) runBackground(typ, label string, fn func(context.Context) error) {
	if h.deps.Tasks != nil {
		h.deps.Tasks.Enqueue(typ, label, func(ctx context.Context, _ *task.Run) error { return fn(ctx) })
		return
	}
	go func() {
		if err := fn(context.Background()); err != nil {
			h.deps.Logger.Warn("background action failed", "type", typ, "error", err)
		}
	}()
}
