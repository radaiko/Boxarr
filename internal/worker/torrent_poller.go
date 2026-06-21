package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/torbox"
)

// torrentActiveStates are the torrent job states the poller tracks.
var torrentActiveStates = []job.State{job.StateQueued, job.StateDownloading, job.StateSeeding}

// pollTorrentsOnce fetches the TorBox torrent list once and reconciles every
// active torrent job. Mirrors pollOnce with its own missing-poll map.
func (w *Workers) pollTorrentsOnce(ctx context.Context) error {
	jobs, err := w.store.JobsByState(ctx, torrentActiveStates...)
	if err != nil {
		return err
	}
	// Filter to torrent protocol (usenet jobs share queued/downloading states).
	torrents := jobs[:0:0]
	for _, j := range jobs {
		if j.Protocol == "torrent" {
			torrents = append(torrents, j)
		}
	}
	if len(torrents) == 0 {
		clear(w.torrentMissingPolls)
		return nil
	}
	list, err := w.tb.ListTorrents(ctx)
	if err != nil {
		return fmt.Errorf("listing torbox torrents: %w", err)
	}
	byID := make(map[int64]torbox.TorrentDownload, len(list))
	for _, d := range list {
		byID[int64(d.ID)] = d
	}
	stillActive := make(map[int64]bool, len(torrents))
	var awaiting, ongoing bool
	for _, j := range torrents {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		stillActive[j.TorBoxID] = true
		rec, ok := byID[j.TorBoxID]
		if !ok {
			if failed := w.handleTorrentMissing(ctx, j); !failed {
				ongoing = true
			}
			continue
		}
		delete(w.torrentMissingPolls, j.TorBoxID)
		switch w.reconcileTorrent(ctx, j, rec) {
		case reconcileOngoing:
			ongoing = true
		case reconcileAwaitingWebDAV:
			awaiting = true
		}
	}
	for tbID := range w.torrentMissingPolls {
		if !stillActive[tbID] {
			delete(w.torrentMissingPolls, tbID)
		}
	}
	if awaiting && !ongoing {
		w.maybeRefreshWebDAV(ctx)
	}
	return nil
}

func (w *Workers) handleTorrentMissing(ctx context.Context, j *job.Job) bool {
	w.torrentMissingPolls[j.TorBoxID]++
	n := w.torrentMissingPolls[j.TorBoxID]
	log := w.logger.With("job_id", j.ID, "torbox_id", j.TorBoxID, "consecutive_misses", n)
	if n < missingPollThreshold {
		log.Debug("active torrent missing from torbox list")
		return false
	}
	j.State = job.StateFailed
	j.FailMessage = fmt.Sprintf("torrent no longer present on TorBox (absent for %d polls)", n)
	if err := w.store.UpdateJob(ctx, j); err != nil {
		log.Error("persisting failed state for missing torrent", "error", err)
		return false
	}
	delete(w.torrentMissingPolls, j.TorBoxID)
	log.Warn("torrent job failed: vanished from torbox")
	return true
}

// reconcileTorrent applies one TorBox torrent record to its job. Completion is
// the AND of download_finished && download_present regardless of download_state
// (a torrent may read "uploading" while files are already present).
func (w *Workers) reconcileTorrent(ctx context.Context, j *job.Job, rec torbox.TorrentDownload) reconcileResult {
	log := w.logger.With("job_id", j.ID, "torbox_id", j.TorBoxID)

	if rec.Failed() {
		j.State = job.StateFailed
		j.FailMessage = "TorBox reported state: " + rec.DownloadState
		if err := w.store.UpdateJob(ctx, j); err != nil {
			log.Error("persisting failed state", "error", err)
		}
		log.Warn("torrent failed on torbox", "download_state", rec.DownloadState)
		w.onGrabFailed(ctx, j)
		return reconcileSettled
	}

	j.TotalBytes = rec.Size
	j.DownloadedBytes = rec.DownloadedBytes()
	j.ProgressPct = rec.ProgressPct()
	j.ETASeconds = rec.ETASeconds()

	if rec.DownloadFinished && rec.DownloadPresent {
		sourceDir, err := w.resolveStoragePathIn(ctx, w.set.TorrentPath(), rec.Name)
		if err != nil {
			log.Debug("waiting for torrent webdav path", "error", err)
			if uerr := w.store.UpdateJob(ctx, j); uerr != nil {
				log.Error("persisting progress", "error", uerr)
			}
			return reconcileAwaitingWebDAV
		}
		if j.MediaType != "" {
			if ierr := w.importMedia(ctx, j, sourceDir); ierr != nil {
				log.Debug("torrent import not ready, will retry", "error", ierr)
				if uerr := w.store.UpdateJob(ctx, j); uerr != nil {
					log.Error("persisting progress", "error", uerr)
				}
				return reconcileAwaitingWebDAV
			}
			return reconcileSettled
		}
		// Legacy farm fallback (no media link).
		storagePath, files, ferr := buildSymlinkFarm(w.set.SymlinkRoot(), j.Category, rec.Name, sourceDir)
		if ferr != nil || files == 0 {
			if ferr != nil {
				log.Error("building symlink farm", "error", ferr)
			}
			if storagePath != "" && files == 0 {
				_ = removeSymlinkDir(w.set.SymlinkRoot(), storagePath)
			}
			if uerr := w.store.UpdateJob(ctx, j); uerr != nil {
				log.Error("persisting progress", "error", uerr)
			}
			return reconcileAwaitingWebDAV
		}
		now := time.Now()
		j.State = job.StateCompleted
		j.StoragePath = storagePath
		j.ProgressPct = 100
		j.ETASeconds = 0
		j.CompletedAt = &now
		if err := w.store.UpdateJob(ctx, j); err != nil {
			log.Error("persisting completed state", "error", err)
		}
		return reconcileSettled
	}

	// Map download_state onto queued/downloading/seeding (completion handled above).
	switch rec.DownloadState {
	case "uploading":
		j.State = job.StateSeeding
	case "metaDL", "checkingResumeData", "paused":
		// intermediate: keep current state, keep polling
	default:
		if j.State == job.StateQueued {
			j.State = job.StateDownloading
		}
	}
	if err := w.store.UpdateJob(ctx, j); err != nil {
		log.Error("persisting progress", "error", err)
	}
	return reconcileOngoing
}
