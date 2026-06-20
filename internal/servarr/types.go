// Package servarr defines the backing rows for Boxarr's Sonarr/Radarr v3
// emulation: quality profiles, root folders, and tags returned to Overseerr/
// Jellyseerr (see docs/specs/05-seerr-emulation.md).
package servarr

import "time"

// QualityProfile is a Servarr quality profile (Seerr reads only id + name).
type QualityProfile struct {
	ID        int64
	Name      string
	IsDefault bool
	CreatedAt time.Time
}

// RootFolder is a Servarr root folder. MediaKind ("tv"|"movie") routes it to the
// right emulated surface; Path must round-trip into the add-media POST body.
type RootFolder struct {
	ID        int64
	Path      string
	MediaKind string
	CreatedAt time.Time
}

// Tag is a Servarr tag (Seerr reads id + label; usually the list is empty).
type Tag struct {
	ID        int64
	Label     string
	CreatedAt time.Time
}
