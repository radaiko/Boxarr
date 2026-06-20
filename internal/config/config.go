// Package config loads and validates boxarr configuration from the environment.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/kelseyhightower/envconfig"
)

// Config holds all runtime configuration, populated from BOXARR_* env vars.
type Config struct {
	TorBoxAPIToken      string        `envconfig:"TORBOX_API_TOKEN" required:"true"`
	WebDAVMountRoot     string        `envconfig:"WEBDAV_MOUNT_ROOT" required:"true"`
	WebDAVUsenetSubpath string        `envconfig:"WEBDAV_USENET_SUBPATH"`
	SymlinkRoot         string        `envconfig:"SYMLINK_ROOT" required:"true"` // transitional: reused worker depends on it; removed in Phase 1 (direct-to-library importer)
	ListenAddr          string        `envconfig:"LISTEN_ADDR" default:":8080"`
	DatabasePath        string        `envconfig:"DATABASE_PATH" default:"/config/boxarr.db"`
	PollInterval        time.Duration `envconfig:"POLL_INTERVAL" default:"1m"`
	LogLevel            string        `envconfig:"LOG_LEVEL" default:"info"`
	Categories          []string      `envconfig:"CATEGORIES" default:"sonarr,radarr,sonarr-anime"` // transitional (Phase 1 removes it)

	// TorBox WebDAV /refresh integration. TorBox's WebDAV listing only
	// refreshes every 15 minutes; hitting /refresh forces it sooner. The
	// feature is active only when both credentials are set.
	TorBoxWebDAVUser       string        `envconfig:"TORBOX_WEBDAV_USER"`
	TorBoxWebDAVPass       string        `envconfig:"TORBOX_WEBDAV_PASS"`
	TorBoxWebDAVRefreshURL string        `envconfig:"TORBOX_WEBDAV_REFRESH_URL" default:"https://webdav.torbox.app/refresh"`
	WebDAVRefreshCooldown  time.Duration `envconfig:"TORBOX_WEBDAV_REFRESH_COOLDOWN" default:"2m"`

	HealEnabled        bool          `envconfig:"HEAL_ENABLED" default:"false"`
	HealInterval       time.Duration `envconfig:"HEAL_INTERVAL" default:"1h"`
	HealLibraryRoots   []string      `envconfig:"HEAL_LIBRARY_ROOTS"`
	HealDryRun         bool          `envconfig:"HEAL_DRY_RUN" default:"false"`
	HealMaxAttempts    int           `envconfig:"HEAL_MAX_ATTEMPTS" default:"3"`
	HealBackoffInitial time.Duration `envconfig:"HEAL_BACKOFF_INITIAL" default:"5m"`
	HealWebhookURL     string        `envconfig:"HEAL_WEBHOOK_URL"`
	HealWebhookEvents  []string      `envconfig:"HEAL_WEBHOOK_EVENTS" default:"failed"`
	// On a dead stored artifact, fall back to a fresh Prowlarr re-search (00 §19.4 / FR-HEAL-2).
	HealProwlarrFallback bool `envconfig:"HEAL_PROWLARR_FALLBACK" default:"true"`

	// ── App auth (NEW) ──
	// /api/v1 SPA auth (X-Api-Key); empty + loopback request = allowed (04 §1).
	APIKey string `envconfig:"API_KEY"`

	// ── Torrent WebDAV path (NEW — 00 §19.5; empty = same flat root as usenet) ──
	WebDAVTorrentSubpath string `envconfig:"WEBDAV_TORRENT_SUBPATH"`

	// ── Indexers / metadata / playback (NEW) ──
	ProwlarrURL      string        `envconfig:"PROWLARR_URL" required:"true"`
	ProwlarrAPIKey   string        `envconfig:"PROWLARR_API_KEY" required:"true"`
	TMDBAPIKey       string        `envconfig:"TMDB_API_KEY" required:"true"`
	TVDBAPIKey       string        `envconfig:"TVDB_API_KEY"`
	TVDBPin          string        `envconfig:"TVDB_PIN"`
	PlexURL          string        `envconfig:"PLEX_URL"`
	PlexToken        string        `envconfig:"PLEX_TOKEN"`
	PlexMovieSection string        `envconfig:"PLEX_MOVIE_SECTION"`
	PlexTVSection    string        `envconfig:"PLEX_TV_SECTION"`
	PlexScanTimeout  time.Duration `envconfig:"PLEX_SCAN_TIMEOUT" default:"60s"`

	// ── Seerr emulation inbound keys (NEW — 05) ──
	SeerrAPIKeys []string `envconfig:"SEERR_API_KEYS"`

	// ── Library roots (NEW — Plex-standard layout; supersede SymlinkRoot in Phase 1) ──
	MovieLibraryRoot string `envconfig:"MOVIE_LIBRARY_ROOT" default:"/data/movies"`
	TVLibraryRoot    string `envconfig:"TV_LIBRARY_ROOT" default:"/data/tv"`

	// ── Same-path escape hatch (NEW — 03 Plex) ──
	HostToPlexPathPrefix string `envconfig:"HOST_TO_PLEX_PATH_PREFIX"`

	// ── Intervals (NEW) ──
	ReconcileInterval time.Duration `envconfig:"RECONCILE_INTERVAL" default:"15m"`
	MetadataInterval  time.Duration `envconfig:"METADATA_REFRESH_INTERVAL" default:"24h"`
	SearchInterval    time.Duration `envconfig:"SEARCH_INTERVAL" default:"6h"`

	// ── Selection score knobs (NEW — FR-SR-5; algorithm in 06 §3) ──
	SelectAllowedResolutions   []string `envconfig:"SELECT_ALLOWED_RESOLUTIONS"`
	SelectMinSize              int64    `envconfig:"SELECT_MIN_SIZE" default:"0"`
	SelectMaxSize              int64    `envconfig:"SELECT_MAX_SIZE" default:"0"`
	SelectSizeLimits           string   `envconfig:"SELECT_SIZE_LIMITS" default:"{}"`
	SelectMinSeeders           int      `envconfig:"SELECT_MIN_SEEDERS" default:"1"`
	SelectMinGrabs             int      `envconfig:"SELECT_MIN_GRABS" default:"0"`
	SelectRequireCached        bool     `envconfig:"SELECT_REQUIRE_CACHED" default:"false"`
	SelectBlockedGroups        []string `envconfig:"SELECT_BLOCKED_GROUPS"`
	SelectBlockedKeywords      []string `envconfig:"SELECT_BLOCKED_KEYWORDS"`
	SelectMinScore             int      `envconfig:"SELECT_MIN_SCORE" default:"0"`
	SelectPreferredResolutions []string `envconfig:"SELECT_PREFERRED_RESOLUTIONS" default:"2160p,1080p,720p"`
	SelectPreferredQualities   []string `envconfig:"SELECT_PREFERRED_QUALITIES" default:"WEB-DL,BluRay,WEBRip,HDTV"`
	SelectPreferredGroups      []string `envconfig:"SELECT_PREFERRED_GROUPS"`
	SelectPreferredKeywords    []string `envconfig:"SELECT_PREFERRED_KEYWORDS"`

	SelectWeightResolution              int `envconfig:"SELECT_WEIGHT_RESOLUTION" default:"400"`
	SelectWeightQuality                 int `envconfig:"SELECT_WEIGHT_QUALITY" default:"200"`
	SelectWeightProtocolCachedTorrent   int `envconfig:"SELECT_WEIGHT_PROTOCOL_CACHED_TORRENT" default:"300"`
	SelectWeightProtocolUsenet          int `envconfig:"SELECT_WEIGHT_PROTOCOL_USENET" default:"200"`
	SelectWeightProtocolUncachedTorrent int `envconfig:"SELECT_WEIGHT_PROTOCOL_UNCACHED_TORRENT" default:"100"`
	SelectWeightHealth                  int `envconfig:"SELECT_WEIGHT_HEALTH" default:"100"`
	SelectSeedSaturation                int `envconfig:"SELECT_SEED_SATURATION" default:"100"`
	SelectWeightPreferredGroup          int `envconfig:"SELECT_WEIGHT_PREFERRED_GROUP" default:"150"`
	SelectWeightPreferredKeyword        int `envconfig:"SELECT_WEIGHT_PREFERRED_KEYWORD" default:"50"`
	SelectWeightFreeleech               int `envconfig:"SELECT_WEIGHT_FREELEECH" default:"40"`
	SelectWeightProper                  int `envconfig:"SELECT_WEIGHT_PROPER" default:"25"`

	// ── Limit knobs (NEW — FR-LIM-2/3) ──
	MaxActiveDownloads int `envconfig:"MAX_ACTIVE_DOWNLOADS" default:"0"`
	MaxCreatePerHour   int `envconfig:"MAX_CREATE_PER_HOUR" default:"60"`
	MaxTorrentPerMin   int `envconfig:"MAX_TORRENT_PER_MIN" default:"300"`
	SearchConcurrency  int `envconfig:"SEARCH_CONCURRENCY" default:"3"`
}

// Load reads configuration from the environment and validates it.
func Load() (*Config, error) {
	var c Config
	if err := envconfig.Process("boxarr", &c); err != nil {
		return nil, fmt.Errorf("processing env config: %w", err)
	}
	if err := c.validateMount(); err != nil {
		return nil, err
	}
	symInfo, err := os.Stat(c.SymlinkRoot)
	if err != nil {
		return nil, fmt.Errorf("symlink root %q: %w", c.SymlinkRoot, err)
	}
	if !symInfo.IsDir() {
		return nil, fmt.Errorf("symlink root %q is not a directory", c.SymlinkRoot)
	}
	for _, lr := range []struct{ name, path string }{
		{"movie library root", c.MovieLibraryRoot},
		{"tv library root", c.TVLibraryRoot},
	} {
		info, err := os.Stat(lr.path)
		if err != nil {
			return nil, fmt.Errorf("%s %q: %w", lr.name, lr.path, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s %q is not a directory", lr.name, lr.path)
		}
	}
	if c.HealEnabled {
		if len(c.HealLibraryRoots) == 0 {
			return nil, fmt.Errorf("HEAL_ENABLED requires HEAL_LIBRARY_ROOTS")
		}
		for _, root := range c.HealLibraryRoots {
			info, err := os.Stat(root)
			if err != nil {
				return nil, fmt.Errorf("heal library root %q: %w", root, err)
			}
			if !info.IsDir() {
				return nil, fmt.Errorf("heal library root %q is not a directory", root)
			}
		}
		if c.HealMaxAttempts <= 0 {
			return nil, fmt.Errorf("HEAL_MAX_ATTEMPTS must be greater than 0")
		}
	}
	return &c, nil
}

// validateMount ensures the WebDAV mount root exists and is a directory.
func (c *Config) validateMount() error {
	info, err := os.Stat(c.WebDAVMountRoot)
	if err != nil {
		return fmt.Errorf("webdav mount root %q: %w", c.WebDAVMountRoot, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("webdav mount root %q is not a directory", c.WebDAVMountRoot)
	}
	return nil
}

// UsenetPath returns the host filesystem path under which TorBox creates one
// folder per completed Usenet download. With an empty WebDAVUsenetSubpath
// (the default) this is the mount root itself, which matches TorBox's WebDAV
// layout — TorBox does not nest releases under a "usenet" folder.
func (c *Config) UsenetPath() string {
	return filepath.Join(c.WebDAVMountRoot, c.WebDAVUsenetSubpath)
}

// SlogLevel maps the configured log level string to a slog.Level.
func (c *Config) SlogLevel() slog.Level {
	switch c.LogLevel {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// WebDAVRefreshEnabled reports whether the TorBox WebDAV /refresh integration
// is configured. It activates only when both WebDAV credentials are present.
func (c *Config) WebDAVRefreshEnabled() bool {
	return c.TorBoxWebDAVUser != "" && c.TorBoxWebDAVPass != ""
}

// TorrentPath returns the host filesystem path under which TorBox surfaces
// completed torrent downloads. With an empty WebDAVTorrentSubpath (the default)
// this collapses to the mount root, the same flat namespace as usenet (00 §19.5).
func (c *Config) TorrentPath() string {
	return filepath.Join(c.WebDAVMountRoot, c.WebDAVTorrentSubpath)
}

// PlexEnabled reports whether the optional Plex integration is configured.
func (c *Config) PlexEnabled() bool { return c.PlexURL != "" && c.PlexToken != "" }

// TVDBEnabled reports whether TVDB (scene/absolute ordering) is configured.
func (c *Config) TVDBEnabled() bool { return c.TVDBAPIKey != "" }

// SeerrEnabled reports whether the inbound Sonarr/Radarr v3 emulation accepts requests.
func (c *Config) SeerrEnabled() bool { return len(c.SeerrAPIKeys) > 0 }

// AllowsCategory reports whether cat is in the configured category allowlist.
func (c *Config) AllowsCategory(cat string) bool {
	for _, allowed := range c.Categories {
		if allowed == cat {
			return true
		}
	}
	return false
}
