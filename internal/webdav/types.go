// Package webdav defines the cached view of one release folder on the flat
// TorBox WebDAV mount (the reconciler's source of truth for the WebDAV view).
package webdav

import "time"

// WebDAVItem is one release folder on the mount, as last seen by the reconciler.
type WebDAVItem struct {
	ID         int64
	Name       string
	RemotePath string
	Size       int64
	Category   string // "movie"|"series"|"unknown"
	Known      bool
	JobID      int64 // 0 = none
	IsBroken   bool
	FirstSeen  time.Time
	LastSeen   time.Time
	CreatedAt  time.Time
}
