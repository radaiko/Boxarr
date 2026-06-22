package selection

import "testing"

// A release from a group verified to ship German outscores an unknown-group
// English release when German is the top preference.
func TestLikelyLanguageGroupBonus(t *testing.T) {
	cfg := Config{PreferredLanguages: []string{"DE"}, WeightLanguage: 100,
		LikelyLanguageGroups: map[string]bool{"wayne": true}}
	wayne := cfg.Score(Release{Title: "Show.S01E01.1080p.WEB.H264-WAYNE"})
	other := cfg.Score(Release{Title: "Show.S01E01.1080p.WEB.H264-OTHER"})
	if wayne <= other {
		t.Fatalf("German-likely group should score higher: wayne=%d other=%d", wayne, other)
	}
}
