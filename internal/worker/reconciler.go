package worker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/notify"
	"github.com/radaiko/boxarr/internal/release"
	"github.com/radaiko/boxarr/internal/webdav"
)

// Reconcile runs one reconcile sweep on demand (POST /api/v1/webdav/refresh).
func (w *Workers) Reconcile(ctx context.Context) error { return w.reconcileOnce(ctx) }

// relinkEpisodesToScene moves anime episode symlinks named with flat TMDB numbering
// (e.g. Season 01/…S01E13) to their scene-based path (Season 02/…S02E01), matching
// how Plex's broadcast-season anime library indexes them — otherwise Plex can't
// match the file and never verifies its language. Idempotent: it acts only when the
// current symlink path differs from the expected scene path, so it's a cheap no-op
// once everything is named correctly (and for non-anime series with no scene data).
func (w *Workers) relinkEpisodesToScene(ctx context.Context) {
	series, err := w.store.ListSeries(ctx)
	if err != nil {
		return
	}
	syms, _ := w.store.ListImportedSymlinks(ctx)
	byPath := make(map[string]*job.ImportedSymlink, len(syms))
	for _, s := range syms {
		byPath[s.SymlinkPath] = s
	}
	for _, sr := range series {
		if sr.RootFolderPath == "" {
			continue
		}
		seriesFolder := movieFolderName(sr.Title, sr.Year)
		eps, _ := w.store.ListEpisodes(ctx, sr.ID)
		moved := false
		for _, ep := range eps {
			if !ep.HasFile || ep.LibraryPath == "" || ep.SceneSeason == 0 {
				continue // no file, or no scene numbering → nothing to correct
			}
			want := w.tvLinkPath(sr.RootFolderPath, seriesFolder, sr.Title,
				[]*media.Episode{ep}, filepath.Ext(ep.LibraryPath))
			if want == ep.LibraryPath {
				continue // already scene-named
			}
			target, terr := os.Readlink(ep.LibraryPath)
			if terr != nil {
				if s := byPath[ep.LibraryPath]; s != nil {
					target = s.TargetPath
				}
			}
			if target == "" {
				continue // unresolvable — don't strand the DB on a path with no file
			}
			if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
				w.logger.Warn("relink: creating dir", "path", want, "error", err)
				continue
			}
			if err := atomicReplaceSymlink(want, target); err != nil {
				w.logger.Warn("relink: linking", "path", want, "error", err)
				continue
			}
			old := ep.LibraryPath
			_ = removeLibrarySymlink(old)
			ep.LibraryPath = want
			if err := w.store.UpdateEpisode(ctx, ep); err != nil {
				w.logger.Warn("relink: updating episode", "id", ep.ID, "error", err)
				continue
			}
			if s := byPath[old]; s != nil {
				_ = w.store.DeleteImportedSymlink(ctx, s.ID)
				_ = w.store.UpsertImportedSymlink(ctx, &job.ImportedSymlink{
					JobID: s.JobID, SymlinkPath: want, TargetPath: s.TargetPath,
				})
			}
			w.logger.Info("relinked episode to scene numbering", "from", old, "to", want, "episode_id", ep.ID)
			moved = true
		}
		if moved {
			w.maybePlexScan(ctx, sr.RootFolderPath, plexKind(sr.SeriesType))
		}
	}
}

// reconcileOnce sweeps the WebDAV mount against known jobs: it upserts a
// webdav_item per release folder, categorizes it, marks vanished items broken,
// and raises an unknown_content notification for items Boxarr did not submit
// (FR-NC-1/2, FR-WD-1/2/3).
func (w *Workers) reconcileOnce(ctx context.Context) error {
	// Heal anime library files that were named with flat TMDB numbering before scene
	// naming existed, so Plex can match (and language-verify) them. Idempotent.
	w.relinkEpisodesToScene(ctx)

	// Capture the sweep marker from the DB clock (not Go's local clock) so the
	// stale check below compares like-for-like against last_seen.
	sweepStart, err := w.store.DBNow(ctx)
	if err != nil {
		return err
	}

	// Index known jobs by release-folder base name (storage_path) and torbox hash.
	// Include healing/heal-failed so an item mid-heal (or that exhausted heals)
	// stays tracked on the mount instead of flipping to "unknown".
	jobs, err := w.store.JobsByState(ctx,
		job.StateCompleted, job.StateImported, job.StateDownloading, job.StateSeeding,
		job.StateQueued, job.StateHealing, job.StateHealFailed)
	if err != nil {
		return err
	}
	byName := map[string]*job.Job{}
	for _, j := range jobs {
		if j.StoragePath != "" {
			byName[filepath.Base(j.StoragePath)] = j
		}
		byName[j.NZBName] = j
	}

	// Library titles: a folder whose parsed title matches a catalog item is
	// tracked even when no job matches it (adopted items have no job; import jobs
	// get reaped). This keeps "known" durable instead of flipping to unknown.
	lib := w.libraryTitleSet(ctx)
	// Mount folders backed by a library item — resolve the actual on-disk library
	// symlinks (ground truth that survives job reaping) plus table records. A
	// folder a library symlink points into is tracked even if its title can't be
	// matched (e.g. a German-titled release of an English-catalog movie).
	targets := w.libraryMountTargets(ctx)

	// Tombstones: paths deleted on TorBox that a stale rclone cache may still
	// list. Skip re-adding them; track which we still see so we can clear the
	// tombstone once the mount finally drops the folder.
	tombs, _ := w.store.ListDeletedPaths(ctx)
	seenTomb := map[string]bool{}

	for _, base := range w.mountBases() {
		entries, derr := os.ReadDir(base)
		if derr != nil {
			w.logger.Debug("reconcile: reading mount", "base", base, "error", derr)
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			remotePath := filepath.Join(base, name)

			j, matched := byName[name]
			if tombs[remotePath] {
				if matched {
					// Re-acquired (a new job owns this path) — lift the tombstone.
					_ = w.store.ClearDeletedPath(ctx, remotePath)
					delete(tombs, remotePath)
				} else {
					// Still a stale listing of a deleted folder — don't re-add it.
					seenTomb[remotePath] = true
					continue
				}
			}

			item := &webdav.WebDAVItem{Name: name, RemotePath: remotePath, Size: dirSize(remotePath)}
			if matched {
				item.Known = true
				item.JobID = j.ID
				item.Category = categoryOf(j.MediaType)
			} else {
				item.Category = guessCategory(name)
				if p, perr := release.ParseRelease(name); perr == nil && p != nil && lib[normTitleKey(p.Title)] {
					item.Known = true // a library item by this title — tracked, just no live job
				} else if folderHasTarget(remotePath, targets) {
					item.Known = true // a library item's file lives in this folder — tracked
				}
			}
			if err := w.store.UpsertWebDAVItem(ctx, item); err != nil {
				w.logger.Error("reconcile: upserting webdav item", "name", name, "error", err)
				continue
			}
			if !item.Known {
				w.maybeNotifyUnknown(ctx, item)
			}
		}
	}

	// GC tombstones whose folder has finally disappeared from the mount.
	for p := range tombs {
		if !seenTomb[p] {
			_ = w.store.ClearDeletedPath(ctx, p)
		}
	}

	if _, err := w.store.MarkWebDAVItemsBrokenNotSeenSince(ctx, sweepStart); err != nil {
		w.logger.Error("reconcile: marking stale items broken", "error", err)
	}
	// Prune long-broken rows (TorBox-rotated content) so the table stays bounded.
	if n, err := w.store.PruneStaleWebDAVItems(ctx, 7); err != nil {
		w.logger.Warn("reconcile: pruning stale items", "error", err)
	} else if n > 0 {
		w.logger.Info("reconcile: pruned stale mount rows", "count", n)
	}
	return nil
}

// mountBases returns the distinct mount roots to sweep (usenet + torrent).
func (w *Workers) mountBases() []string {
	u := w.set.UsenetPath()
	tp := w.set.TorrentPath()
	if tp == u {
		return []string{u}
	}
	return []string{u, tp}
}

// maybeNotifyUnknown raises an unknown_content notification once per item.
func (w *Workers) maybeNotifyUnknown(ctx context.Context, item *webdav.WebDAVItem) {
	// Dedup: skip if an unread unknown_content notification already names it.
	existing, _ := w.store.ListNotifications(ctx, false, 500)
	for _, n := range existing {
		if n.Type == "unknown_content" && containsName(n.Payload, item.RemotePath) {
			return
		}
	}
	payload, _ := json.Marshal(map[string]any{
		"name": item.Name, "size": item.Size, "category": item.Category,
		"remotePath": item.RemotePath, "actions": []string{"adopt", "ignore", "delete"},
	})
	if _, err := w.store.EnqueueNotification(ctx, &notify.Notification{
		Type: "unknown_content", Payload: string(payload),
	}); err != nil {
		w.logger.Error("reconcile: enqueuing unknown_content", "error", err)
	}
}

func containsName(payload, remotePath string) bool {
	var m map[string]any
	if json.Unmarshal([]byte(payload), &m) != nil {
		return false
	}
	rp, _ := m["remotePath"].(string)
	return rp == remotePath
}

func categoryOf(mediaType string) string {
	switch mediaType {
	case "movie":
		return "movie"
	case "episode", "season", "series":
		return "series"
	default:
		return "unknown"
	}
}

// guessCategory best-effort categorizes an unknown release folder by name.
func guessCategory(name string) string {
	p, err := release.ParseRelease(name)
	if err != nil || p == nil {
		return "unknown"
	}
	if p.SeasonNumber > 0 || p.EpisodeStart > 0 || p.IsSeasonPack || p.AirDate != "" || len(p.AbsoluteEpisodes) > 0 {
		return "series"
	}
	if p.Year > 0 {
		return "movie"
	}
	return "unknown"
}

func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, ierr := d.Info(); ierr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// libraryMountTargets returns every mount path a library item points to, read
// from the on-disk library symlinks (ground truth, survives job reaping) plus any
// imported_symlinks table records.
func (w *Workers) libraryMountTargets(ctx context.Context) []string {
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
	if movies, err := w.store.ListMovies(ctx); err == nil {
		for _, m := range movies {
			readlink(m.LibraryPath)
		}
	}
	if series, err := w.store.ListSeries(ctx); err == nil {
		for _, s := range series {
			if eps, e := w.store.ListEpisodes(ctx, s.ID); e == nil {
				for _, ep := range eps {
					readlink(ep.LibraryPath)
				}
			}
		}
	}
	if syms, err := w.store.ListImportedSymlinks(ctx); err == nil {
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

var reTitleNorm = regexp.MustCompile(`[^a-z0-9]+`)

// normTitleKey normalizes a title for matching (mirrors the API's normTitle):
// lower-case, "&"→"and", non-alphanumeric runs → single space.
func normTitleKey(s string) string {
	s = strings.ReplaceAll(strings.ToLower(s), "&", " and ")
	return strings.TrimSpace(reTitleNorm.ReplaceAllString(s, " "))
}

// libraryTitleSet returns the normalized titles of every catalog movie + series,
// so the reconciler can recognize a mount folder as tracked by title alone.
func (w *Workers) libraryTitleSet(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	if movies, err := w.store.ListMovies(ctx); err == nil {
		for _, m := range movies {
			out[normTitleKey(m.Title)] = true
			for _, alt := range m.AltTitles { // German/other-language releases
				out[normTitleKey(alt)] = true
			}
		}
	}
	if series, err := w.store.ListSeries(ctx); err == nil {
		for _, s := range series {
			out[normTitleKey(s.Title)] = true
		}
	}
	return out
}
