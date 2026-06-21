package settings

import "github.com/radaiko/boxarr/internal/selection"

// Selection-score setting keys (FR-SR-5). These make every selection knob
// DB-overridable from the UI; SelectionConfig overlays them on the env/default
// seed and reuses selection.FromConfig as the single mapping source of truth.
const (
	KeySelectAllowedResolutions   = "select.allowed_resolutions"
	KeySelectPreferredResolutions = "select.preferred_resolutions"
	KeySelectPreferredQualities   = "select.preferred_qualities"
	KeySelectPreferredGroups      = "select.preferred_groups"
	KeySelectPreferredKeywords    = "select.preferred_keywords"
	KeySelectBlockedGroups        = "select.blocked_groups"
	KeySelectBlockedKeywords      = "select.blocked_keywords"
	KeySelectMinSize              = "select.min_size"
	KeySelectMaxSize              = "select.max_size"
	KeySelectSizeLimits           = "select.size_limits"
	KeySelectMinSeeders           = "select.min_seeders"
	KeySelectMinGrabs             = "select.min_grabs"
	KeySelectRequireCached        = "select.require_cached"
	KeySelectMinScore             = "select.min_score"

	KeySelectWeightResolution              = "select.weight_resolution"
	KeySelectWeightQuality                 = "select.weight_quality"
	KeySelectWeightProtocolCachedTorrent   = "select.weight_protocol_cached_torrent"
	KeySelectWeightProtocolUsenet          = "select.weight_protocol_usenet"
	KeySelectWeightProtocolUncachedTorrent = "select.weight_protocol_uncached_torrent"
	KeySelectWeightHealth                  = "select.weight_health"
	KeySelectSeedSaturation                = "select.seed_saturation"
	KeySelectWeightPreferredGroup          = "select.weight_preferred_group"
	KeySelectWeightPreferredKeyword        = "select.weight_preferred_keyword"
	KeySelectWeightFreeleech               = "select.weight_freeleech"
	KeySelectWeightProper                  = "select.weight_proper"
)

// selectionKeys is every selection key, used to register them as writable and to
// surface them in EffectiveNonSecret.
var selectionKeys = []string{
	KeySelectAllowedResolutions, KeySelectPreferredResolutions, KeySelectPreferredQualities,
	KeySelectPreferredGroups, KeySelectPreferredKeywords, KeySelectBlockedGroups, KeySelectBlockedKeywords,
	KeySelectMinSize, KeySelectMaxSize, KeySelectSizeLimits, KeySelectMinSeeders, KeySelectMinGrabs,
	KeySelectRequireCached, KeySelectMinScore,
	KeySelectWeightResolution, KeySelectWeightQuality, KeySelectWeightProtocolCachedTorrent,
	KeySelectWeightProtocolUsenet, KeySelectWeightProtocolUncachedTorrent, KeySelectWeightHealth,
	KeySelectSeedSaturation, KeySelectWeightPreferredGroup, KeySelectWeightPreferredKeyword,
	KeySelectWeightFreeleech, KeySelectWeightProper,
}

// SelectionConfig builds the live selection score from current settings: it
// shallow-copies the env/default seed, overlays any DB-set selection values, and
// runs it through selection.FromConfig (the single field→config mapping).
func (s *Store) SelectionConfig() selection.Config {
	c := *s.seed // copy: reassigning scalar/slice/string fields below never mutates the seed
	c.SelectAllowedResolutions = s.csv(KeySelectAllowedResolutions, s.seed.SelectAllowedResolutions)
	c.SelectPreferredResolutions = s.csv(KeySelectPreferredResolutions, s.seed.SelectPreferredResolutions)
	c.SelectPreferredQualities = s.csv(KeySelectPreferredQualities, s.seed.SelectPreferredQualities)
	c.SelectPreferredGroups = s.csv(KeySelectPreferredGroups, s.seed.SelectPreferredGroups)
	c.SelectPreferredKeywords = s.csv(KeySelectPreferredKeywords, s.seed.SelectPreferredKeywords)
	c.SelectBlockedGroups = s.csv(KeySelectBlockedGroups, s.seed.SelectBlockedGroups)
	c.SelectBlockedKeywords = s.csv(KeySelectBlockedKeywords, s.seed.SelectBlockedKeywords)
	c.SelectMinSize = s.i64(KeySelectMinSize, s.seed.SelectMinSize)
	c.SelectMaxSize = s.i64(KeySelectMaxSize, s.seed.SelectMaxSize)
	c.SelectSizeLimits = s.str(KeySelectSizeLimits, s.seed.SelectSizeLimits)
	c.SelectMinSeeders = s.intv(KeySelectMinSeeders, s.seed.SelectMinSeeders)
	c.SelectMinGrabs = s.intv(KeySelectMinGrabs, s.seed.SelectMinGrabs)
	c.SelectRequireCached = s.boolv(KeySelectRequireCached, s.seed.SelectRequireCached)
	c.SelectMinScore = s.intv(KeySelectMinScore, s.seed.SelectMinScore)
	c.SelectWeightResolution = s.intv(KeySelectWeightResolution, s.seed.SelectWeightResolution)
	c.SelectWeightQuality = s.intv(KeySelectWeightQuality, s.seed.SelectWeightQuality)
	c.SelectWeightProtocolCachedTorrent = s.intv(KeySelectWeightProtocolCachedTorrent, s.seed.SelectWeightProtocolCachedTorrent)
	c.SelectWeightProtocolUsenet = s.intv(KeySelectWeightProtocolUsenet, s.seed.SelectWeightProtocolUsenet)
	c.SelectWeightProtocolUncachedTorrent = s.intv(KeySelectWeightProtocolUncachedTorrent, s.seed.SelectWeightProtocolUncachedTorrent)
	c.SelectWeightHealth = s.intv(KeySelectWeightHealth, s.seed.SelectWeightHealth)
	c.SelectSeedSaturation = s.intv(KeySelectSeedSaturation, s.seed.SelectSeedSaturation)
	c.SelectWeightPreferredGroup = s.intv(KeySelectWeightPreferredGroup, s.seed.SelectWeightPreferredGroup)
	c.SelectWeightPreferredKeyword = s.intv(KeySelectWeightPreferredKeyword, s.seed.SelectWeightPreferredKeyword)
	c.SelectWeightFreeleech = s.intv(KeySelectWeightFreeleech, s.seed.SelectWeightFreeleech)
	c.SelectWeightProper = s.intv(KeySelectWeightProper, s.seed.SelectWeightProper)
	return selection.FromConfig(&c)
}
