package release

import "testing"

func TestParseReleaseGolden(t *testing.T) {
	cases := []struct {
		in      string
		season  int
		epStart int
		epEnd   int
		pack    bool
		airDate string
	}{
		{"Mr.Robot.S01E05.HDTV.x264-KILLERS", 1, 5, 0, false, ""},
		{"Show.Name.S01E01-E03.1080p.WEB-DL.x264-GRP", 1, 1, 3, false, ""},
		{"Show.Name.S01E01E02.1080p.BluRay.x264-GRP", 1, 1, 2, false, ""},
		{"Sample Series S01 COMPLETE 720p WEBRip x264-GRP", 1, 0, 0, true, ""},
		{"Show.S01.1080p.BluRay", 1, 0, 0, true, ""},
		{"The.Daily.Show.2024.01.15.720p.WEB-DL", 0, 0, 0, false, "2024-01-15"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			r, err := ParseRelease(c.in)
			if err != nil {
				t.Fatalf("ParseRelease: %v", err)
			}
			if r.SeasonNumber != c.season {
				t.Errorf("season = %d, want %d", r.SeasonNumber, c.season)
			}
			if r.EpisodeStart != c.epStart {
				t.Errorf("epStart = %d, want %d", r.EpisodeStart, c.epStart)
			}
			if r.EpisodeEnd != c.epEnd {
				t.Errorf("epEnd = %d, want %d", r.EpisodeEnd, c.epEnd)
			}
			if r.IsSeasonPack != c.pack {
				t.Errorf("isSeasonPack = %v, want %v", r.IsSeasonPack, c.pack)
			}
			if r.AirDate != c.airDate {
				t.Errorf("airDate = %q, want %q", r.AirDate, c.airDate)
			}
		})
	}
}

func TestParseReleaseAnime(t *testing.T) {
	// Fansub absolute-numbered release: anitogo should surface the absolute episode.
	r, err := ParseRelease("[HorribleSubs] Detective Conan - 862 [1080p].mkv")
	if err != nil {
		t.Fatalf("ParseRelease: %v", err)
	}
	if !r.IsAnime {
		t.Error("expected IsAnime=true for a [Group]-prefixed release")
	}
	if len(r.AbsoluteEpisodes) == 0 || r.AbsoluteEpisodes[0] != 862 {
		t.Errorf("AbsoluteEpisodes = %v, want [862]", r.AbsoluteEpisodes)
	}
}
