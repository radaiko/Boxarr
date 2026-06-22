package settings

import (
	"context"

	"github.com/radaiko/boxarr/internal/selection"
)

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

	// Per-type language rules.
	KeySelectMovieLangRequired   = "select.movie_lang_required"
	KeySelectMovieLangPreferred  = "select.movie_lang_preferred"
	KeySelectSeriesLangRequired  = "select.series_lang_required"
	KeySelectSeriesLangPreferred = "select.series_lang_preferred"
	KeySelectAnimeLangRequired   = "select.anime_lang_required"
	KeySelectAnimeLangPreferred  = "select.anime_lang_preferred"
	KeySelectAnimeRequireAny     = "select.anime_require_any"
	KeySelectAnimePreferSubs     = "select.anime_prefer_subs"
	KeySelectWeightLanguage      = "select.weight_language"
	KeySelectWeightSubs          = "select.weight_subs"
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
	KeySelectMovieLangRequired, KeySelectMovieLangPreferred,
	KeySelectSeriesLangRequired, KeySelectSeriesLangPreferred,
	KeySelectAnimeLangRequired, KeySelectAnimeLangPreferred,
	KeySelectAnimeRequireAny, KeySelectAnimePreferSubs,
	KeySelectWeightLanguage, KeySelectWeightSubs,
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

// SelectionConfigFor returns the selection score for a content type ("movie",
// "series", "anime") — the base config plus that type's language rules. Other
// kinds (e.g. "" for free search) get no language gate.
func (s *Store) SelectionConfigFor(kind string) selection.Config {
	cfg := s.SelectionConfig()
	cfg.WeightLanguage = s.intv(KeySelectWeightLanguage, s.seed.SelectWeightLanguage)
	cfg.WeightSubs = s.intv(KeySelectWeightSubs, s.seed.SelectWeightSubs)
	switch kind {
	case "movie":
		cfg.RequiredLanguages = s.csv(KeySelectMovieLangRequired, s.seed.SelectMovieLangRequired)
		cfg.PreferredLanguages = s.csv(KeySelectMovieLangPreferred, s.seed.SelectMovieLangPreferred)
	case "series":
		cfg.RequiredLanguages = s.csv(KeySelectSeriesLangRequired, s.seed.SelectSeriesLangRequired)
		cfg.PreferredLanguages = s.csv(KeySelectSeriesLangPreferred, s.seed.SelectSeriesLangPreferred)
	case "anime":
		cfg.RequiredLanguages = s.csv(KeySelectAnimeLangRequired, s.seed.SelectAnimeLangRequired)
		cfg.PreferredLanguages = s.csv(KeySelectAnimeLangPreferred, s.seed.SelectAnimeLangPreferred)
		cfg.RequireAnyLanguage = s.boolv(KeySelectAnimeRequireAny, s.seed.SelectAnimeRequireAny)
		cfg.PreferEnglishSubs = s.boolv(KeySelectAnimePreferSubs, s.seed.SelectAnimePreferSubs)
	}
	// Learned group tendencies: groups verified to ship the top preferred language
	// (from the release-language knowledge base) get a likelihood bonus in scoring.
	if top := topLang(cfg.PreferredLanguages); top != "" {
		if groups, err := s.db.GroupsProvidingLanguage(context.Background(), top); err == nil && len(groups) > 0 {
			cfg.LikelyLanguageGroups = groups
		}
	}
	return cfg
}

func topLang(langs []string) string {
	if len(langs) == 0 {
		return ""
	}
	return langs[0]
}
