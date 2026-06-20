package worker

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/torbox"
)

// submitTorrentsOnce submits every pending torrent job to TorBox. It mirrors the
// usenet submitter but keeps its own back-off so a usenet 429 never pauses it.
func (w *Workers) submitTorrentsOnce(ctx context.Context) error {
	if now := time.Now(); now.Before(w.torrentSubmitBackoffUntil) {
		return nil
	}
	jobs, err := w.store.JobsByState(ctx, job.StatePending)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if j.Protocol != "torrent" {
			continue
		}
		if cooldown := w.submitTorrentJob(ctx, j); cooldown > 0 {
			w.torrentSubmitBackoffUntil = time.Now().Add(cooldown)
			return nil
		}
	}
	return nil
}

// submitTorrentJob submits one torrent job (magnet preferred over .torrent bytes).
func (w *Workers) submitTorrentJob(ctx context.Context, j *job.Job) time.Duration {
	log := w.logger.With("job_id", j.ID, "name", j.NZBName)
	j.State = job.StateSubmitting
	if err := w.store.UpdateJob(ctx, j); err != nil {
		log.Error("marking torrent job submitting", "error", err)
		return 0
	}
	req := torbox.TorrentCreateRequest{
		Magnet:         j.TorrentMagnet,
		TorrentContent: j.TorrentFile,
		TorrentName:    j.NZBName,
	}
	var result *torbox.TorrentCreateResult
	bo := backoff.WithContext(newRetryPolicy(), ctx)
	err := backoff.Retry(func() error {
		r, cerr := w.tb.CreateTorrent(ctx, req)
		if cerr != nil {
			if _, limited := torbox.RateLimit(cerr); limited {
				return backoff.Permanent(cerr)
			}
			if torbox.Retryable(cerr) {
				log.Warn("transient torrent submit error, retrying", "error", cerr)
				return cerr
			}
			return backoff.Permanent(cerr)
		}
		result = r
		return nil
	}, bo)

	if err != nil {
		if retryAfter, limited := torbox.RateLimit(err); limited {
			cooldown := retryAfter
			if cooldown <= 0 {
				cooldown = defaultRateLimitCooldown
			}
			if cooldown > time.Hour {
				cooldown = time.Hour
			}
			j.State = job.StatePending
			if uerr := w.store.UpdateJob(ctx, j); uerr != nil {
				log.Error("reverting rate-limited torrent job", "error", uerr)
			}
			log.Warn("torbox rate-limited torrent submit; pausing", "cooldown", cooldown.String())
			return cooldown
		}
		j.State = job.StateFailed
		j.FailMessage = "TorBox torrent submission failed: " + err.Error()
		if uerr := w.store.UpdateJob(ctx, j); uerr != nil {
			log.Error("marking torrent job failed", "error", uerr)
		}
		log.Error("torrent submission permanently failed", "error", err)
		return 0
	}

	now := time.Now()
	id := int64(result.TorrentID)
	if id == 0 {
		id = int64(result.QueuedID)
	}
	j.State = job.StateQueued
	j.TorBoxID = id
	j.TorBoxHash = result.Hash
	j.SubmittedAt = &now
	if err := w.store.UpdateJob(ctx, j); err != nil {
		log.Error("marking torrent job queued", "error", err)
		return 0
	}
	log.Info("torrent submitted to torbox", "torbox_id", j.TorBoxID)
	return 0
}
