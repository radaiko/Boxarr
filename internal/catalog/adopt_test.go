package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/settings"
	"github.com/radaiko/boxarr/internal/store"
)

func TestResolveAdoptMovieAndSeries(t *testing.T) {
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/season/"):
			_, _ = w.Write([]byte(`{"season_number":1,"episodes":[{"episode_number":1,"season_number":1,"name":"Pilot","air_date":"2008-01-20"}]}`))
		case strings.HasPrefix(r.URL.Path, "/tv/"):
			_, _ = w.Write([]byte(`{"id":1396,"name":"Breaking Bad","status":"Ended","first_air_date":"2008-01-20",
				"seasons":[{"season_number":1,"episode_count":1,"air_date":"2008-01-20"}],"external_ids":{"tvdb_id":81189}}`))
		case r.URL.Path == "/search/tv":
			_, _ = w.Write([]byte(`{"results":[{"id":1396,"name":"Breaking Bad","first_air_date":"2008-01-20"}]}`))
		case strings.HasPrefix(r.URL.Path, "/movie/"):
			_, _ = w.Write([]byte(`{"id":603,"title":"The Matrix","imdb_id":"tt0133093","status":"Released","release_date":"1999-03-31"}`))
		case r.URL.Path == "/search/movie":
			_, _ = w.Write([]byte(`{"results":[{"id":603,"title":"The Matrix","release_date":"1999-03-31"}]}`))
		default:
			t.Errorf("unexpected tmdb path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	set, err := settings.New(ctx, st, &config.Config{MovieLibraryRoot: "/data/movies", TVLibraryRoot: "/data/tv"})
	if err != nil {
		t.Fatal(err)
	}
	_ = set.Set(ctx, settings.KeyTMDBBaseURL, srv.URL)
	cat := New(st, set)

	// Movie folder → creates the movie, returns ("movie", id).
	mt, ref, err := cat.ResolveAdopt(ctx, "The.Matrix.1999.1080p.BluRay.x264-GRP", "")
	if err != nil || mt != "movie" || ref == 0 {
		t.Fatalf("adopt movie: mt=%q ref=%d err=%v", mt, ref, err)
	}
	if m, _ := st.GetMovieByTMDB(ctx, 603); m == nil {
		t.Error("movie should be cataloged by adopt")
	}
	// Re-adopt the same → ErrAlreadyExists handled → still resolves to the row.
	if mt2, ref2, err2 := cat.ResolveAdopt(ctx, "The.Matrix.1999.2160p.WEB", ""); err2 != nil || mt2 != "movie" || ref2 != ref {
		t.Fatalf("re-adopt should reuse existing: mt=%q ref=%d err=%v", mt2, ref2, err2)
	}

	// Series folder (has SxxEyy) → creates the series, returns ("series", id).
	st2, err := st.GetSeriesByTMDB(ctx, 1396)
	if err == nil && st2 != nil {
		t.Fatal("series should not exist yet")
	}
	mt, ref, err = cat.ResolveAdopt(ctx, "Breaking.Bad.S01E01.1080p.WEB-DL", "")
	if err != nil || mt != "series" || ref == 0 {
		t.Fatalf("adopt series: mt=%q ref=%d err=%v", mt, ref, err)
	}
	sr, _ := st.GetSeriesByTMDB(ctx, 1396)
	if sr == nil {
		t.Fatal("series should be cataloged by adopt")
	}
	// The adopted season's episode rows must exist (so the importer can match).
	if eps, _ := st.ListEpisodes(ctx, sr.ID); len(eps) == 0 {
		t.Error("adopt must ensure the adopted season's episodes exist")
	}

	// Re-adopt an existing series (ErrAlreadyExists path) still re-syncs episodes
	// and resolves to the same series id.
	mt3, ref3, err3 := cat.ResolveAdopt(ctx, "Breaking.Bad.S01E01.2160p.WEB", "")
	if err3 != nil || mt3 != "series" || ref3 != sr.ID {
		t.Fatalf("re-adopt existing series: mt=%q ref=%d err=%v", mt3, ref3, err3)
	}
}
