package worker

import (
	"context"
	"os"
	"path/filepath"

	"github.com/radaiko/boxarr/internal/job"
)

// removeLibrarySymlink unlinks a direct-to-library symlink and removes its now-
// empty parent (Title (Year) / Season NN) dir, best-effort.
func removeLibrarySymlink(linkPath string) error {
	if err := os.Remove(linkPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = os.Remove(filepath.Dir(linkPath)) // only succeeds if empty
	return nil
}

// deleteGiveUpAttempts bounds how many times the deleter retries a failing
// TorBox deletion before dropping the job row anyway. At the default 1m poll
// interval this is roughly an hour of retries. The download may then be left
// orphaned on TorBox, which is logged at error level.
var deleteGiveUpAttempts = 60

// deleteOnce removes from TorBox every job Sonarr asked to delete with its
// files. TorBox's controlusenetdownload is intermittently flaky (HTTP 500,
// "try again later"), so a failed attempt is retried on the next cycle rather
// than dropping the request.
// DeleteDownloads tears down the given jobs' downloads (TorBox + library symlinks
// + the job row), best-effort, calling report(done, total, name) after each so a
// background task can show progress and the list of removed releases. Used when a
// tracked movie/series is deleted from the library.
func (w *Workers) DeleteDownloads(ctx context.Context, jobIDs []int64, report func(done, total int, name string)) {
	for i, id := range jobIDs {
		name := ""
		if j, err := w.store.GetJob(ctx, id); err == nil {
			name = j.NZBName
			if j.TorBoxID != 0 {
				var derr error
				if j.Protocol == "torrent" {
					derr = w.tb.ControlTorrent(ctx, j.TorBoxID, "delete")
				} else {
					derr = w.tb.ControlUsenet(ctx, j.TorBoxID, "delete")
				}
				if derr != nil {
					w.logger.Warn("delete download: torbox delete failed", "job_id", id, "error", derr)
				}
			}
			if syms, e := w.store.ListImportedSymlinksByJob(ctx, id); e == nil {
				for _, s := range syms {
					_ = removeLibrarySymlink(s.SymlinkPath)
				}
			}
			// Tombstone the mount folder so the reconciler doesn't re-add it as
			// "unknown" while the rclone listing still shows the deleted download.
			if j.StoragePath != "" {
				_ = w.store.AddDeletedPath(ctx, j.StoragePath)
			}
			if e := w.store.DeleteJob(ctx, id); e != nil {
				w.logger.Warn("delete download: dropping job row", "job_id", id, "error", e)
			}
		}
		if report != nil {
			report(i+1, len(jobIDs), name)
		}
	}
}

func (w *Workers) deleteOnce(ctx context.Context) error {
	jobs, err := w.store.JobsByState(ctx, job.StateDeleted)
	if err != nil {
		return err
	}
	active := make(map[int64]bool, len(jobs))
	for _, j := range jobs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		active[j.ID] = true
		w.deleteJob(ctx, j)
	}
	// Drop retry counters for jobs no longer awaiting deletion.
	for id := range w.deleteAttempts {
		if !active[id] {
			delete(w.deleteAttempts, id)
		}
	}
	return nil
}

// deleteJob deletes one job's download from TorBox and removes its row. On a
// transient TorBox failure it keeps the row so the next cycle retries; once
// deleteGiveUpAttempts consecutive failures have elapsed it drops the row
// regardless.
func (w *Workers) deleteJob(ctx context.Context, j *job.Job) {
	log := w.logger.With("job_id", j.ID, "torbox_id", j.TorBoxID)

	if j.TorBoxID != 0 {
		var derr error
		if j.Protocol == "torrent" {
			derr = w.tb.ControlTorrent(ctx, j.TorBoxID, "delete")
		} else {
			derr = w.tb.ControlUsenet(ctx, j.TorBoxID, "delete")
		}
		if err := derr; err != nil {
			w.deleteAttempts[j.ID]++
			if w.deleteAttempts[j.ID] < deleteGiveUpAttempts {
				log.Warn("torbox delete failed; will retry next cycle",
					"attempt", w.deleteAttempts[j.ID], "error", err)
				return // keep the row
			}
			log.Error("torbox delete still failing; dropping job, download may be orphaned",
				"attempts", w.deleteAttempts[j.ID], "error", err)
		} else {
			log.Info("download deleted from torbox")
		}
	}
	delete(w.deleteAttempts, j.ID)
	// Remove direct-to-library symlinks recorded for this job (00 §5.1 importer).
	// Query just this job's symlinks so deleting a whole series (many jobs) is
	// O(N) not O(N²) on the single SQLite connection.
	if syms, err := w.store.ListImportedSymlinksByJob(ctx, j.ID); err == nil {
		for _, s := range syms {
			if rerr := removeLibrarySymlink(s.SymlinkPath); rerr != nil {
				log.Warn("removing library symlink", "path", s.SymlinkPath, "error", rerr)
			}
		}
	}
	// Remove the per-release symlink-farm directory (legacy farm path).
	if j.StoragePath != "" {
		if err := removeSymlinkDir(w.set.SymlinkRoot(), j.StoragePath); err != nil {
			log.Debug("removing symlink-farm directory", "dir", j.StoragePath, "error", err)
		}
		// Tombstone so the reconciler doesn't re-add the deleted folder as unknown.
		_ = w.store.AddDeletedPath(ctx, j.StoragePath)
	}
	w.notifyEvent(ctx, "deletion_completed", j, nil)
	if err := w.store.DeleteJob(ctx, j.ID); err != nil {
		log.Error("removing deleted job row", "error", err)
	}
}
