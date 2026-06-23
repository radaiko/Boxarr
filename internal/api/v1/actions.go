package v1

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/radaiko/boxarr/internal/logbuf"
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
		return h.deps.Catalog.UpgradeWanted(ctx)
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

// triggerLibraryRefresh reconciles the TorBox/WebDAV mount and re-runs the Plex
// stream check, in the background.
func (h *Handler) triggerLibraryRefresh(w http.ResponseWriter, r *http.Request) {
	h.runBackground("refresh", "Refresh from TorBox + Plex", func(ctx context.Context) error {
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

// releaseLanguages returns the verified release→language knowledge base (the real
// audio/subtitle languages observed after download), for display + export.
func (h *Handler) releaseLanguages(w http.ResponseWriter, r *http.Request) {
	rows, err := h.deps.Store.ListReleaseLangs(r.Context(), 1000)
	if err != nil {
		h.serverError(w, "listing release languages", err)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"items": rows, "total": len(rows)})
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
