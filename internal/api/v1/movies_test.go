package v1

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/radaiko/boxarr/internal/catalog"
	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/metadata/tmdb"
	"github.com/radaiko/boxarr/internal/store"
)

// fakeTMDB serves the minimal TMDB endpoints catalog ingest uses.
func fakeTMDB(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/movie/"):
			_, _ = w.Write([]byte(`{"id":603,"title":"The Matrix","imdb_id":"tt0133093",
				"status":"Released","release_date":"1999-03-31","runtime":136,"poster_path":"/m.jpg"}`))
		case r.URL.Path == "/search/movie":
			_, _ = w.Write([]byte(`{"results":[{"id":603,"title":"The Matrix","release_date":"1999-03-31"}]}`))
		default:
			t.Errorf("unexpected tmdb path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newV1Cat(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := &config.Config{MovieLibraryRoot: "/data/movies"}
	cat := catalog.New(st, tmdb.NewWithBaseURL("tok", fakeTMDB(t).URL), cfg)
	h := NewHandler(Deps{
		Store: st, Cfg: cfg, Catalog: cat,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "test",
	})
	return h.Router(), st
}

func TestMovieAddListGetDelete(t *testing.T) {
	h, _ := newV1Cat(t)

	// Add.
	rec := req(t, h, http.MethodPost, "/movies", "", "127.0.0.1:1", `{"tmdbId":603}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add: %d body=%s", rec.Code, rec.Body.String())
	}
	var added movieDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &added)
	if added.Title != "The Matrix" || added.Year != 1999 || added.Status != "wanted" {
		t.Fatalf("added movie: %+v", added)
	}

	// Duplicate -> 409.
	if rec := req(t, h, http.MethodPost, "/movies", "", "127.0.0.1:1", `{"tmdbId":603}`); rec.Code != http.StatusConflict {
		t.Errorf("duplicate add should 409, got %d", rec.Code)
	}

	// List.
	rec = req(t, h, http.MethodGet, "/movies", "", "127.0.0.1:1", "")
	var list struct {
		Items []movieDTO `json:"items"`
		Total int        `json:"total"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if list.Total != 1 {
		t.Fatalf("list total = %d, want 1", list.Total)
	}

	// Get + monitored toggle.
	if rec := req(t, h, http.MethodGet, "/movies/"+itoa(added.ID), "", "127.0.0.1:1", ""); rec.Code != http.StatusOK {
		t.Errorf("get: %d", rec.Code)
	}
	if rec := req(t, h, http.MethodPut, "/movies/"+itoa(added.ID)+"/monitored", "", "127.0.0.1:1", `{"monitored":false}`); rec.Code != http.StatusOK {
		t.Errorf("monitored toggle: %d", rec.Code)
	}

	// Delete -> 204, then 404.
	if rec := req(t, h, http.MethodDelete, "/movies/"+itoa(added.ID), "", "127.0.0.1:1", ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d", rec.Code)
	}
	if rec := req(t, h, http.MethodGet, "/movies/"+itoa(added.ID), "", "127.0.0.1:1", ""); rec.Code != http.StatusNotFound {
		t.Errorf("get after delete should 404, got %d", rec.Code)
	}
}

func TestMovieLookup(t *testing.T) {
	h, _ := newV1Cat(t)
	rec := req(t, h, http.MethodGet, "/movies/lookup?term=matrix", "", "127.0.0.1:1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("lookup: %d", rec.Code)
	}
	var got struct {
		Items []catalog.MovieCandidate `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Items) != 1 || got.Items[0].TMDBID != 603 {
		t.Fatalf("lookup items: %+v", got.Items)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
