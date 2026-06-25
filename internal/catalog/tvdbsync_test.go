package catalog

import (
	"testing"

	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/metadata/tvdb"
)

func TestResolveSceneMapBingeDrop(t *testing.T) {
	abs := func(n int) *int { return &n }
	// A binge-drop season: all 4 TVDB episodes share one air date.
	tvEps := []tvdb.Episode{
		{Number: 1, SeasonNumber: 1, AbsoluteNumber: abs(1), Aired: "2026-01-01"},
		{Number: 2, SeasonNumber: 1, AbsoluteNumber: abs(2), Aired: "2026-01-01"},
		{Number: 3, SeasonNumber: 1, AbsoluteNumber: abs(3), Aired: "2026-01-01"},
		{Number: 4, SeasonNumber: 1, AbsoluteNumber: abs(4), Aired: "2026-01-01"},
	}
	local := []*media.Episode{
		{ID: 11, SeasonNumber: 1, EpisodeNumber: 1, AirDate: "2026-01-01"},
		{ID: 12, SeasonNumber: 1, EpisodeNumber: 2, AirDate: "2026-01-01"},
		{ID: 13, SeasonNumber: 1, EpisodeNumber: 3, AirDate: "2026-01-01"},
		{ID: 14, SeasonNumber: 1, EpisodeNumber: 4, AirDate: "2026-01-01"},
	}
	m := resolveSceneMap(tvEps, local)
	// Each local episode must map to its OWN number, not collapse onto E4.
	want := map[int64]int{11: 1, 12: 2, 13: 3, 14: 4}
	for id, wantEp := range want {
		if m[id].episode != wantEp || m[id].season != 1 {
			t.Errorf("episode id %d mapped to S%02dE%02d, want S01E%02d", id, m[id].season, m[id].episode, wantEp)
		}
	}
}

func TestResolveSceneMapAnimeAbsolute(t *testing.T) {
	abs := func(n int) *int { return &n }
	// Anime: flat TMDB S01E13 should map to TVDB S02E01 via absolute, even with no air date.
	tvEps := []tvdb.Episode{
		{Number: 12, SeasonNumber: 1, AbsoluteNumber: abs(12)},
		{Number: 1, SeasonNumber: 2, AbsoluteNumber: abs(13)},
	}
	local := []*media.Episode{{ID: 1, SeasonNumber: 1, EpisodeNumber: 13}}
	m := resolveSceneMap(tvEps, local)
	if m[1].season != 2 || m[1].episode != 1 {
		t.Errorf("flat E13 mapped to S%02dE%02d, want S02E01", m[1].season, m[1].episode)
	}
}
