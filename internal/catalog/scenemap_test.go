package catalog

import (
	"testing"

	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/metadata/tvdb"
)

func iptr(i int) *int { return &i }

// Skeleton Knight: TMDB split into real S1 + S2. A future S2 episode whose air
// date + (season,episode) don't yet resolve against TVDB must NOT collapse onto
// the season-1 episode that shares its per-season number via the absolute fallback.
func TestResolveSceneMapNoCrossSeasonAbsoluteCollision(t *testing.T) {
	tvEps := []tvdb.Episode{
		{SeasonNumber: 1, Number: 5, Aired: "2022-05-05", AbsoluteNumber: iptr(5)},
	}
	local := []*media.Episode{
		{ID: 1, SeasonNumber: 1, EpisodeNumber: 5, AirDate: "2022-05-05"}, // real S1E5
		{ID: 2, SeasonNumber: 2, EpisodeNumber: 5, AirDate: "2026-08-03"}, // future S2E5
	}
	m := resolveSceneMap(tvEps, local)
	if te, ok := m[1]; !ok || te.season != 1 || te.episode != 5 {
		t.Errorf("S1E5 should map to scene (1,5), got %+v ok=%v", te, ok)
	}
	if te, ok := m[2]; ok {
		t.Errorf("S2E5 must not collapse onto a season-1 scene via byAbs[5], got %+v", te)
	}
}

// Flat anime (Solo Leveling): everything in TMDB season 1; the absolute fallback
// still maps a high local episode number onto TVDB's later season.
func TestResolveSceneMapFlatAnimeStillMapsByAbsolute(t *testing.T) {
	tvEps := []tvdb.Episode{
		{SeasonNumber: 2, Number: 1, Aired: "2025-01-01", AbsoluteNumber: iptr(13)},
	}
	local := []*media.Episode{
		{ID: 7, SeasonNumber: 1, EpisodeNumber: 13}, // flat TMDB E13, no air-date match
	}
	m := resolveSceneMap(tvEps, local)
	if te, ok := m[7]; !ok || te.season != 2 || te.episode != 1 {
		t.Errorf("flat E13 should map to scene (2,1) via absolute, got %+v ok=%v", te, ok)
	}
}
