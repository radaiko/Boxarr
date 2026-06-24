package catalog

import (
	"context"
	"testing"
	"time"

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

func TestUpgradeDue(t *testing.T) {
	cat, _, _ := newCatalog(t, selCfg())
	now := time.Now()
	recent := now.Add(-2 * time.Hour)
	old2024 := "2024-01-01"

	// force always re-searches.
	if !cat.upgradeDue(old2024, &recent, now, false, true) {
		t.Error("force must always be due")
	}
	// lang_missing on an old release: retries on the daily interval (due after 2h<1d? no) —
	// recently searched (2h ago) is NOT yet due on a daily cadence.
	if cat.upgradeDue(old2024, &recent, now, true, false) {
		t.Error("lang_missing searched 2h ago should not be due yet (daily cadence)")
	}
	// lang_missing searched 2 days ago: due (daily cadence), even though the release
	// is >1yr old (which on the normal slow cadence would NOT be due).
	twoDaysAgo := now.Add(-48 * time.Hour)
	if !cat.upgradeDue(old2024, &twoDaysAgo, now, true, false) {
		t.Error("lang_missing searched 2d ago should be due on the daily cadence")
	}
	if cat.upgradeDue(old2024, &twoDaysAgo, now, false, false) {
		t.Error("non-lang_missing old release searched 2d ago should NOT be due (slow cadence)")
	}
	// never searched → due.
	if !cat.upgradeDue(old2024, nil, now, true, false) {
		t.Error("never-searched lang_missing should be due")
	}
}
