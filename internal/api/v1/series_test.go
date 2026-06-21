package v1

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radaiko/boxarr/internal/catalog"
	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/settings"
	"github.com/radaiko/boxarr/internal/store"
)

// fakeTMDBSeries serves the TV endpoints series ingest uses.
func fakeTMDBSeries(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/season/"):
			_, _ = w.Write([]byte(`{"season_number":1,"episodes":[
				{"episode_number":1,"season_number":1,"name":"Pilot","air_date":"2008-01-20"},
				{"episode_number":2,"season_number":1,"name":"Cat's in the Bag","air_date":"2099-01-01"}]}`))
		case strings.HasPrefix(r.URL.Path, "/tv/"):
			_, _ = w.Write([]byte(`{"id":1396,"name":"Breaking Bad","status":"Ended",
				"first_air_date":"2008-01-20","number_of_seasons":1,
				"seasons":[{"season_number":1,"episode_count":2,"air_date":"2008-01-20"}],
				"external_ids":{"tvdb_id":81189,"imdb_id":"tt0903747"}}`))
		case r.URL.Path == "/search/tv":
			_, _ = w.Write([]byte(`{"results":[{"id":1396,"name":"Breaking Bad","first_air_date":"2008-01-20"}]}`))
		default:
			t.Errorf("unexpected tmdb path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newV1Series(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "sr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	set := mkSettings(t, st, &config.Config{TVLibraryRoot: "/data/tv", AnimeLibraryRoot: "/data/anime"})
	_ = set.Set(context.Background(), settings.KeyTMDBBaseURL, fakeTMDBSeries(t).URL)
	cat := catalog.New(st, set)
	h := NewHandler(Deps{Store: st, Settings: set, Catalog: cat,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).Router()
	return h, st
}

func TestSeriesAddPersistsSeasonsAndEpisodes(t *testing.T) {
	h, st := newV1Series(t)
	rec := req(t, h, http.MethodPost, "/series", "", "127.0.0.1:1", `{"tmdbId":1396}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add series: %d body=%s", rec.Code, rec.Body.String())
	}
	var sd seriesDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &sd)
	if sd.Title != "Breaking Bad" || sd.TVDBID != 81189 {
		t.Fatalf("series: %+v", sd)
	}
	// Detail shows seasons + episodes with rolled-up status.
	rec = req(t, h, http.MethodGet, "/series/"+itoa(sd.ID), "", "127.0.0.1:1", "")
	var detail seriesDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	if len(detail.Seasons) != 1 || len(detail.Seasons[0].Episodes) != 2 {
		t.Fatalf("seasons/episodes: %+v", detail.Seasons)
	}
	// Only the aired episode is wanted; the future one is missing → rollup wanted.
	if detail.Status != "wanted" {
		t.Errorf("series rollup = %q, want wanted", detail.Status)
	}

	// WantedEpisodes returns only the aired one.
	wanted, _ := st.WantedEpisodes(context.Background())
	if len(wanted) != 1 || wanted[0].EpisodeNumber != 1 {
		t.Fatalf("wanted episodes: %+v", wanted)
	}

	// Duplicate add → 409.
	if rec := req(t, h, http.MethodPost, "/series", "", "127.0.0.1:1", `{"tmdbId":1396}`); rec.Code != http.StatusConflict {
		t.Errorf("duplicate add should 409, got %d", rec.Code)
	}
}

func TestAddAnimeSeriesUsesAnimeRoot(t *testing.T) {
	h, st := newV1Series(t)
	rec := req(t, h, http.MethodPost, "/series", "", "127.0.0.1:1", `{"tmdbId":1396,"seriesType":"anime"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add anime: %d body=%s", rec.Code, rec.Body.String())
	}
	var sd seriesDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &sd)
	if sd.SeriesType != "anime" {
		t.Fatalf("seriesType = %q, want anime", sd.SeriesType)
	}
	sr, _ := st.GetSeries(context.Background(), sd.ID)
	if sr.RootFolderPath != "/data/anime" {
		t.Errorf("anime series root = %q, want /data/anime", sr.RootFolderPath)
	}
}

func TestSeriesLookup(t *testing.T) {
	h, _ := newV1Series(t)
	rec := req(t, h, http.MethodGet, "/series/lookup?term=breaking", "", "127.0.0.1:1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup: %d", rec.Code)
	}
	var got struct {
		Items []catalog.SeriesCandidate `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Items) != 1 || got.Items[0].TMDBID != 1396 {
		t.Fatalf("lookup items: %+v", got.Items)
	}
}
