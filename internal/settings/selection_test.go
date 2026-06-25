package settings

import (
	"context"
	"testing"

	"github.com/radaiko/boxarr/internal/config"
)

func TestSelectionConfigOverlaysDB(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	// Seed carries env/defaults; DB overrides must win and be reflected live.
	seed := &config.Config{
		SelectPreferredResolutions: []string{"1080p"},
		SelectMinSeeders:           1,
		SelectWeightResolution:     400,
		SelectMaxSize:              0,
		SelectRequireCached:        false,
	}
	s, err := New(ctx, st, seed)
	if err != nil {
		t.Fatal(err)
	}
	// Before any override: falls back to seed.
	if c := s.SelectionConfig(); c.MinSeeders != 1 || len(c.PreferredResolutions) != 1 || c.RequireCached {
		t.Fatalf("seed fallback wrong: %+v", c)
	}
	// Override several knobs of different types.
	_ = s.Set(ctx, KeySelectPreferredResolutions, "2160p,1080p,720p")
	_ = s.Set(ctx, KeySelectMinSeeders, "5")
	_ = s.Set(ctx, KeySelectMaxSize, "9000000000")
	_ = s.Set(ctx, KeySelectRequireCached, "true")
	_ = s.Set(ctx, KeySelectWeightResolution, "999")
	_ = s.Set(ctx, KeySelectBlockedKeywords, "CAM,TS")

	c := s.SelectionConfig()
	if len(c.PreferredResolutions) != 3 || c.PreferredResolutions[0] != "2160p" {
		t.Errorf("preferred resolutions not overlaid: %v", c.PreferredResolutions)
	}
	if c.MinSeeders != 5 {
		t.Errorf("min seeders = %d, want 5", c.MinSeeders)
	}
	if c.MaxSize != 9000000000 {
		t.Errorf("max size = %d, want 9e9", c.MaxSize)
	}
	if !c.RequireCached {
		t.Error("require cached should be true")
	}
	if c.WeightResolution != 999 {
		t.Errorf("weight resolution = %d, want 999", c.WeightResolution)
	}
	if len(c.BlockedKeywords) != 2 || c.BlockedKeywords[0] != "CAM" {
		t.Errorf("blocked keywords not overlaid: %v", c.BlockedKeywords)
	}
	// Seed must be untouched by the shallow-copy overlay.
	if seed.SelectMinSeeders != 1 || len(seed.SelectPreferredResolutions) != 1 {
		t.Errorf("overlay mutated the seed: %+v", seed)
	}
	// All selection keys are writable.
	if !Writable(KeySelectWeightProper) || !Writable(KeySelectSizeLimits) {
		t.Error("selection keys must be writable")
	}
	// They surface (non-secret) in EffectiveNonSecret.
	eff := s.EffectiveNonSecret()
	if eff[KeySelectMinSeeders] != "5" || eff[KeySelectPreferredResolutions] != "2160p,1080p,720p" {
		t.Errorf("selection not in EffectiveNonSecret: %q / %q", eff[KeySelectMinSeeders], eff[KeySelectPreferredResolutions])
	}
}

func TestSelectionConfigUppercasesLanguages(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	s, err := New(ctx, st, &config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// A lower-case / spaced override must be normalized to upper-case so it matches
	// DetectLanguages output (which the score compares case-sensitively).
	_ = s.Set(ctx, KeySelectMovieLangPreferred, "de, en")
	_ = s.Set(ctx, KeySelectMovieLangRequired, "de")
	c := s.SelectionConfigFor("movie")
	if len(c.PreferredLanguages) != 2 || c.PreferredLanguages[0] != "DE" || c.PreferredLanguages[1] != "EN" {
		t.Errorf("preferred languages not upper-cased: %v", c.PreferredLanguages)
	}
	if len(c.RequiredLanguages) != 1 || c.RequiredLanguages[0] != "DE" {
		t.Errorf("required languages not upper-cased: %v", c.RequiredLanguages)
	}
}

func TestLikelyGroupsLearnRequiredLanguage(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	// A group reliable for German (4/4) — German is REQUIRED, English merely preferred.
	for _, n := range []string{"a", "b", "c", "d"} {
		_ = st.UpsertReleaseLang(ctx, "Film."+n+".German.DL-WAYNE", "WAYNE", []string{"de"}, nil, "plex")
	}
	s, err := New(ctx, st, &config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Set(ctx, KeySelectMovieLangRequired, "DE")
	_ = s.Set(ctx, KeySelectMovieLangPreferred, "EN")
	c := s.SelectionConfigFor("movie")
	if !c.LikelyLanguageGroups["wayne"] {
		t.Errorf("WAYNE (reliable for required DE) should be a likely-language group: %v", c.LikelyLanguageGroups)
	}
}
