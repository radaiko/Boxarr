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

func TestParseReleaseAnimeGroupFromFansubPrefix(t *testing.T) {
	// torrentname mis-parses the [Group] fansub layout and grabs the SxxEyy token
	// as the group; anitogo recovers the real release group.
	cases := []struct{ in, group string }{
		{"[Yameii] Witch Hat Atelier - S01E07 [English Dub] [CR WEB-DL 1080p H264 AAC] [4649D9F7]", "Yameii"},
		{"[Yameii] Solo Leveling - S02E11 [English Dub] [CR WEB-DL 1080p] [1CD7B335]", "Yameii"},
		{"[Yameii] Let This Grieving Soul Retire-S01E21 [English Dub] [CR WEB-DL 1080p H264 AAC] [B44D3D84]", "Yameii"},
		{"[SubsPlease] Frieren - 01 (1080p) [9C2D4F8A].mkv", "SubsPlease"},
	}
	for _, c := range cases {
		r, err := ParseRelease(c.in)
		if err != nil {
			t.Fatalf("ParseRelease(%q): %v", c.in, err)
		}
		if r.Group != c.group {
			t.Errorf("Group = %q, want %q for %q", r.Group, c.group, c.in)
		}
	}
}

func TestParseReleaseSceneGroupUnchanged(t *testing.T) {
	// Scene-style release: anitogo returns no group, so torrentname's correct
	// trailing group must be kept.
	r, err := ParseRelease("Bleach.S02E06.1080p.DSNP.WEB-DL.MULTi.AAC2.0.H.264-DUSKLiGHT")
	if err != nil {
		t.Fatalf("ParseRelease: %v", err)
	}
	if r.Group != "DUSKLiGHT" {
		t.Errorf("Group = %q, want %q", r.Group, "DUSKLiGHT")
	}
}
