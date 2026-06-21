package selection

import "testing"

func TestLanguageGateAllOf(t *testing.T) {
	// Movies/series: German required (all-of), English preferred.
	cfg := Config{RequiredLanguages: []string{"DE"}, PreferredLanguages: []string{"EN"}, WeightLanguage: 40}

	if cfg.Rejected(Release{Title: "Avatar.2009.German.DL.1080p.BluRay.x264-GRP"}) {
		t.Error("German DL release should pass")
	}
	if !cfg.Rejected(Release{Title: "Avatar.2009.1080p.BluRay.x264-GRP"}) {
		t.Error("English-only (no German) should be rejected")
	}
	if cfg.Rejected(Release{Title: "Anaconda.2025.MULTi.2160p.BluRay.REMUX-GRP"}) {
		t.Error("MULTi should satisfy the required-language gate")
	}
	// English-preferred boost: German DL (DE+EN) > German-only (DE).
	deEN := cfg.Score(Release{Title: "Avatar.2009.German.DL.1080p.BluRay-GRP"})
	deOnly := cfg.Score(Release{Title: "Avatar.2009.German.1080p.BluRay-GRP"})
	if deEN <= deOnly {
		t.Errorf("EN-preferred should boost the score: deEN=%d deOnly=%d", deEN, deOnly)
	}
}

func TestLanguageGateAnyOfAnime(t *testing.T) {
	// Anime: German OR English (any-of), prefer English subs.
	cfg := Config{RequiredLanguages: []string{"DE", "EN"}, RequireAnyLanguage: true, PreferEnglishSubs: true, WeightSubs: 20}

	if cfg.Rejected(Release{Title: "Frieren.S01E12.1080p.WEB.x264"}) {
		t.Error("untagged anime should pass any-of (assumed original/English)")
	}
	if cfg.Rejected(Release{Title: "Frieren.S01E12.German.1080p.WEB-GRP"}) {
		t.Error("German anime should pass")
	}
	if !cfg.Rejected(Release{Title: "Frieren.S01E12.FRENCH.1080p.WEB.x264"}) {
		t.Error("French-only anime should be rejected (any-of DE/EN, languages detected)")
	}
	withSubs := cfg.Score(Release{Title: "Frieren.S01E12.SUBBED.1080p.WEB-GRP"})
	noSubs := cfg.Score(Release{Title: "Frieren.S01E12.1080p.WEB-GRP"})
	if withSubs <= noSubs {
		t.Errorf("English subs should boost the score: %d vs %d", withSubs, noSubs)
	}
}
