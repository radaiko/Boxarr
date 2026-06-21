package worker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/notify"
	"github.com/radaiko/boxarr/internal/release"
	"github.com/radaiko/boxarr/internal/webdav"
)

// Reconcile runs one reconcile sweep on demand (POST /api/v1/webdav/refresh).
func (w *Workers) Reconcile(ctx context.Context) error { return w.reconcileOnce(ctx) }

// reconcileOnce sweeps the WebDAV mount against known jobs: it upserts a
// webdav_item per release folder, categorizes it, marks vanished items broken,
// and raises an unknown_content notification for items Boxarr did not submit
// (FR-NC-1/2, FR-WD-1/2/3).
func (w *Workers) reconcileOnce(ctx context.Context) error {
	sweepStart := timeNow()

	// Index known jobs by release-folder base name (storage_path) and torbox hash.
	jobs, err := w.store.JobsByState(ctx,
		job.StateCompleted, job.StateImported, job.StateDownloading, job.StateSeeding, job.StateQueued)
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
			size := dirSize(remotePath)

			item := &webdav.WebDAVItem{Name: name, RemotePath: remotePath, Size: size}
			if j, ok := byName[name]; ok {
				item.Known = true
				item.JobID = j.ID
				item.Category = categoryOf(j.MediaType)
			} else {
				item.Category = guessCategory(name)
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

	if _, err := w.store.MarkWebDAVItemsBrokenNotSeenSince(ctx, sweepStart); err != nil {
		w.logger.Error("reconcile: marking stale items broken", "error", err)
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
