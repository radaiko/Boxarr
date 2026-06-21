// Package settings is Boxarr's single live configuration source. It overlays
// DB-persisted values (set from the UI) on top of an env/default seed, exposes
// typed live getters, and hands out memoized API clients that rebuild only when
// their credentials change. A UI save therefore takes effect on the next read —
// no restart (00 §runtime config; user decision: hot-reload, everything UI-settable).
package settings

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/metadata/tmdb"
	"github.com/radaiko/boxarr/internal/metadata/tvdb"
	"github.com/radaiko/boxarr/internal/plex"
	"github.com/radaiko/boxarr/internal/prowlarr"
	"github.com/radaiko/boxarr/internal/store"
	"github.com/radaiko/boxarr/internal/torbox"
)

// Canonical setting keys (dotted). These are what the UI reads/writes and what
// the settings table stores.
const (
	KeyTorBoxToken           = "torbox.token"
	KeyWebDAVUser            = "torbox.webdav_user"
	KeyWebDAVPass            = "torbox.webdav_pass"
	KeyWebDAVRefreshURL      = "torbox.webdav_refresh_url"
	KeyWebDAVRefreshCooldown = "torbox.webdav_refresh_cooldown"
	KeyProwlarrURL           = "prowlarr.url"
	KeyProwlarrAPIKey        = "prowlarr.api_key"
	KeyTMDBToken             = "tmdb.token"
	KeyTVDBAPIKey            = "tvdb.api_key"
	KeyTVDBPin               = "tvdb.pin"
	KeyPlexURL               = "plex.url"
	KeyPlexToken             = "plex.token"
	KeyPlexMovieSection      = "plex.movie_section"
	KeyPlexTVSection         = "plex.tv_section"
	KeySeerrAPIKeys          = "seerr.api_keys"
	KeyAPIKey                = "api.key"
	KeyMovieLibraryRoot      = "library.movie_root"
	KeyTVLibraryRoot         = "library.tv_root"
	KeySymlinkRoot           = "library.symlink_root"
	KeyWebDAVMountRoot       = "webdav.mount_root"
	KeyWebDAVUsenetSubpath   = "webdav.usenet_subpath"
	KeyWebDAVTorrentSubpath  = "webdav.torrent_subpath"
	KeyReconcileInterval     = "interval.reconcile"
	KeyMetadataInterval      = "interval.metadata"
	KeySearchInterval        = "interval.search"
	KeyPollInterval          = "interval.poll"
	KeyAutomationEnabled     = "automation.enabled"
	KeyMaxActiveDownloads    = "limit.max_active"
	KeyMaxCreatePerHour      = "limit.max_create_per_hour"
	KeyMaxTorrentPerMin      = "limit.max_torrent_per_min"
)

// Store is the live settings source.
type Store struct {
	db   *store.Store
	seed *config.Config // env/default fallback (never nil)

	mu    sync.RWMutex
	cache map[string]string // DB overlay (set from the UI)

	// memoized clients (rebuilt only when their credential fingerprint changes)
	cmu    sync.Mutex
	tbTok  string
	tbCl   *torbox.Client
	prURL  string
	prKey  string
	prCl   *prowlarr.Client
	tmTok  string
	tmCl   *tmdb.Client
	tvKey  string
	tvPin  string
	tvCl   *tvdb.Client
	plxURL string
	plxTok string
	plxCl  *plex.Client
}

// New builds a Store seeded by cfg (env/defaults) with the DB overlay loaded.
func New(ctx context.Context, db *store.Store, seed *config.Config) (*Store, error) {
	s := &Store{db: db, seed: seed, cache: map[string]string{}}
	if err := s.reload(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// reload refreshes the DB overlay cache.
func (s *Store) reload(ctx context.Context) error {
	all, err := s.db.AllSettings(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cache = all
	s.mu.Unlock()
	return nil
}

// Set persists one setting and refreshes the cache + client memo.
func (s *Store) Set(ctx context.Context, key, value string) error {
	if err := s.db.SetSetting(ctx, key, value); err != nil {
		return err
	}
	s.mu.Lock()
	s.cache[key] = value
	s.mu.Unlock()
	s.invalidateClients()
	return nil
}

// All returns the current DB overlay (UI-set values only; not seed defaults).
func (s *Store) All() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.cache))
	for k, v := range s.cache {
		out[k] = v
	}
	return out
}

// lookup returns a DB-overlay value if present and non-empty.
func (s *Store) lookup(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.cache[key]
	if ok && v != "" {
		return v, true
	}
	return "", false
}

func (s *Store) str(key, fallback string) string {
	if v, ok := s.lookup(key); ok {
		return v
	}
	return fallback
}

func (s *Store) dur(key string, fallback time.Duration) time.Duration {
	if v, ok := s.lookup(key); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func (s *Store) intv(key string, fallback int) int {
	if v, ok := s.lookup(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func (s *Store) boolv(key string, fallback bool) bool {
	if v, ok := s.lookup(key); ok {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}
	return fallback
}

func (s *Store) csv(key string, fallback []string) []string {
	if v, ok := s.lookup(key); ok {
		return strings.Split(v, ",")
	}
	return fallback
}
