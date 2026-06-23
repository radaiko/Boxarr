package catalog

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/prowlarr"
	"github.com/radaiko/boxarr/internal/release"
)

// sceneNum is an episode's broadcast/scene position. Anime on TMDB is often a
// single flat season (e.g. Solo Leveling S01E01-25) while release groups split it
// into broadcast seasons/cours (S01E01-12 + S02E01-13). We can't get that from
// TMDB, so we derive it from air-date gaps. absolute is the 1-based index across
// the whole series (what absolute-numbered fansubs use).
type sceneNum struct {
	season, episode, absolute int
}

// sceneSeasonGap is the air-date gap that starts a new broadcast season. Weekly
// anime airs ~7 days apart and short recaps add a week or two; a genuine new
// cour/season is months later, so 45 days cleanly separates them.
const sceneSeasonGap = 45 * 24 * time.Hour

// sceneNumbers derives scene (season, episode, absolute) for every episode. If
// TheTVDB has populated authoritative scene numbers (scene_season > 0) we trust
// them; otherwise we fall back to deriving them from air-date gaps.
func sceneNumbers(eps []*media.Episode) map[int64]sceneNum {
	hasTVDB := false
	for _, e := range eps {
		if e.SceneSeason > 0 {
			hasTVDB = true
			break
		}
	}
	if hasTVDB {
		out := make(map[int64]sceneNum, len(eps))
		for _, e := range eps {
			if e.SceneSeason > 0 {
				out[e.ID] = sceneNum{season: e.SceneSeason, episode: e.SceneEpisode, absolute: e.AbsoluteNumber}
			} else {
				out[e.ID] = sceneNum{season: e.SeasonNumber, episode: e.EpisodeNumber, absolute: e.AbsoluteNumber}
			}
		}
		return out
	}
	ordered := append([]*media.Episode(nil), eps...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.AirDate != "" && b.AirDate != "" && a.AirDate != b.AirDate {
			return a.AirDate < b.AirDate
		}
		if a.SeasonNumber != b.SeasonNumber {
			return a.SeasonNumber < b.SeasonNumber
		}
		return a.EpisodeNumber < b.EpisodeNumber
	})
	out := make(map[int64]sceneNum, len(ordered))
	season, episode, absolute := 1, 0, 0
	var prev time.Time
	havePrev := false
	for _, e := range ordered {
		absolute++
		t, terr := time.Parse("2006-01-02", e.AirDate)
		switch {
		case havePrev && terr == nil && t.Sub(prev) > sceneSeasonGap:
			season++
			episode = 1
		default:
			episode++
		}
		if terr == nil {
			prev = t
			havePrev = true
		}
		out[e.ID] = sceneNum{season: season, episode: episode, absolute: absolute}
	}
	return out
}

// episodeQueries builds the search-term variations for one anime episode: the
// original (TMDB) numbering, the derived scene numbering, and the absolute number
// — so releases tagged any of those ways are found.
func episodeQueries(title string, ep *media.Episode, sc sceneNum) []string {
	add := map[string]bool{}
	var qs []string
	push := func(q string) {
		if !add[q] {
			add[q] = true
			qs = append(qs, q)
		}
	}
	push(fmt.Sprintf("%s S%02dE%02d", title, ep.SeasonNumber, ep.EpisodeNumber))
	if sc.season != ep.SeasonNumber || sc.episode != ep.EpisodeNumber {
		push(fmt.Sprintf("%s S%02dE%02d", title, sc.season, sc.episode))
	}
	if sc.absolute > 0 {
		push(fmt.Sprintf("%s %02d", title, sc.absolute))
	}
	return qs
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func normTitleKey(s string) string { return nonAlnum.ReplaceAllString(strings.ToLower(s), "") }

// episodeMatches reports whether a release title is for this episode, matching by
// original numbering, scene numbering, absolute number, or episode title — so a
// scene release tagged S02E13 still maps to the flat S01E25 episode.
func episodeMatches(relTitle string, ep *media.Episode, sc sceneNum) bool {
	info, err := release.ParseRelease(relTitle)
	if err != nil || info == nil {
		return false
	}
	hitsSE := func(season, epNum int) bool {
		if info.SeasonNumber != season {
			return false
		}
		if info.IsSeasonPack {
			return true
		}
		if info.EpisodeEnd > 0 {
			return epNum >= info.EpisodeStart && epNum <= info.EpisodeEnd
		}
		return info.EpisodeStart == epNum
	}
	if hitsSE(ep.SeasonNumber, ep.EpisodeNumber) || hitsSE(sc.season, sc.episode) {
		return true
	}
	for _, a := range info.AbsoluteEpisodes {
		if a == sc.absolute {
			return true
		}
	}
	// Episode-title match (anime releases often embed the title and skip numbering).
	if t := normTitleKey(ep.Title); len(t) >= 10 && strings.Contains(normTitleKey(relTitle), t) {
		return true
	}
	return false
}

// episodeReleases returns the candidate releases for one episode. Anime uses the
// scene-aware multi-query search + episode filter; other series keep the simple
// single SxxExx query (TMDB numbering already matches scene for live-action).
func (s *Service) episodeReleases(ctx context.Context, title string, ep *media.Episode, kind string, sc sceneNum) []prowlarr.ReleaseResource {
	if kind == "anime" {
		return s.searchEpisodeReleases(ctx, title, ep, sc)
	}
	q := fmt.Sprintf("%s S%02dE%02d", title, ep.SeasonNumber, ep.EpisodeNumber)
	res, err := s.search.Search(ctx, prowlarr.SearchParams{Query: q, Type: "tvsearch", Categories: []int{5000}})
	if err != nil {
		s.logSearchErr(q, err)
		return nil
	}
	return res
}

// searchEpisodeReleases runs every query variation for an anime episode, merges +
// dedupes the candidates, and keeps only releases that actually map to the episode.
func (s *Service) searchEpisodeReleases(ctx context.Context, title string, ep *media.Episode, sc sceneNum) []prowlarr.ReleaseResource {
	seen := map[string]bool{}
	var pool []prowlarr.ReleaseResource
	for _, q := range episodeQueries(title, ep, sc) {
		res, err := s.search.Search(ctx, prowlarr.SearchParams{Query: q, Type: "tvsearch", Categories: []int{5000}})
		if err != nil {
			s.logSearchErr(q, err)
			continue
		}
		for _, r := range res {
			key := r.GUID
			if key == "" {
				key = r.InfoHash
			}
			if key == "" {
				key = r.Title
			}
			if !seen[key] {
				seen[key] = true
				pool = append(pool, r)
			}
		}
	}
	matched := pool[:0]
	for _, r := range pool {
		if episodeMatches(r.Title, ep, sc) {
			matched = append(matched, r)
		}
	}
	return matched
}
