package selection

import (
	"math"
	"testing"
)

// defaultCfg mirrors the locked defaults (06 §3.5 / config defaults).
func defaultCfg() Config {
	return Config{
		PreferredResolutions:          []string{"2160p", "1080p", "720p"},
		PreferredQualities:            []string{"WEB-DL", "BluRay", "WEBRip", "HDTV"},
		MinSeeders:                    1,
		WeightResolution:              400,
		WeightQuality:                 200,
		WeightProtocolCachedTorrent:   300,
		WeightProtocolUsenet:          200,
		WeightProtocolUncachedTorrent: 100,
		WeightHealth:                  100,
		SeedSaturation:                100,
		WeightPreferredGroup:          150,
		WeightPreferredKeyword:        50,
		WeightFreeleech:               40,
		WeightProper:                  25,
	}
}

func TestProtocolPreferenceCachedUsenetUncached(t *testing.T) {
	cfg := defaultCfg()
	cached := cfg.Score(Release{Protocol: "torrent", Cached: true, Resolution: "1080p", Quality: "WEB-DL", Seeders: 50})
	usenet := cfg.Score(Release{Protocol: "usenet", Resolution: "1080p", Quality: "WEB-DL", Grabs: 50})
	uncached := cfg.Score(Release{Protocol: "torrent", Cached: false, Resolution: "1080p", Quality: "WEB-DL", Seeders: 50})
	if cached <= usenet || usenet <= uncached {
		t.Fatalf("want cached(%d) > usenet(%d) > uncached(%d)", cached, usenet, uncached)
	}
}

func TestResolutionPreferenceWeighting(t *testing.T) {
	cfg := defaultCfg()
	uhd := cfg.Score(Release{Protocol: "usenet", Resolution: "2160p", Quality: "WEB-DL", Grabs: 5})
	hd := cfg.Score(Release{Protocol: "usenet", Resolution: "720p", Quality: "WEB-DL", Grabs: 5})
	if uhd <= hd {
		t.Fatalf("2160p (%d) should outscore 720p (%d)", uhd, hd)
	}
}

func TestRejectConditions(t *testing.T) {
	cfg := defaultCfg()
	cfg.BlockedKeywords = []string{"CAM"}
	cfg.MaxSize = 1000
	cases := map[string]Release{
		"below min seeders": {Protocol: "torrent", Seeders: 0, Resolution: "1080p"},
		"blocked keyword":   {Protocol: "usenet", Title: "Movie.2024.CAM", Grabs: 5, Resolution: "1080p"},
		"over max size":     {Protocol: "usenet", Size: 2000, Grabs: 5, Resolution: "1080p"},
	}
	for name, r := range cases {
		if cfg.Score(r) != math.MinInt {
			t.Errorf("%s: should be rejected (MinInt), got %d", name, cfg.Score(r))
		}
	}
	cfg.AllowedResolutions = []string{"2160p"}
	if cfg.Score(Release{Protocol: "usenet", Resolution: "1080p", Grabs: 5}) != math.MinInt {
		t.Error("resolution outside allow-list should be rejected")
	}
}

func TestRankOrdersAndTieBreaks(t *testing.T) {
	cfg := defaultCfg()
	releases := []Release{
		{Title: "uncached", Protocol: "torrent", Resolution: "1080p", Quality: "WEB-DL", Seeders: 10},
		{Title: "cached-big", Protocol: "torrent", Cached: true, Resolution: "1080p", Quality: "WEB-DL", Seeders: 10, Size: 9000},
		{Title: "cached-small", Protocol: "torrent", Cached: true, Resolution: "1080p", Quality: "WEB-DL", Seeders: 10, Size: 100},
	}
	ranked := cfg.Rank(releases)
	if ranked[0].Release.Title != "cached-small" {
		t.Fatalf("cached + smaller size should rank first, got %q", ranked[0].Release.Title)
	}
	if ranked[2].Release.Title != "uncached" {
		t.Fatalf("uncached should rank last, got %q", ranked[2].Release.Title)
	}
}

func TestRejectedReleaseRanksLast(t *testing.T) {
	cfg := defaultCfg()
	ranked := cfg.Rank([]Release{
		{Title: "bad", Protocol: "torrent", Seeders: 0, Resolution: "1080p"}, // rejected
		{Title: "good", Protocol: "usenet", Grabs: 5, Resolution: "1080p", Quality: "WEB-DL"},
	})
	if ranked[0].Release.Title != "good" || ranked[1].Score != math.MinInt {
		t.Fatalf("rejected should sort last with MinInt score: %+v", ranked)
	}
}
