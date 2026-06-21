package catalog

import "testing"

func TestLanguageSatisfied(t *testing.T) {
	deOnly := []string{"DE"}
	deEn := []string{"DE", "EN"}
	// Ranked (movies/series): German is the target.
	if !languageSatisfied("Film.2024.German.DL.1080p.BluRay", deOnly, false) {
		t.Error("German movie should be satisfied")
	}
	if languageSatisfied("Film.2024.1080p.AMZN.WEB-DL.H264", deOnly, false) {
		t.Error("English-only movie should NOT be satisfied (German is the target)")
	}
	if languageSatisfied("Film.2024.English.1080p", deEn, false) {
		t.Error("English not satisfied when German is the top preferred (ranked)")
	}
	// requireAny (anime): German OR English is enough.
	if !languageSatisfied("Anime.S01E01.English.1080p", deEn, true) {
		t.Error("English satisfies requireAny")
	}
	if languageSatisfied("Anime.S01E01.Japanese.1080p", deEn, true) {
		t.Error("Japanese-only should not satisfy requireAny [DE,EN]")
	}
	// No goal configured → nothing to upgrade toward.
	if !languageSatisfied("whatever.1080p", nil, false) {
		t.Error("no goal → satisfied")
	}
}
