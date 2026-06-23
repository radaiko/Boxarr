package catalog

import (
	"context"
	"testing"

	"github.com/radaiko/boxarr/internal/selection"
)

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

func TestVerifiedLacksLang(t *testing.T) {
	cat, st, _ := newCatalog(t, selCfg())
	ctx := context.Background()
	_ = st.UpsertReleaseLang(ctx, "Show.S01E01.German.DL-LIAR", "liar", []string{"en"}, nil, "plex")
	_ = st.UpsertReleaseLang(ctx, "Show.S01E01.German.DL-REAL", "real", []string{"de", "en"}, nil, "plex")

	// Recorded but no German (name lied) → lacking for a DE goal.
	if !cat.verifiedLacksLang(ctx, "Show.S01E01.German.DL-LIAR", []string{"DE"}, false) {
		t.Error("LIAR has no German → should be verified-lacking")
	}
	// Recorded with German → not lacking.
	if cat.verifiedLacksLang(ctx, "Show.S01E01.German.DL-REAL", []string{"DE"}, false) {
		t.Error("REAL has German → should not be lacking")
	}
	// Never recorded → never skipped.
	if cat.verifiedLacksLang(ctx, "Unknown.Release", []string{"DE"}, false) {
		t.Error("unrecorded release must not be treated as lacking")
	}
	// requireAny (anime DE or EN): English present satisfies.
	if cat.verifiedLacksLang(ctx, "Show.S01E01.German.DL-LIAR", []string{"DE", "EN"}, true) {
		t.Error("English present satisfies requireAny [DE,EN]")
	}
}

func TestShouldGrabUpgrade(t *testing.T) {
	cat, _, _ := newCatalog(t, selCfg())
	cfg := selection.Config{PreferredLanguages: []string{"DE", "EN"}, RequireAnyLanguage: true}
	ideal := []string{"DE", "EN"}
	cur := "Solo.Leveling.S01E13.2024.1080p.AMZN.WEB-DL-ADWeb" // verified jpn-only, lang_missing

	// lang_missing: a lower-quality but language-providing release IS grabbed —
	// language beats quality for an unwatchable file.
	if !cat.shouldGrabUpgrade(cfg, ideal, cur, "Solo.Leveling.S01E13.480p.German.DL-GRP", true) {
		t.Error("lang_missing should accept a language-providing release at lower quality")
	}
	// lang_missing: never re-grab the identical current release.
	if cat.shouldGrabUpgrade(cfg, ideal, cur, cur, true) {
		t.Error("must not re-grab the current release")
	}
	// lang_missing: an untagged candidate (no DE/EN/MULTi) is not grabbed.
	if cat.shouldGrabUpgrade(cfg, ideal, cur, "Solo.Leveling.S01E13.720p.WEB-DL", true) {
		t.Error("untagged candidate must not be grabbed")
	}
	// not lang_missing: an equal release is not an upgrade (no quality churn).
	de := "Solo.Leveling.S01E13.1080p.German.DL-GRP"
	if cat.shouldGrabUpgrade(cfg, ideal, de, de, false) {
		t.Error("non-missing: equal release is not an upgrade")
	}
}
