package store

import (
	"context"
	"testing"

	"github.com/radaiko/boxarr/internal/media"
)

func TestResetFailedForRetry(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	mid, _ := st.CreateMovie(ctx, &media.Movie{TMDBID: 1, Title: "M", Monitored: true})
	_ = st.SetMovieStatus(ctx, mid, media.MediaFailed)
	sid, _ := st.CreateSeries(ctx, &media.Series{TMDBID: 2, Title: "S", Monitored: true})
	seasonID, _ := st.UpsertSeason(ctx, &media.Season{SeriesID: sid, SeasonNumber: 1})
	eid, _ := st.UpsertEpisode(ctx, &media.Episode{SeriesID: sid, SeasonID: seasonID, SeasonNumber: 1, EpisodeNumber: 1})
	_ = st.SetEpisodeStatus(ctx, eid, media.MediaFailed)

	n, err := st.ResetFailedForRetry(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("reset count = %d, want 2", n)
	}
	if m, _ := st.GetMovie(ctx, mid); m.Status != media.MediaWanted || m.JobID != 0 {
		t.Errorf("movie not reset: status=%s jobID=%d", m.Status, m.JobID)
	}
	if e, _ := st.GetEpisode(ctx, eid); e.Status != media.MediaWanted {
		t.Errorf("episode not reset: %s", e.Status)
	}
	// Idempotent: nothing failed now.
	if n2, _ := st.ResetFailedForRetry(ctx); n2 != 0 {
		t.Errorf("second reset should be a no-op, got %d", n2)
	}
}
