package worker

import (
	"context"
	"encoding/json"

	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/notify"
)

// notifyEvent enqueues a notification-center event for a job (FR-NC-3). It is
// best-effort: a failure to persist the notification never blocks the pipeline.
func (w *Workers) notifyEvent(ctx context.Context, eventType string, j *job.Job, extra map[string]any) {
	payload := map[string]any{
		"name":      j.NZBName,
		"protocol":  j.Protocol,
		"mediaType": j.MediaType,
		"mediaRef":  j.MediaRef,
	}
	if j.FailMessage != "" {
		payload["error"] = j.FailMessage
	}
	for k, v := range extra {
		payload[k] = v
	}
	b, _ := json.Marshal(payload)
	if _, err := w.store.EnqueueNotification(ctx, &notify.Notification{
		Type: eventType, Payload: string(b), JobID: j.ID,
	}); err != nil {
		w.logger.Error("enqueuing notification", "type", eventType, "error", err)
	}
}

// onGrabFailed emits a grab_failed notification, blocklists the broken release so
// it's never grabbed again, and returns the item to `wanted` for an auto-retry —
// the next auto-search grabs a DIFFERENT release (the blocklist prevents the
// re-grab-the-same-broken-release loop the old code avoided by leaving it failed).
// When no other release is acceptable the item just stays wanted and keeps
// searching as new releases appear. Falls back to `failed` if the reset can't run.
func (w *Workers) onGrabFailed(ctx context.Context, j *job.Job) {
	w.notifyEvent(ctx, "grab_failed", j, nil)
	if j.NZBName != "" {
		reason := j.FailMessage
		if reason == "" {
			reason = "download failed"
		}
		if err := w.store.BlocklistGrab(ctx, j.NZBName, reason); err != nil {
			w.logger.Error("blocklisting failed release", "release", j.NZBName, "error", err)
		}
	}
	switch j.MediaType {
	case "movie":
		if err := w.store.ResetMovieForRetry(ctx, j.MediaRef); err != nil {
			_ = w.store.SetMovieStatus(ctx, j.MediaRef, media.MediaFailed)
		}
	case "episode":
		if err := w.store.ResetEpisodeForRetry(ctx, j.MediaRef); err != nil {
			_ = w.store.SetEpisodeStatus(ctx, j.MediaRef, media.MediaFailed)
		}
	}
	w.logger.Info("grab failed — blocklisted release, re-searching for a different one",
		"job_id", j.ID, "release", j.NZBName, "mediaType", j.MediaType, "mediaRef", j.MediaRef)
}
