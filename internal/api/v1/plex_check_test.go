package v1

import (
	"testing"

	"github.com/radaiko/boxarr/internal/plex"
)

func TestIsHeavyAnalysisPref(t *testing.T) {
	heavy := []string{"enableBIFGeneration", "enableCreditsMarkerGeneration", "enableIntroMarkerGeneration", "enableVoiceActivityGeneration", "loudnessAnalysis"}
	for _, id := range heavy {
		if !isHeavyAnalysisPref(id) {
			t.Errorf("%q should be flagged heavy", id)
		}
	}
	light := []string{"enableAutoPhotoTags", "augmentWithProviderContent", "scannerThreadCount"}
	for _, id := range light {
		if isHeavyAnalysisPref(id) {
			t.Errorf("%q should NOT be flagged heavy", id)
		}
	}
}

func TestPlexLocationMatches(t *testing.T) {
	locs := []plex.Location{{Path: "/mnt/library/movies"}}
	if !plexLocationMatches(locs, "/mnt/library/movies") {
		t.Error("exact path should match")
	}
	if !plexLocationMatches(locs, "/mnt/library/movies/") {
		t.Error("trailing slash should match")
	}
	if !plexLocationMatches([]plex.Location{{Path: "/mnt/library"}}, "/mnt/library/movies") {
		t.Error("parent location should match (Plex scans a superset)")
	}
	if plexLocationMatches(locs, "/data/films") {
		t.Error("unrelated path must not match")
	}
}

func TestSettingTruthy(t *testing.T) {
	cases := map[string]bool{`"1"`: true, `"0"`: false, `true`: true, `false`: false, `""`: false, `1`: true}
	for raw, want := range cases {
		s := plex.Setting{Value: []byte(raw)}
		if s.Truthy() != want {
			t.Errorf("Truthy(%s) = %v, want %v", raw, s.Truthy(), want)
		}
	}
}
