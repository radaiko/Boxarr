// Package media defines Boxarr's catalog domain types — series, seasons,
// episodes, and movies — and the per-item acquisition status (MediaStatus),
// which is distinct from a download job's lifecycle state (internal/job).
package media

import "time"

// MediaStatus is a catalog item's acquisition lifecycle (per movie / per
// episode). It is independent of job.State (the per-download lifecycle); the
// reconciler keeps the two in sync (see docs/specs/02-data-model.md §2.2).
type MediaStatus string

const (
	MediaWanted      MediaStatus = "wanted"         // monitored, eligible (aired/released), no file
	MediaSearching   MediaStatus = "searching"      // a search/grab is in flight
	MediaQueued      MediaStatus = "queued"         // grabbed; job queued on TorBox, not yet downloading
	MediaDownloading MediaStatus = "downloading"    // a linked job is actively downloading/seeding
	MediaAvailable   MediaStatus = "available"      // imported; library symlink present
	MediaMissing     MediaStatus = "missing"        // monitored but not yet eligible, or unmonitored
	MediaExpired     MediaStatus = "expired_broken" // was available; WebDAV target gone (heal candidate)
)

// Series is a monitored TV show.
type Series struct {
	ID               int64
	TMDBID           int64
	TVDBID           int64 // 0 = unresolved
	IMDBID           string
	Title            string
	SortTitle        string
	Year             int
	Overview         string
	SeriesType       string // "standard"|"daily"|"anime"
	TMDBStatus       string
	Monitored        bool
	SeasonFolder     bool
	QualityProfileID int64
	RootFolderPath   string
	LibraryPath      string
	PosterPath       string
	BackdropPath     string
	MetadataJSON     string
	LastMetadataSync *time.Time
	AddedAt          time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Season is one season of a Series.
type Season struct {
	ID           int64
	SeriesID     int64
	SeasonNumber int
	Monitored    bool
	EpisodeCount int
	AirDate      string // "YYYY-MM-DD" | ""
	PosterPath   string
	MetadataJSON string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Episode is one episode of a Series.
type Episode struct {
	ID             int64
	SeriesID       int64
	SeasonID       int64
	SeasonNumber   int
	EpisodeNumber  int
	AbsoluteNumber int // 0 = none
	TMDBID         int64
	TVDBID         int64
	Title          string
	Overview       string
	AirDate        string // "YYYY-MM-DD" | ""
	Runtime        int
	StillPath      string
	Status         MediaStatus
	Monitored      bool
	HasFile        bool
	JobID          int64 // 0 = none
	LibraryPath    string
	MetadataJSON   string
	LastSearchedAt *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Movie is a monitored film.
type Movie struct {
	ID                  int64
	TMDBID              int64
	IMDBID              string
	Title               string
	SortTitle           string
	Year                int
	Overview            string
	TMDBStatus          string
	MinimumAvailability string // "announced"|"inCinemas"|"released"|"preDB"
	ReleaseDate         string // "YYYY-MM-DD" | ""
	DigitalRelease      string
	PhysicalRelease     string
	Runtime             int
	Status              MediaStatus
	Monitored           bool
	HasFile             bool
	QualityProfileID    int64
	RootFolderPath      string
	LibraryPath         string
	JobID               int64 // 0 = none
	PosterPath          string
	BackdropPath        string
	MetadataJSON        string
	LastMetadataSync    *time.Time
	LastSearchedAt      *time.Time
	AddedAt             time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
