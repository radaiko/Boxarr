package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/metadata/tmdb"
	"github.com/radaiko/boxarr/internal/prowlarr"
	"github.com/radaiko/boxarr/internal/store"
)

// fakeSearcher returns a canned release list for every query.
type fakeSearcher struct{ results []prowlarr.ReleaseResource }

func (f *fakeSearcher) Search(_ context.Context, _ prowlarr.SearchParams) ([]prowlarr.ReleaseResource, error) {
	return f.results, nil
}

func selCfg() *config.Config {
	return &config.Config{
		MovieLibraryRoot: "/data/movies", TVLibraryRoot: "/data/tv",
		SelectPreferredResolutions: []string{"2160p", "1080p"},
		SelectMinSeeders:           1, SelectWeightResolution: 400, SelectWeightProtocolUsenet: 200,
		SelectWeightHealth: 100, SelectSeedSaturation: 100,
	}
}

func newCatalog(t *testing.T, cfg *config.Config) (*Service, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(st, tmdb.NewWithBaseURL("tok", "http://unused"), cfg), st
}

func TestAutoSearchGrabsBestForWantedMovie(t *testing.T) {
	cfg := selCfg()
	cat, st := newCatalog(t, cfg)
	ctx := context.Background()

	// An NZB indexer endpoint the grab will fetch.
	nzb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<nzb/>"))
	}))
	defer nzb.Close()

	cat.SetSearcher(&fakeSearcher{results: []prowlarr.ReleaseResource{
		{Title: "Movie.2024.1080p.WEB-DL", Protocol: "usenet", Grabs: 50, Size: 1e9, DownloadURL: nzb.URL + "/x.nzb"},
	}})

	mid, _ := st.CreateMovie(ctx, &media.Movie{TMDBID: 1, Title: "Movie", Year: 2024,
		Monitored: true, Status: media.MediaWanted, ReleaseDate: "2024-01-01"})

	if err := cat.AutoSearchWanted(ctx); err != nil {
		t.Fatalf("AutoSearchWanted: %v", err)
	}
	// A pending job linked to the movie was created.
	jobs, _ := st.JobsByState(ctx, job.StatePending)
	if len(jobs) != 1 || jobs[0].MediaType != "movie" || jobs[0].MediaRef != mid {
		t.Fatalf("auto-search did not grab the movie: %+v", jobs)
	}
	m, _ := st.GetMovie(ctx, mid)
	if m.Status != media.MediaSearching || m.JobID == 0 {
		t.Fatalf("movie not linked to grab: status=%s jobID=%d", m.Status, m.JobID)
	}
}

func TestAutoSearchDisabledWithoutSearcher(t *testing.T) {
	cat, st := newCatalog(t, selCfg())
	ctx := context.Background()
	_, _ = st.CreateMovie(ctx, &media.Movie{TMDBID: 1, Title: "M", Monitored: true,
		Status: media.MediaWanted, ReleaseDate: "2024-01-01"})
	if err := cat.AutoSearchWanted(ctx); err != nil {
		t.Fatalf("AutoSearchWanted: %v", err)
	}
	jobs, _ := st.JobsByState(ctx, job.StatePending)
	if len(jobs) != 0 {
		t.Error("auto-search must be a no-op when no searcher is wired")
	}
}

func TestRefreshMetadataPromotesAiredEpisode(t *testing.T) {
	cfg := selCfg()
	// TMDB serving a series with a newly-aired episode 2.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/season/"):
			_, _ = w.Write([]byte(`{"season_number":1,"episodes":[
				{"episode_number":1,"season_number":1,"name":"P","air_date":"2008-01-20"},
				{"episode_number":2,"season_number":1,"name":"Q","air_date":"2008-01-27"}]}`))
		default:
			_, _ = w.Write([]byte(`{"id":1,"name":"BB","status":"Ended","first_air_date":"2008-01-20",
				"seasons":[{"season_number":1,"episode_count":2,"air_date":"2008-01-20"}],
				"external_ids":{"tvdb_id":81189}}`))
		}
	}))
	defer srv.Close()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cat := New(st, tmdb.NewWithBaseURL("tok", srv.URL), cfg)
	ctx := context.Background()

	// Seed a series with only episode 1 present (missing status for ep flow).
	sid, _ := st.CreateSeries(ctx, &media.Series{TMDBID: 1, Title: "BB", Monitored: true})
	seasonID, _ := st.UpsertSeason(ctx, &media.Season{SeriesID: sid, SeasonNumber: 1, Monitored: true})
	_, _ = st.UpsertEpisode(ctx, &media.Episode{SeriesID: sid, SeasonID: seasonID, SeasonNumber: 1,
		EpisodeNumber: 1, AirDate: "2008-01-20", Monitored: true, Status: media.MediaMissing})

	if err := cat.RefreshMetadata(ctx); err != nil {
		t.Fatalf("RefreshMetadata: %v", err)
	}
	eps, _ := st.ListEpisodes(ctx, sid)
	if len(eps) != 2 {
		t.Fatalf("refresh should add the new episode, got %d", len(eps))
	}
	for _, e := range eps {
		if e.Status != media.MediaWanted {
			t.Errorf("aired monitored episode S01E%02d should be wanted, got %s", e.EpisodeNumber, e.Status)
		}
	}
}
