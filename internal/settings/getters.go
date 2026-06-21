package settings

import (
	"log/slog"
	"path/filepath"
	"time"

	"github.com/radaiko/boxarr/internal/metadata/tmdb"
	"github.com/radaiko/boxarr/internal/metadata/tvdb"
	"github.com/radaiko/boxarr/internal/plex"
	"github.com/radaiko/boxarr/internal/prowlarr"
	"github.com/radaiko/boxarr/internal/torbox"
)

// ── Connections ──
func (s *Store) TorBoxToken() string      { return s.str(KeyTorBoxToken, s.seed.TorBoxAPIToken) }
func (s *Store) ProwlarrURL() string      { return s.str(KeyProwlarrURL, s.seed.ProwlarrURL) }
func (s *Store) ProwlarrAPIKey() string   { return s.str(KeyProwlarrAPIKey, s.seed.ProwlarrAPIKey) }
func (s *Store) TMDBToken() string        { return s.str(KeyTMDBToken, s.seed.TMDBAPIKey) }
func (s *Store) TVDBAPIKey() string       { return s.str(KeyTVDBAPIKey, s.seed.TVDBAPIKey) }
func (s *Store) TVDBPin() string          { return s.str(KeyTVDBPin, s.seed.TVDBPin) }
func (s *Store) PlexURL() string          { return s.str(KeyPlexURL, s.seed.PlexURL) }
func (s *Store) PlexToken() string        { return s.str(KeyPlexToken, s.seed.PlexToken) }
func (s *Store) PlexMovieSection() string { return s.str(KeyPlexMovieSection, s.seed.PlexMovieSection) }
func (s *Store) PlexTVSection() string    { return s.str(KeyPlexTVSection, s.seed.PlexTVSection) }
func (s *Store) SeerrAPIKeys() []string   { return s.csv(KeySeerrAPIKeys, s.seed.SeerrAPIKeys) }
func (s *Store) APIKey() string           { return s.str(KeyAPIKey, s.seed.APIKey) }

// Base-URL overrides (empty = the client's production default). Used by the
// connection-test endpoints and mirror/self-host setups.
func (s *Store) TorBoxBaseURL() string { return s.str(KeyTorBoxBaseURL, "") }
func (s *Store) TMDBBaseURL() string   { return s.str(KeyTMDBBaseURL, "") }
func (s *Store) TVDBBaseURL() string   { return s.str(KeyTVDBBaseURL, "") }

// ── TorBox WebDAV force-refresh ──
func (s *Store) TorBoxWebDAVUser() string { return s.str(KeyWebDAVUser, s.seed.TorBoxWebDAVUser) }
func (s *Store) TorBoxWebDAVPass() string { return s.str(KeyWebDAVPass, s.seed.TorBoxWebDAVPass) }
func (s *Store) TorBoxWebDAVRefreshURL() string {
	return s.str(KeyWebDAVRefreshURL, s.seed.TorBoxWebDAVRefreshURL)
}
func (s *Store) WebDAVRefreshCooldown() time.Duration {
	return s.dur(KeyWebDAVRefreshCooldown, s.seed.WebDAVRefreshCooldown)
}
func (s *Store) WebDAVRefreshEnabled() bool {
	return s.TorBoxWebDAVUser() != "" && s.TorBoxWebDAVPass() != ""
}

// ── Paths ──
func (s *Store) WebDAVMountRoot() string { return s.str(KeyWebDAVMountRoot, s.seed.WebDAVMountRoot) }
func (s *Store) WebDAVUsenetSubpath() string {
	return s.str(KeyWebDAVUsenetSubpath, s.seed.WebDAVUsenetSubpath)
}
func (s *Store) WebDAVTorrentSubpath() string {
	return s.str(KeyWebDAVTorrentSubpath, s.seed.WebDAVTorrentSubpath)
}
func (s *Store) UsenetPath() string {
	return filepath.Join(s.WebDAVMountRoot(), s.WebDAVUsenetSubpath())
}
func (s *Store) TorrentPath() string {
	return filepath.Join(s.WebDAVMountRoot(), s.WebDAVTorrentSubpath())
}
func (s *Store) SymlinkRoot() string      { return s.str(KeySymlinkRoot, s.seed.SymlinkRoot) }
func (s *Store) MovieLibraryRoot() string { return s.str(KeyMovieLibraryRoot, s.seed.MovieLibraryRoot) }
func (s *Store) TVLibraryRoot() string    { return s.str(KeyTVLibraryRoot, s.seed.TVLibraryRoot) }

// ── Intervals ──
func (s *Store) PollInterval() time.Duration { return s.dur(KeyPollInterval, s.seed.PollInterval) }
func (s *Store) ReconcileInterval() time.Duration {
	return s.dur(KeyReconcileInterval, s.seed.ReconcileInterval)
}
func (s *Store) MetadataInterval() time.Duration {
	return s.dur(KeyMetadataInterval, s.seed.MetadataInterval)
}
func (s *Store) SearchInterval() time.Duration {
	return s.dur(KeySearchInterval, s.seed.SearchInterval)
}

// ── Limits ──
func (s *Store) MaxActiveDownloads() int {
	return s.intv(KeyMaxActiveDownloads, s.seed.MaxActiveDownloads)
}
func (s *Store) MaxCreatePerHour() int { return s.intv(KeyMaxCreatePerHour, s.seed.MaxCreatePerHour) }
func (s *Store) MaxTorrentPerMin() int { return s.intv(KeyMaxTorrentPerMin, s.seed.MaxTorrentPerMin) }

// ── Heal (kept on the seed; heal is env-gated infra, exposed read-only) ──
func (s *Store) HealEnabled() bool                 { return s.seed.HealEnabled }
func (s *Store) HealInterval() time.Duration       { return s.seed.HealInterval }
func (s *Store) HealLibraryRoots() []string        { return s.seed.HealLibraryRoots }
func (s *Store) HealDryRun() bool                  { return s.seed.HealDryRun }
func (s *Store) HealMaxAttempts() int              { return s.seed.HealMaxAttempts }
func (s *Store) HealBackoffInitial() time.Duration { return s.seed.HealBackoffInitial }
func (s *Store) HealWebhookURL() string            { return s.seed.HealWebhookURL }
func (s *Store) HealWebhookEvents() []string       { return s.seed.HealWebhookEvents }
func (s *Store) HealProwlarrFallback() bool {
	return s.boolv("heal.prowlarr_fallback", s.seed.HealProwlarrFallback)
}

// ── Automation ──
func (s *Store) AutomationEnabled() bool {
	return s.boolv(KeyAutomationEnabled, s.seed.AutomationEnabled)
}

// ── Categories (transitional) ──
func (s *Store) Categories() []string { return s.seed.Categories }
func (s *Store) AllowsCategory(cat string) bool {
	for _, c := range s.Categories() {
		if c == cat {
			return true
		}
	}
	return false
}

// SlogLevel maps the seed log level (bootstrap-only) to slog.Level.
func (s *Store) SlogLevel() slog.Level { return s.seed.SlogLevel() }

// ── Enabled predicates ──
func (s *Store) PlexEnabled() bool     { return s.PlexURL() != "" && s.PlexToken() != "" }
func (s *Store) TVDBEnabled() bool     { return s.TVDBAPIKey() != "" }
func (s *Store) SeerrEnabled() bool    { return len(s.SeerrAPIKeys()) > 0 }
func (s *Store) TorrentsEnabled() bool { return s.TorBoxToken() != "" }

// SelectionConfig is implemented in selection.go (overlays DB on the seed).

// ── Memoized client factories (rebuilt only when credentials change) ──

// baseURL keys let tests (and mirror/self-host setups) redirect a client off its
// production endpoint. Empty = the client's built-in default.
const (
	KeyTorBoxBaseURL = "torbox.base_url"
	KeyTMDBBaseURL   = "tmdb.base_url"
	KeyTVDBBaseURL   = "tvdb.base_url"
)

func (s *Store) TorBox() *torbox.Client {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	tok := s.TorBoxToken()
	if s.tbCl == nil || s.tbTok != tok {
		if base := s.str(KeyTorBoxBaseURL, ""); base != "" {
			s.tbCl = torbox.NewWithBaseURL(tok, base)
		} else {
			s.tbCl = torbox.New(tok)
		}
		s.tbTok = tok
	}
	return s.tbCl
}

func (s *Store) Prowlarr() *prowlarr.Client {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	url, key := s.ProwlarrURL(), s.ProwlarrAPIKey()
	if s.prCl == nil || s.prURL != url || s.prKey != key {
		s.prCl, s.prURL, s.prKey = prowlarr.New(url, key), url, key
	}
	return s.prCl
}

func (s *Store) TMDB() *tmdb.Client {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	tok := s.TMDBToken()
	if s.tmCl == nil || s.tmTok != tok {
		if base := s.str(KeyTMDBBaseURL, ""); base != "" {
			s.tmCl = tmdb.NewWithBaseURL(tok, base)
		} else {
			s.tmCl = tmdb.New(tok)
		}
		s.tmTok = tok
	}
	return s.tmCl
}

func (s *Store) TVDB() *tvdb.Client {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	key, pin := s.TVDBAPIKey(), s.TVDBPin()
	if s.tvCl == nil || s.tvKey != key || s.tvPin != pin {
		if base := s.str(KeyTVDBBaseURL, ""); base != "" {
			s.tvCl = tvdb.NewWithBaseURL(key, pin, base)
		} else {
			s.tvCl = tvdb.New(key, pin)
		}
		s.tvKey, s.tvPin = key, pin
	}
	return s.tvCl
}

func (s *Store) Plex() *plex.Client {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	url, tok := s.PlexURL(), s.PlexToken()
	if s.plxCl == nil || s.plxURL != url || s.plxTok != tok {
		s.plxCl, s.plxURL, s.plxTok = plex.New(url, tok), url, tok
	}
	return s.plxCl
}

// invalidateClients drops all memoized clients so the next factory call rebuilds
// from current settings (called after Set).
func (s *Store) invalidateClients() {
	s.cmu.Lock()
	defer s.cmu.Unlock()
	s.tbCl, s.prCl, s.tmCl, s.tvCl, s.plxCl = nil, nil, nil, nil, nil
}
