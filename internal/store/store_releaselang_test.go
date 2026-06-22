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
