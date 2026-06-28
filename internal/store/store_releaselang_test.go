package store

import (
	"context"
	"testing"

	"github.com/radaiko/boxarr/internal/media"
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

func TestLangMissingCounts(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	mid, _ := st.CreateMovie(ctx, &media.Movie{TMDBID: 1, Title: "M", Monitored: true})
	sid, _ := st.CreateSeries(ctx, &media.Series{TMDBID: 2, Title: "S", Monitored: true})
	seasonID, _ := st.UpsertSeason(ctx, &media.Season{SeriesID: sid, SeasonNumber: 1})
	e1, _ := st.UpsertEpisode(ctx, &media.Episode{SeriesID: sid, SeasonID: seasonID, SeasonNumber: 1, EpisodeNumber: 1})
	e2, _ := st.UpsertEpisode(ctx, &media.Episode{SeriesID: sid, SeasonID: seasonID, SeasonNumber: 1, EpisodeNumber: 2})

	if mv, ep, _ := st.LangMissingCounts(ctx); mv != 0 || ep != 0 {
		t.Fatalf("baseline should be 0/0, got %d/%d", mv, ep)
	}
	_ = st.SetMovieLangMissing(ctx, mid, true)
	_ = st.SetEpisodeLangMissing(ctx, e1, true)
	_ = st.SetEpisodeLangMissing(ctx, e2, true)
	_ = st.SetEpisodeLangMissing(ctx, e2, false) // flips back → not counted
	mv, ep, err := st.LangMissingCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if mv != 1 || ep != 1 {
		t.Errorf("got %d movies / %d episodes, want 1/1", mv, ep)
	}
}

func TestBackfillReleaseGroups(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	// One row with a wrong (episode-token) group, one already-correct scene row.
	_ = st.UpsertReleaseLang(ctx, "[Yameii] Solo Leveling - S02E11 [English Dub] [CR WEB-DL 1080p] [1CD7B335]",
		"s02e11", []string{"en"}, []string{"en"}, "plex")
	_ = st.UpsertReleaseLang(ctx, "Bleach.S02E06.1080p.DSNP.WEB-DL.MULTi.AAC2.0.H.264-DUSKLiGHT",
		"dusklight", []string{"de", "en"}, nil, "plex")

	regroup := func(name string) string {
		if name == "[Yameii] Solo Leveling - S02E11 [English Dub] [CR WEB-DL 1080p] [1CD7B335]" {
			return "Yameii"
		}
		return "DUSKLiGHT"
	}
	n, err := st.BackfillReleaseGroups(ctx, regroup)
	if err != nil {
		t.Fatalf("BackfillReleaseGroups: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 row updated, got %d", n)
	}
	// The Yameii row's group is corrected (stored lowercased).
	groups, _ := st.GroupsProvidingLanguage(ctx, "en")
	if !groups["yameii"] {
		t.Errorf("yameii group should exist after backfill; got %v", groups)
	}
	if groups["s02e11"] {
		t.Error("the wrong s02e11 group should be gone after backfill")
	}
}
