package catalog

import "testing"

func TestLanguageSatisfied(t *testing.T) {
	if !languageSatisfied("movie", "Film.2024.German.DL.1080p.BluRay") {
		t.Error("German movie should be satisfied")
	}
	if languageSatisfied("movie", "Film.2024.1080p.AMZN.WEB-DL.H264") {
		t.Error("English-only movie should NOT be satisfied (German is the target)")
	}
	if languageSatisfied("series", "Show.S01E01.1080p.WEB-DL") {
		t.Error("non-German series should not be satisfied")
	}
	if !languageSatisfied("anime", "Anime.S01E01.1080p.AMZN.WEB-DL") {
		t.Error("anime is always language-satisfied (DE==EN)")
	}
}
