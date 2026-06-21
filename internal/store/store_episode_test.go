package store

import (
	"context"
	"testing"

	"github.com/radaiko/boxarr/internal/media"
)

func TestMarkEpisodesSearched(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	sid, err := st.CreateSeries(ctx, &media.Series{TMDBID: 1, Title: "X"})
	if err != nil {
		t.Fatal(err)
	}
	seasonID, _ := st.UpsertSeason(ctx, &media.Season{SeriesID: sid, SeasonNumber: 1})
	epID, err := st.UpsertEpisode(ctx, &media.Episode{SeriesID: sid, SeasonID: seasonID, SeasonNumber: 1, EpisodeNumber: 1, TMDBID: 5})
	if err != nil {
		t.Fatal(err)
	}
	if ep, _ := st.GetEpisode(ctx, epID); ep.LastSearchedAt != nil {
		t.Fatal("last_searched_at should start nil")
	}
	if err := st.MarkEpisodesSearched(ctx, epID); err != nil {
		t.Fatal(err)
	}
	if ep, _ := st.GetEpisode(ctx, epID); ep.LastSearchedAt == nil {
		t.Error("last_searched_at should be set after MarkEpisodesSearched")
	}
}
