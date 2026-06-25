package settings

import (
	"context"
	"strings"

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
	// Normalize language codes to upper-case: DetectLanguages emits upper-case
	// display codes (DE/EN/MULTI) and the score compares them case-sensitively, so a
	// lower-case override (e.g. "de") would otherwise silently break scoring.
	cfg.RequiredLanguages = upperAll(cfg.RequiredLanguages)
	cfg.PreferredLanguages = upperAll(cfg.PreferredLanguages)

	// Learned group tendencies: groups that RELIABLY ship a preferred language — at
	// least LikelyGroupMinRatio of their verified downloads, over at least
	// LikelyGroupMinSample releases — get a likelihood bonus in scoring, raising the
	// chance of getting the right language on the first try. A high bar (90%) avoids
	// boosting groups that only occasionally happen to include the language. We union
	// the qualifying groups across ALL preferred languages (not just the top), so the
	// bonus matches the per-language "trusted" badge in the Languages UI.
	groups := map[string]bool{}
	for _, lang := range cfg.PreferredLanguages {
		stats, err := s.db.GroupLanguageStats(context.Background(), lang)
		if err != nil {
			continue
		}
		for _, st := range stats {
			if st.Total >= LikelyGroupMinSample && st.Ratio >= LikelyGroupMinRatio {
				groups[st.Group] = true
			}
		}
	}
	if len(groups) > 0 {
		cfg.LikelyLanguageGroups = groups
	}
	return cfg
}

// Thresholds for treating a release group as a reliable source of a language
// (learned from the release-language knowledge base). Shared with the Languages
// UI so its "trusted" badge matches the groups scoring rewards.
const (
	LikelyGroupMinRatio  = 0.90 // ≥90% of the group's verified releases carry the language
	LikelyGroupMinSample = 3    // ...over at least this many verified releases
)

func upperAll(langs []string) []string {
	out := make([]string, 0, len(langs))
	for _, l := range langs {
		if v := strings.ToUpper(strings.TrimSpace(l)); v != "" {
			out = append(out, v)
		}
	}
	return out
}
