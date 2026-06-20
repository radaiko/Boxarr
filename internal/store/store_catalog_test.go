package store

import (
	"context"
	"testing"

	"github.com/radaiko/boxarr/internal/media"
)

func TestCatalogCRUDAndWanted(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	sid, err := st.CreateSeries(ctx, &media.Series{TMDBID: 1399, Title: "GoT", Year: 2011, Monitored: true})
	if err != nil {
		t.Fatalf("CreateSeries: %v", err)
	}
	if got, err := st.GetSeriesByTMDB(ctx, 1399); err != nil || got == nil || got.ID != sid {
		t.Fatalf("GetSeriesByTMDB: %+v err=%v", got, err)
	}

	seasonID, err := st.UpsertSeason(ctx, &media.Season{SeriesID: sid, SeasonNumber: 1, Monitored: true, EpisodeCount: 2})
	if err != nil {
		t.Fatalf("UpsertSeason: %v", err)
	}
	aired := &media.Episode{SeriesID: sid, SeasonID: seasonID, SeasonNumber: 1, EpisodeNumber: 1,
		AirDate: "2011-04-17", Monitored: true, Status: media.MediaWanted}
	unaired := &media.Episode{SeriesID: sid, SeasonID: seasonID, SeasonNumber: 1, EpisodeNumber: 2,
		AirDate: "2099-01-01", Monitored: true, Status: media.MediaWanted}
	if _, err := st.UpsertEpisode(ctx, aired); err != nil {
		t.Fatalf("UpsertEpisode aired: %v", err)
	}
	if _, err := st.UpsertEpisode(ctx, unaired); err != nil {
		t.Fatalf("UpsertEpisode unaired: %v", err)
	}

	wanted, err := st.WantedEpisodes(ctx)
	if err != nil {
		t.Fatalf("WantedEpisodes: %v", err)
	}
	if len(wanted) != 1 || wanted[0].EpisodeNumber != 1 {
		t.Fatalf("WantedEpisodes should return only the aired episode, got %d: %+v", len(wanted), wanted)
	}

	// Upsert must preserve lifecycle columns (status/has_file) on metadata refresh.
	if err := st.SetEpisodeStatus(ctx, wanted[0].ID, media.MediaAvailable); err != nil {
		t.Fatalf("SetEpisodeStatus: %v", err)
	}
	if _, err := st.UpsertEpisode(ctx, &media.Episode{SeriesID: sid, SeasonID: seasonID,
		SeasonNumber: 1, EpisodeNumber: 1, AirDate: "2011-04-17", Title: "renamed"}); err != nil {
		t.Fatalf("UpsertEpisode refresh: %v", err)
	}
	eps, _ := st.ListEpisodes(ctx, sid)
	var e1 *media.Episode
	for _, e := range eps {
		if e.EpisodeNumber == 1 {
			e1 = e
		}
	}
	if e1 == nil || e1.Status != media.MediaAvailable || e1.Title != "renamed" {
		t.Fatalf("upsert must refresh metadata but keep status: %+v", e1)
	}

	// CASCADE delete.
	if err := st.DeleteSeries(ctx, sid); err != nil {
		t.Fatalf("DeleteSeries: %v", err)
	}
	if eps, _ := st.ListEpisodes(ctx, sid); len(eps) != 0 {
		t.Fatalf("DeleteSeries should cascade to episodes, got %d", len(eps))
	}
}

func TestMovieCRUDAndWanted(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	released, err := st.CreateMovie(ctx, &media.Movie{TMDBID: 603, Title: "The Matrix", Year: 1999,
		Monitored: true, Status: media.MediaWanted, ReleaseDate: "1999-03-31"})
	if err != nil {
		t.Fatalf("CreateMovie: %v", err)
	}
	if _, err := st.CreateMovie(ctx, &media.Movie{TMDBID: 0, Title: "Future", Year: 2099,
		Monitored: true, Status: media.MediaWanted, ReleaseDate: "2099-01-01"}); err != nil {
		t.Fatalf("CreateMovie future: %v", err)
	}
	if m, err := st.GetMovieByTMDB(ctx, 603); err != nil || m == nil || m.ID != released {
		t.Fatalf("GetMovieByTMDB: %+v err=%v", m, err)
	}
	wanted, err := st.WantedMovies(ctx)
	if err != nil {
		t.Fatalf("WantedMovies: %v", err)
	}
	if len(wanted) != 1 || wanted[0].ID != released {
		t.Fatalf("WantedMovies should return only the released movie, got %+v", wanted)
	}
}
