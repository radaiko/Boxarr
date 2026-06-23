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

// onGrabFailed emits a grab_failed notification and marks the linked media item
// `failed` (FR-GP-7). It is intentionally NOT returned to `wanted`: auto-retry
// would just re-grab the same broken release in a loop. The item shows the
// failure reason (from the job) and waits for a manual reset/retry, which the
// failed status keeps out of the auto-search query (WantedMovies/WantedEpisodes).
func (w *Workers) onGrabFailed(ctx context.Context, j *job.Job) {
	w.notifyEvent(ctx, "grab_failed", j, nil)
	switch j.MediaType {
	case "movie":
		_ = w.store.SetMovieStatus(ctx, j.MediaRef, media.MediaFailed)
	case "episode":
		_ = w.store.SetEpisodeStatus(ctx, j.MediaRef, media.MediaFailed)
	}
}
