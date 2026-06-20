// Package notify defines Boxarr's notification-center domain type.
package notify

import "time"

// Notification is one entry in the notification center. Type is one of
// download_completed | grab_failed | heal_triggered | heal_succeeded |
// heal_failed | deletion_completed | limit_reached | unknown_content. Payload is
// a JSON blob whose shape depends on Type (see docs/specs/04-internal-api.md §9).
type Notification struct {
	ID        int64
	Type      string
	Payload   string // JSON; defaults to "{}"
	JobID     int64  // 0 = none (nullable FK)
	Read      bool
	CreatedAt time.Time
	ReadAt    *time.Time
}
