package worker

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/settings"
	"github.com/radaiko/boxarr/internal/torbox"
)

// startOfDayUTC is midnight UTC today — the window for the learned daily-grab cap.
func startOfDayUTC() time.Time {
	t := time.Now().UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// persistRateLimit remembers a TorBox throttle: it stores the cooldown (so it
// survives restarts), records the event, and lowers the learned daily-grab cap to
// the number of grabs that triggered the limit.
func (w *Workers) persistRateLimit(ctx context.Context, cooldown time.Duration) {
	until := time.Now().Add(cooldown)
	_ = w.set.Set(ctx, settings.KeyTorBoxCooldownUntil, until.UTC().Format(time.RFC3339))
	today, _ := w.store.CountJobsSubmittedSince(ctx, startOfDayUTC())
	detail := fmt.Sprintf("rate-limited after %d grabs today; cooldown %s", today, cooldown.Round(time.Second))
	if cur := w.set.TorBoxDailyCap(); today > 0 && (cur == 0 || int(today) < cur) {
		_ = w.set.Set(ctx, settings.KeyTorBoxDailyCap, strconv.Itoa(int(today)))
		detail += fmt.Sprintf("; learned daily cap = %d", today)
	}
	_ = w.store.RecordLimitEvent(ctx, "rate_limit", detail)
}

// redundantPendingReason returns a non-empty reason when a pending job should be
// retired instead of submitted: its media is already imported, or another job for
// the same media is already in flight/imported (a duplicate grab).
func (w *Workers) redundantPendingReason(ctx context.Context, j *job.Job) string {
	if j.MediaRef == 0 {
		return ""
	}
	switch j.MediaType {
	case "movie":
		if m, err := w.store.GetMovie(ctx, j.MediaRef); err == nil && m.HasFile {
			return "movie already imported"
		}
	case "episode":
		if ep, err := w.store.GetEpisode(ctx, j.MediaRef); err == nil && ep.HasFile {
			return "episode already imported"
		}
	}
	if ahead, _ := w.store.JobAheadForMedia(ctx, j.ID, j.MediaType, j.MediaRef); ahead {
		return "duplicate of another active job"
	}
	return ""
}

// defaultRateLimitCooldown is how long the submitter pauses after a TorBox
// 429 that carried no Retry-After hint. TorBox caps NZB creation at 60/hour,
// so a short pause is enough to stop hammering a closed window.
const defaultRateLimitCooldown = 5 * time.Minute

// submitOnce submits every pending job to TorBox.
func (w *Workers) submitOnce(ctx context.Context) error {
	if now := time.Now(); now.Before(w.submitBackoffUntil) {
		return nil // still cooling down from a TorBox rate-limit
	}
	// Respect a persisted cooldown (survives restarts) and the learned daily cap.
	if cu := w.set.TorBoxCooldownUntil(); !cu.IsZero() && time.Now().Before(cu) {
		return nil
	}
	if cap := w.set.TorBoxDailyCap(); cap > 0 {
		if today, _ := w.store.CountJobsSubmittedSince(ctx, startOfDayUTC()); int(today) >= cap {
			w.submitBackoffUntil = startOfDayUTC().Add(24 * time.Hour) // resume tomorrow
			return nil
		}
	}
	jobs, err := w.store.JobsByState(ctx, job.StatePending)
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if j.Protocol == "torrent" {
			continue // torrent jobs are driven by the torrent submitter (added in a later increment)
		}
		if reason := w.redundantPendingReason(ctx, j); reason != "" {
			// Don't re-download something already imported / handled by another
			// job; retire the duplicate so it leaves the queue.
			j.State = job.StateManuallyResolved
			j.FailMessage = "superseded: " + reason
			if uerr := w.store.UpdateJob(ctx, j); uerr != nil {
				w.logger.Error("retiring superseded job", "job_id", j.ID, "error", uerr)
			} else {
				w.logger.Info("retired superseded pending job", "job_id", j.ID, "reason", reason)
			}
			continue
		}
		if cooldown := w.submitJob(ctx, j); cooldown > 0 {
			// TorBox is throttling: the job is back in `pending` and the
			// rest of the queue would only earn more 429s. Pause and let a
			// later tick drain it once the window clears.
			w.submitBackoffUntil = time.Now().Add(cooldown)
			return nil
		}
	}
	return nil
}

// submitJob submits one job, retrying transient TorBox errors with backoff.
// On a TorBox rate-limit (429) it leaves the job pending and returns the
// cooldown the submitter should observe; otherwise it returns 0.
func (w *Workers) submitJob(ctx context.Context, j *job.Job) time.Duration {
	log := w.logger.With("job_id", j.ID, "nzb_name", j.NZBName)
	j.State = job.StateSubmitting
	if err := w.store.UpdateJob(ctx, j); err != nil {
		log.Error("marking job submitting", "error", err)
		return 0
	}

	req := torbox.CreateRequest{
		NZBContent: j.NZBContent,
		NZBName:    j.NZBName + ".nzb",
		Link:       j.NZBURL,
	}

	var result *torbox.CreateResult
	bo := backoff.WithContext(newRetryPolicy(), ctx)
	err := backoff.Retry(func() error {
		r, err := w.tb.CreateUsenetDownload(ctx, req)
		if err != nil {
			if _, limited := torbox.RateLimit(err); limited {
				// A 429 will not clear within the retry budget; stop now and
				// let submitOnce schedule a much longer cooldown instead.
				return backoff.Permanent(err)
			}
			if torbox.Retryable(err) {
				log.Warn("transient submit error, retrying", "error", err)
				return err
			}
			return backoff.Permanent(err)
		}
		result = r
		return nil
	}, bo)

	if err != nil {
		if retryAfter, limited := torbox.RateLimit(err); limited {
			// Not a job failure: TorBox is throttling. Revert to pending so a
			// later tick retries this NZB once the rate-limit window clears.
			cooldown := retryAfter
			if cooldown <= 0 {
				cooldown = defaultRateLimitCooldown
			}
			if cooldown > time.Hour {
				cooldown = time.Hour
			}
			j.State = job.StatePending
			if uerr := w.store.UpdateJob(ctx, j); uerr != nil {
				log.Error("reverting rate-limited job to pending", "error", uerr)
			}
			w.persistRateLimit(ctx, cooldown) // remember cooldown + learn the daily cap
			log.Warn("torbox rate-limited submission; pausing submitter",
				"error", err, "cooldown", cooldown.String())
			return cooldown
		}
		j.State = job.StateFailed
		j.FailMessage = "TorBox submission failed: " + err.Error()
		if uerr := w.store.UpdateJob(ctx, j); uerr != nil {
			log.Error("marking job failed", "error", uerr)
		}
		log.Error("submission permanently failed", "error", err)
		return 0
	}

	now := time.Now()
	j.State = job.StateQueued
	j.TorBoxID = int64(result.UsenetDownloadID)
	j.TorBoxHash = result.Hash
	j.SubmittedAt = &now
	if err := w.store.UpdateJob(ctx, j); err != nil {
		log.Error("marking job queued", "error", err)
		return 0
	}
	log.Info("job submitted to torbox", "torbox_id", j.TorBoxID)
	return 0
}

// newRetryPolicy returns the backoff policy for transient TorBox errors:
// capped exponential backoff, max ~2 minutes total.
func newRetryPolicy() backoff.BackOff {
	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 2 * time.Second
	bo.MaxInterval = 30 * time.Second
	bo.MaxElapsedTime = 2 * time.Minute
	return bo
}
