package selection

import "testing"

// MULTi should score above an English-only release when German is the top
// preferred language (highest chance of German), but below explicit German.
func TestMultiLikelihoodBonus(t *testing.T) {
	cfg := Config{PreferredLanguages: []string{"DE"}, WeightLanguage: 100}
	de := cfg.Score(Release{Title: "Film.2024.German.DL.1080p.BluRay.x264-GRP"})
	multi := cfg.Score(Release{Title: "Film.2024.MULTi.1080p.BluRay.x264-GRP"})
	en := cfg.Score(Release{Title: "Film.2024.1080p.BluRay.x264-GRP"})
	if de <= multi || multi <= en {
		t.Fatalf("want de > multi > en, got de=%d multi=%d en=%d", de, multi, en)
	}
}
