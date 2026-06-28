package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/settings"
	"github.com/radaiko/boxarr/internal/torbox"
)

// persistRateLimit remembers a TorBox throttle: it stores the cooldown (so it
// survives restarts) and records the event. TorBox's NZB-creation limit is a
// short rolling window (a 429 carries a minutes-long Retry-After), so the cooldown
// alone paces submission — we deliberately do NOT learn a daily cap from it, which
// previously froze the whole queue for a UTC day and ratcheted permanently down.
func (w *Workers) persistRateLimit(ctx context.Context, cooldown time.Duration) {
	until := time.Now().Add(cooldown)
	_ = w.set.Set(ctx, settings.KeyTorBoxCooldownUntil, until.UTC().Format(time.RFC3339))
	hour, _ := w.store.CountJobsSubmittedSince(ctx, time.Now().Add(-time.Hour))
	_ = w.store.RecordLimitEvent(ctx, "rate_limit",
		fmt.Sprintf("rate-limited after %d grabs in the last hour; cooldown %s", hour, cooldown.Round(time.Second)))
}

// redundantPendingReason returns a non-empty reason when a pending job should be
// retired instead of submitted: its media is already imported, or another job for
// the same media is already in flight/imported (a duplicate grab).
func (w *Workers) redundantPendingReason(ctx context.Context, j *job.Job) string {
	if j.MediaRef == 0 {
		return ""
	}
	if j.IsUpgrade {
		// An upgrade legitimately replaces an already-imported item; the
		// upgrade search guards against duplicate upgrades before grabbing.
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
	// Respect a persisted cooldown (survives restarts). Submission is paced solely
	// by TorBox's own 429 cooldowns — no day-long freeze.
	if cu := w.set.TorBoxCooldownUntil(); !cu.IsZero() && time.Now().Before(cu) {
		return nil
	}
	jobs, err := w.store.JobsByState(ctx, job.StatePending)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil // nothing to submit — skip the rate/cooldown TorBox calls
	}

	// Proactively pace submissions under TorBox's create limit (default 60/hour)
	// so we never trip its account cooldown. budget < 0 means "no cap configured".
	budget := -1
	if cap := w.set.MaxCreatePerHour(); cap > 0 {
		hour, _ := w.store.CountJobsSubmittedSince(ctx, time.Now().Add(-time.Hour))
		if int(hour) >= cap {
			return nil // rolling hourly ceiling reached; a later tick resumes as grabs age out
		}
		budget = cap - int(hour)
		if perTick := perTickBudget(cap, w.set.PollInterval()); perTick < budget {
			budget = perTick // spread the hourly budget across ticks instead of bursting
		}
	}

	// Respect TorBox's account-level cooldown (from /user/me): while it's in
	// cooldown, queued downloads don't progress and new submissions only renew it.
	if until := w.torboxAccountCooldown(ctx); !until.IsZero() {
		w.submitBackoffUntil = until
		_ = w.set.Set(ctx, settings.KeyTorBoxCooldownUntil, until.UTC().Format(time.RFC3339))
		w.logger.Warn("torbox account in cooldown; pausing submissions", "until", until)
		_ = w.store.RecordLimitEvent(ctx, "account_cooldown",
			"TorBox account in cooldown until "+until.UTC().Format(time.RFC3339))
		return nil
	}

	submitted := 0
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
		submitted++
		if budget >= 0 && submitted >= budget {
			return nil // hit this tick's pacing budget; the next tick continues draining
		}
	}
	return nil
}

// perTickBudget spreads an hourly create cap across submitter ticks so a backlog
// drains at a steady rate instead of bursting the whole hour's budget at once
// (which is what trips TorBox's rate limit + account cooldown). At least 1/tick.
func perTickBudget(capPerHour int, poll time.Duration) int {
	if poll <= 0 {
		return capPerHour
	}
	n := int(int64(capPerHour) * int64(poll) / int64(time.Hour))
	if n < 1 {
		n = 1
	}
	return n
}

// torboxAccountCooldown returns the TorBox account's cooldown end time when it is
// currently in cooldown (per /user/me), or the zero time otherwise. A failed
// lookup is treated as "not in cooldown" so a transient API error never wedges
// the submitter.
func (w *Workers) torboxAccountCooldown(ctx context.Context) time.Time {
	u, err := w.tb.UserMe(ctx)
	if err != nil || u == nil || u.CooldownUntil == "" {
		return time.Time{}
	}
	until, perr := time.Parse(time.RFC3339, u.CooldownUntil)
	if perr != nil || !time.Now().Before(until) {
		return time.Time{}
	}
	return until
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
