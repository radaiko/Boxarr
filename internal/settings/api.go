package settings

import (
	"strconv"
	"strings"
)

// secretKeys are never returned in cleartext by the settings API.
var secretKeys = map[string]bool{
	KeyTorBoxToken:    true,
	KeyWebDAVPass:     true,
	KeyProwlarrAPIKey: true,
	KeyTMDBToken:      true,
	KeyTVDBAPIKey:     true,
	KeyTVDBPin:        true,
	KeyPlexToken:      true,
	KeySeerrAPIKeys:   true,
	KeyAPIKey:         true,
}

// writableKeys is the allow-list of keys the settings API may write.
var writableKeys = map[string]bool{
	KeyTorBoxToken: true, KeyWebDAVUser: true, KeyWebDAVPass: true,
	KeyWebDAVRefreshURL: true, KeyWebDAVRefreshCooldown: true,
	KeyProwlarrURL: true, KeyProwlarrAPIKey: true, KeyTMDBToken: true,
	KeyTVDBAPIKey: true, KeyTVDBPin: true, KeyPlexURL: true, KeyPlexToken: true,
	KeyPlexMovieSection: true, KeyPlexTVSection: true, KeySeerrAPIKeys: true,
	KeyAPIKey: true, KeyMovieLibraryRoot: true, KeyTVLibraryRoot: true,
	KeySymlinkRoot: true, KeyWebDAVMountRoot: true, KeyWebDAVUsenetSubpath: true,
	KeyWebDAVTorrentSubpath: true, KeyReconcileInterval: true, KeyMetadataInterval: true,
	KeySearchInterval: true, KeyPollInterval: true, KeyAutomationEnabled: true,
	KeyMaxActiveDownloads: true, KeyMaxCreatePerHour: true, KeyMaxTorrentPerMin: true,
	KeyTorBoxBaseURL: true, KeyTMDBBaseURL: true, KeyTVDBBaseURL: true,
}

func init() {
	// Selection-score keys are all writable (and non-secret).
	for _, k := range selectionKeys {
		writableKeys[k] = true
	}
}

// Writable reports whether key may be set via the settings API.
func Writable(key string) bool { return writableKeys[key] }

// Redacted returns the DB-overlay values with secret keys masked (so the UI can
// show "set" vs "unset" without leaking credentials).
func (s *Store) Redacted() map[string]string {
	out := map[string]string{}
	for k, v := range s.All() {
		if secretKeys[k] && v != "" {
			out[k] = "********"
		} else {
			out[k] = v
		}
	}
	return out
}

// EffectiveNonSecret returns the effective (DB→env→default) values for every
// non-secret key, so the UI can render current settings including those still
// coming from env/defaults.
func (s *Store) EffectiveNonSecret() map[string]string {
	eff := map[string]string{
		KeyProwlarrURL:          s.ProwlarrURL(),
		KeyPlexURL:              s.PlexURL(),
		KeyPlexMovieSection:     s.PlexMovieSection(),
		KeyPlexTVSection:        s.PlexTVSection(),
		KeyMovieLibraryRoot:     s.MovieLibraryRoot(),
		KeyTVLibraryRoot:        s.TVLibraryRoot(),
		KeySymlinkRoot:          s.SymlinkRoot(),
		KeyWebDAVMountRoot:      s.WebDAVMountRoot(),
		KeyWebDAVUsenetSubpath:  s.WebDAVUsenetSubpath(),
		KeyWebDAVTorrentSubpath: s.WebDAVTorrentSubpath(),
		KeyReconcileInterval:    s.ReconcileInterval().String(),
		KeyMetadataInterval:     s.MetadataInterval().String(),
		KeySearchInterval:       s.SearchInterval().String(),
		KeyPollInterval:         s.PollInterval().String(),
		KeyWebDAVRefreshURL:     s.TorBoxWebDAVRefreshURL(),
		KeyWebDAVUser:           s.TorBoxWebDAVUser(),
		KeyAutomationEnabled:    boolStr(s.AutomationEnabled()),
		KeyMaxActiveDownloads:   itoa(s.MaxActiveDownloads()),
		KeyMaxCreatePerHour:     itoa(s.MaxCreatePerHour()),
		KeyMaxTorrentPerMin:     itoa(s.MaxTorrentPerMin()),
		KeySeerrAPIKeys:         strings.Join(s.SeerrAPIKeys(), ","),
	}
	c := s.SelectionConfig()
	for k, v := range map[string]string{
		KeySelectAllowedResolutions:            strings.Join(c.AllowedResolutions, ","),
		KeySelectPreferredResolutions:          strings.Join(c.PreferredResolutions, ","),
		KeySelectPreferredQualities:            strings.Join(c.PreferredQualities, ","),
		KeySelectPreferredGroups:               strings.Join(c.PreferredGroups, ","),
		KeySelectPreferredKeywords:             strings.Join(c.PreferredKeywords, ","),
		KeySelectBlockedGroups:                 strings.Join(c.BlockedGroups, ","),
		KeySelectBlockedKeywords:               strings.Join(c.BlockedKeywords, ","),
		KeySelectMinSize:                       strconv.FormatInt(c.MinSize, 10),
		KeySelectMaxSize:                       strconv.FormatInt(c.MaxSize, 10),
		KeySelectSizeLimits:                    s.str(KeySelectSizeLimits, s.seed.SelectSizeLimits),
		KeySelectMinSeeders:                    itoa(c.MinSeeders),
		KeySelectMinGrabs:                      itoa(c.MinGrabs),
		KeySelectRequireCached:                 boolStr(c.RequireCached),
		KeySelectMinScore:                      itoa(c.MinScore),
		KeySelectWeightResolution:              itoa(c.WeightResolution),
		KeySelectWeightQuality:                 itoa(c.WeightQuality),
		KeySelectWeightProtocolCachedTorrent:   itoa(c.WeightProtocolCachedTorrent),
		KeySelectWeightProtocolUsenet:          itoa(c.WeightProtocolUsenet),
		KeySelectWeightProtocolUncachedTorrent: itoa(c.WeightProtocolUncachedTorrent),
		KeySelectWeightHealth:                  itoa(c.WeightHealth),
		KeySelectSeedSaturation:                itoa(c.SeedSaturation),
		KeySelectWeightPreferredGroup:          itoa(c.WeightPreferredGroup),
		KeySelectWeightPreferredKeyword:        itoa(c.WeightPreferredKeyword),
		KeySelectWeightFreeleech:               itoa(c.WeightFreeleech),
		KeySelectWeightProper:                  itoa(c.WeightProper),
	} {
		eff[k] = v
	}
	return eff
}

// Configured reports, per integration, whether the credential needed to use it
// is present (effective). Lets the UI show connection status without secrets.
func (s *Store) Configured() map[string]bool {
	return map[string]bool{
		"torbox":   s.TorBoxToken() != "",
		"prowlarr": s.ProwlarrURL() != "" && s.ProwlarrAPIKey() != "",
		"tmdb":     s.TMDBToken() != "",
		"tvdb":     s.TVDBEnabled(),
		"plex":     s.PlexEnabled(),
		"seerr":    s.SeerrEnabled(),
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func itoa(n int) string { return strconv.Itoa(n) }
