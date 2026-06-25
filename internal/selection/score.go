// Package selection ranks Prowlarr releases by a deliberately simple,
// configurable weighted score (FR-SR-5) — not Sonarr's custom-format engine.
// Hard rejects are evaluated first; survivors are scored; ties break deterministically.
package selection

import (
	"encoding/json"
	"math"
	"sort"
	"strings"

	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/release"
)

// SizeLimit is a per-quality byte band (0 = unbounded).
type SizeLimit struct {
	Min int64 `json:"min"`
	Max int64 `json:"max"`
}

// Config holds the resolved selection knobs.
type Config struct {
	AllowedResolutions   []string
	PreferredResolutions []string
	PreferredQualities   []string
	MinSize, MaxSize     int64
	SizeLimits           map[string]SizeLimit
	MinSeeders, MinGrabs int
	RequireCached        bool
	PreferredGroups      []string
	BlockedGroups        []string
	PreferredKeywords    []string
	BlockedKeywords      []string
	MinScore             int

	WeightResolution              int
	WeightQuality                 int
	WeightProtocolCachedTorrent   int
	WeightProtocolUsenet          int
	WeightProtocolUncachedTorrent int
	WeightHealth                  int
	SeedSaturation                int
	WeightPreferredGroup          int
	WeightPreferredKeyword        int
	WeightFreeleech               int
	WeightProper                  int

	// Language rules (resolved per content type by settings.SelectionConfigFor).
	// RequiredLanguages gates releases by detected audio language; RequireAny means
	// any-of (anime: DE or EN) rather than all-of (movies/series: DE). MULTi acts as
	// a wildcard. PreferredLanguages and English subs only boost the score.
	RequiredLanguages  []string
	RequireAnyLanguage bool
	PreferredLanguages []string
	WeightLanguage     int
	PreferEnglishSubs  bool

	// LikelyLanguageGroups (lower-cased release groups) are groups empirically
	// verified to ship the top preferred language — learned from the release-
	// language knowledge base. A candidate from such a group gets the same
	// "likely contains the language" bonus as a MULTi release.
	LikelyLanguageGroups map[string]bool
	WeightSubs           int
}

// Release is the scoring view of a candidate (Prowlarr fields + parsed quality).
type Release struct {
	Title        string
	Protocol     string // "usenet" | "torrent"
	Size         int64
	Seeders      int
	Grabs        int
	Resolution   string
	Quality      string
	Group        string
	Proper       bool
	Repack       bool
	IndexerFlags []string
	Cached       bool // torrent cached on TorBox
}

// FromConfig builds a selection Config from the app config (env knobs).
func FromConfig(c *config.Config) Config {
	limits := map[string]SizeLimit{}
	if c.SelectSizeLimits != "" && c.SelectSizeLimits != "{}" {
		_ = json.Unmarshal([]byte(c.SelectSizeLimits), &limits)
	}
	return Config{
		AllowedResolutions:            c.SelectAllowedResolutions,
		PreferredResolutions:          c.SelectPreferredResolutions,
		PreferredQualities:            c.SelectPreferredQualities,
		MinSize:                       c.SelectMinSize,
		MaxSize:                       c.SelectMaxSize,
		SizeLimits:                    limits,
		MinSeeders:                    c.SelectMinSeeders,
		MinGrabs:                      c.SelectMinGrabs,
		RequireCached:                 c.SelectRequireCached,
		PreferredGroups:               c.SelectPreferredGroups,
		BlockedGroups:                 c.SelectBlockedGroups,
		PreferredKeywords:             c.SelectPreferredKeywords,
		BlockedKeywords:               c.SelectBlockedKeywords,
		MinScore:                      c.SelectMinScore,
		WeightResolution:              c.SelectWeightResolution,
		WeightQuality:                 c.SelectWeightQuality,
		WeightProtocolCachedTorrent:   c.SelectWeightProtocolCachedTorrent,
		WeightProtocolUsenet:          c.SelectWeightProtocolUsenet,
		WeightProtocolUncachedTorrent: c.SelectWeightProtocolUncachedTorrent,
		WeightHealth:                  c.SelectWeightHealth,
		SeedSaturation:                c.SelectSeedSaturation,
		WeightPreferredGroup:          c.SelectWeightPreferredGroup,
		WeightPreferredKeyword:        c.SelectWeightPreferredKeyword,
		WeightFreeleech:               c.SelectWeightFreeleech,
		WeightProper:                  c.SelectWeightProper,
	}
}

// Rejected reports whether r fails a hard filter (so it is never auto-grabbed).
func (cfg Config) Rejected(r Release) bool {
	if len(cfg.AllowedResolutions) > 0 && !contains(cfg.AllowedResolutions, r.Resolution) {
		return true
	}
	mn, mx := cfg.MinSize, cfg.MaxSize
	if lim, ok := cfg.SizeLimits[r.Quality]; ok {
		if lim.Min > 0 {
			mn = lim.Min
		}
		if lim.Max > 0 {
			mx = lim.Max
		}
	}
	if mn > 0 && r.Size > 0 && r.Size < mn {
		return true
	}
	if mx > 0 && r.Size > mx {
		return true
	}
	if r.Protocol == "torrent" && r.Seeders < cfg.MinSeeders {
		return true
	}
	if r.Protocol == "usenet" && r.Grabs < cfg.MinGrabs {
		return true
	}
	if contains(cfg.BlockedGroups, r.Group) {
		return true
	}
	if containsKeyword(cfg.BlockedKeywords, r.Title) {
		return true
	}
	if r.Protocol == "torrent" && cfg.RequireCached && !r.Cached {
		return true
	}
	if len(cfg.RequiredLanguages) > 0 {
		langs := release.DetectLanguages(r.Title)
		if cfg.RequireAnyLanguage {
			// Any-of (anime DE or EN): only reject when languages ARE tagged but
			// none match — untagged ≈ original/English, common for anime.
			if len(langs) > 0 && !anyLangSatisfied(cfg.RequiredLanguages, langs) {
				return true
			}
		} else if !allLangsSatisfied(cfg.RequiredLanguages, langs) {
			// All-of (movies/series require German): missing/untagged → reject.
			return true
		}
	}
	return false
}

// langSatisfied treats MULTi as a wildcard (a multi-language release is assumed to
// include the wanted track) so good MULTi releases aren't rejected.
func langSatisfied(have []string, want string) bool {
	return contains(have, want) || contains(have, "MULTI")
}

func anyLangSatisfied(want, have []string) bool {
	for _, w := range want {
		if langSatisfied(have, w) {
			return true
		}
	}
	return false
}

func allLangsSatisfied(want, have []string) bool {
	for _, w := range want {
		if !langSatisfied(have, w) {
			return false
		}
	}
	return true
}

// Score returns the weighted score for r, or math.MinInt if rejected.
func (cfg Config) Score(r Release) int {
	if cfg.Rejected(r) {
		return math.MinInt
	}
	s := 0
	if i := indexOf(cfg.PreferredResolutions, r.Resolution); i >= 0 {
		s += cfg.WeightResolution * (len(cfg.PreferredResolutions) - i)
	}
	if i := indexOf(cfg.PreferredQualities, r.Quality); i >= 0 {
		s += cfg.WeightQuality * (len(cfg.PreferredQualities) - i)
	}
	switch {
	case r.Protocol == "torrent" && r.Cached:
		s += cfg.WeightProtocolCachedTorrent
	case r.Protocol == "usenet":
		s += cfg.WeightProtocolUsenet
	case r.Protocol == "torrent":
		s += cfg.WeightProtocolUncachedTorrent
	}
	health := r.Seeders
	if r.Protocol == "usenet" {
		health = r.Grabs
	}
	if cfg.SeedSaturation > 0 {
		if health > cfg.SeedSaturation {
			health = cfg.SeedSaturation
		}
		s += cfg.WeightHealth * health / cfg.SeedSaturation
	}
	if contains(cfg.PreferredGroups, r.Group) {
		s += cfg.WeightPreferredGroup
	}
	if containsKeyword(cfg.PreferredKeywords, r.Title) {
		s += cfg.WeightPreferredKeyword
	}
	if hasFlag(r.IndexerFlags, "freeleech") {
		s += cfg.WeightFreeleech
	}
	if r.Proper || r.Repack {
		s += cfg.WeightProper
	}
	if len(cfg.PreferredLanguages) > 0 || cfg.PreferEnglishSubs {
		langs := release.DetectLanguages(r.Title)
		for _, pl := range cfg.PreferredLanguages {
			if contains(langs, pl) {
				s += cfg.WeightLanguage
			}
		}
		// "Highest chance of a preferred language": when NONE of the preferred
		// languages is explicitly tagged, a MULTi release — or one from a group
		// empirically verified to reliably ship a preferred language — likely still
		// carries it, so give it a partial bonus (below an explicitly-tagged release).
		// The Plex check verifies after download and re-searches if it's missing.
		anyPreferredTagged := false
		for _, pl := range cfg.PreferredLanguages {
			if contains(langs, pl) {
				anyPreferredTagged = true
				break
			}
		}
		if len(cfg.PreferredLanguages) > 0 && !anyPreferredTagged {
			likelyGroup := false
			if len(cfg.LikelyLanguageGroups) > 0 {
				if p, err := release.ParseRelease(r.Title); err == nil && p != nil && p.Group != "" {
					likelyGroup = cfg.LikelyLanguageGroups[strings.ToLower(p.Group)]
				}
			}
			if contains(langs, "MULTI") || likelyGroup {
				s += cfg.WeightLanguage / 2
			}
		}
		if cfg.PreferEnglishSubs && release.HasEnglishSubs(r.Title) {
			s += cfg.WeightSubs
		}
	}
	return s
}

// Scored pairs a release with its computed score (after Rank).
type Scored struct {
	Release Release
	Score   int
}

// Rank scores every release and returns them sorted best-first with deterministic
// tie-breaks: score desc, cached desc, health desc, size asc, title asc.
func (cfg Config) Rank(releases []Release) []Scored {
	out := make([]Scored, len(releases))
	for i, r := range releases {
		out[i] = Scored{Release: r, Score: cfg.Score(r)}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Release.Cached != b.Release.Cached {
			return a.Release.Cached
		}
		ha, hb := health(a.Release), health(b.Release)
		if ha != hb {
			return ha > hb
		}
		if a.Release.Size != b.Release.Size {
			return a.Release.Size < b.Release.Size
		}
		return a.Release.Title < b.Release.Title
	})
	return out
}

func health(r Release) int {
	if r.Protocol == "usenet" {
		return r.Grabs
	}
	return r.Seeders
}

func indexOf(list []string, v string) int {
	for i, x := range list {
		if x == v {
			return i
		}
	}
	return -1
}

func contains(list []string, v string) bool { return indexOf(list, v) >= 0 }

func containsKeyword(keywords []string, title string) bool {
	lt := strings.ToLower(title)
	for _, kw := range keywords {
		if kw != "" && strings.Contains(lt, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

func hasFlag(flags []string, want string) bool {
	for _, f := range flags {
		if strings.EqualFold(f, want) {
			return true
		}
	}
	return false
}
