package catalog

import (
	"fmt"
	"testing"

	"github.com/radaiko/boxarr/internal/media"
)

// soloLeveling builds the live-instance pattern: one flat TMDB season of 25 eps,
// 12 aired early 2024, 13 aired early 2025 (a ~9-month gap before E13).
func soloLeveling() []*media.Episode {
	var eps []*media.Episode
	d2024 := []string{"2024-01-07", "2024-01-14", "2024-01-21", "2024-01-28", "2024-02-04", "2024-02-11", "2024-02-18", "2024-03-03", "2024-03-10", "2024-03-17", "2024-03-24", "2024-03-31"}
	d2025 := []string{"2025-01-05", "2025-01-12", "2025-01-19", "2025-01-26", "2025-02-02", "2025-02-09", "2025-02-16", "2025-02-23", "2025-03-02", "2025-03-09", "2025-03-16", "2025-03-23", "2025-03-30"}
	all := append(append([]string{}, d2024...), d2025...)
	titles := make([]string, 25)
	titles[24] = "On to the Next Target"
	for i, air := range all {
		eps = append(eps, &media.Episode{ID: int64(i + 1), SeasonNumber: 1, EpisodeNumber: i + 1, AirDate: air, Title: titles[i]})
	}
	return eps
}

func TestSceneNumbersSplit(t *testing.T) {
	sc := sceneNumbers(soloLeveling())
	// E12 stays scene S01E12; E13 becomes scene S02E01; E25 becomes scene S02E13.
	cases := []struct{ id, season, episode, absolute int }{
		{12, 1, 12, 12}, {13, 2, 1, 13}, {25, 2, 13, 25},
	}
	for _, c := range cases {
		got := sc[int64(c.id)]
		if got.season != c.season || got.episode != c.episode || got.absolute != c.absolute {
			t.Errorf("ep %d: got S%02dE%02d abs%d, want S%02dE%02d abs%d",
				c.id, got.season, got.episode, got.absolute, c.season, c.episode, c.absolute)
		}
	}
}

func TestEpisodeMatchesAndQueries(t *testing.T) {
	eps := soloLeveling()
	sc := sceneNumbers(eps)
	e25 := eps[24] // S01E25 "On to the Next Target" => scene S02E13, abs 25
	scene := sc[e25.ID]

	qs := episodeQueries("Solo Leveling", e25, scene)
	if fmt.Sprint(qs) != "[Solo Leveling S01E25 Solo Leveling S02E13 Solo Leveling 25]" {
		t.Errorf("queries = %v", qs)
	}

	match := map[string]bool{
		"Solo.Leveling.S02E13.On.to.the.Next.Target.1080p.WEB-DL":   true,  // scene S/E
		"[Yameii] Solo Leveling - S02E13 [English Dub] [CR WEB-DL]": true,  // scene S/E (the dub!)
		"Solo.Leveling.S01E25.1080p.WEB-DL":                         true,  // original S/E
		"[SubsPlease] Solo Leveling - 25 (1080p)":                   true,  // absolute
		"Solo.Leveling.S02E12-13.MULTi.1080p":                       true,  // scene range
		"Solo.Leveling.S02E05.1080p.WEB-DL":                         false, // different scene episode
		"Solo.Leveling.S01E10.1080p.WEB-DL":                         false, // different original episode
	}
	for title, want := range match {
		if got := episodeMatches(title, e25, scene); got != want {
			t.Errorf("episodeMatches(%q) = %v, want %v", title, got, want)
		}
	}
}
