package store

import (
	"context"
	"testing"
)

func TestReleaseLangKnowledge(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	_ = st.UpsertReleaseLang(ctx, "Show.S01E01.German.DL-WAYNE", "WAYNE", []string{"de", "en"}, []string{"de"}, "plex")
	_ = st.UpsertReleaseLang(ctx, "Film.2024.English-OTHER", "OTHER", []string{"en"}, nil, "plex")

	groups, err := st.GroupsProvidingLanguage(ctx, "de")
	if err != nil {
		t.Fatal(err)
	}
	if !groups["wayne"] {
		t.Error("WAYNE should be known to provide German")
	}
	if groups["other"] {
		t.Error("OTHER should not provide German")
	}
	// Upsert is keyed by release name.
	_ = st.UpsertReleaseLang(ctx, "Show.S01E01.German.DL-WAYNE", "WAYNE", []string{"de"}, nil, "plex")
	if rows, _ := st.ListReleaseLangs(ctx, 10); len(rows) != 2 {
		t.Fatalf("expected 2 distinct releases, got %d", len(rows))
	}
}

func TestGroupLanguageStats(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	// RELIABLE: 4 releases, all German -> ratio 1.0.
	for i, n := range []string{"a", "b", "c", "d"} {
		_ = st.UpsertReleaseLang(ctx, "R.RELIABLE."+n+string(rune('0'+i)), "RELIABLE", []string{"de"}, nil, "plex")
	}
	// MIXED: 4 releases, 1 German -> ratio 0.25.
	_ = st.UpsertReleaseLang(ctx, "R.MIXED.de", "MIXED", []string{"de"}, nil, "plex")
	for _, n := range []string{"x", "y", "z"} {
		_ = st.UpsertReleaseLang(ctx, "R.MIXED."+n, "MIXED", []string{"en"}, nil, "plex")
	}
	stats, err := st.GroupLanguageStats(ctx, "de")
	if err != nil {
		t.Fatal(err)
	}
	byG := map[string]GroupLangStat{}
	for _, s := range stats {
		byG[s.Group] = s
	}
	if g := byG["reliable"]; g.Total != 4 || g.InLang != 4 || g.Ratio != 1.0 {
		t.Errorf("reliable: %+v, want total=4 inLang=4 ratio=1", g)
	}
	if g := byG["mixed"]; g.Total != 4 || g.InLang != 1 || g.Ratio != 0.25 {
		t.Errorf("mixed: %+v, want total=4 inLang=1 ratio=0.25", g)
	}
	// Sorted by InLang desc: reliable (4) before mixed (1).
	if len(stats) < 2 || stats[0].Group != "reliable" {
		t.Errorf("expected reliable first by InLang, got %+v", stats)
	}
}
