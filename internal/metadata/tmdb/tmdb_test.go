package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfigurationAndImageURL(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth: %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"images":{"secure_base_url":"https://image.tmdb.org/t/p/",
			"poster_sizes":["w500","original"]}}`))
	}))
	defer srv.Close()
	c := NewWithBaseURL("tok", srv.URL)
	if _, err := c.Configuration(context.Background()); err != nil {
		t.Fatalf("Configuration: %v", err)
	}
	if _, err := c.Configuration(context.Background()); err != nil { // cached
		t.Fatalf("Configuration cached: %v", err)
	}
	if hits != 1 {
		t.Errorf("Configuration should be cached after first fetch, hits=%d", hits)
	}
	if got := c.ImageURL("w500", "/poster.jpg"); got != "https://image.tmdb.org/t/p/w500/poster.jpg" {
		t.Errorf("ImageURL = %q", got)
	}
}

func TestFindByTVDB(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("external_source") != "tvdb_id" {
			t.Errorf("external_source: %q", r.URL.Query().Get("external_source"))
		}
		_, _ = w.Write([]byte(`{"movie_results":[],"tv_results":[{"id":1399,"name":"GoT","first_air_date":"2011-04-17"}]}`))
	}))
	defer srv.Close()
	res, err := NewWithBaseURL("tok", srv.URL).FindByTVDB(context.Background(), 121361)
	if err != nil {
		t.Fatalf("FindByTVDB: %v", err)
	}
	if len(res.TVResults) != 1 || res.TVResults[0].ID != 1399 {
		t.Fatalf("find: %+v", res)
	}
}

func TestTVDetailsWithExternalIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("append_to_response") != "external_ids" {
			t.Errorf("append: %q", r.URL.Query().Get("append_to_response"))
		}
		_, _ = w.Write([]byte(`{"id":1399,"name":"GoT","status":"Ended","first_air_date":"2011-04-17",
			"number_of_seasons":8,"seasons":[{"season_number":1,"episode_count":10,"air_date":"2011-04-17"}],
			"external_ids":{"tvdb_id":121361,"imdb_id":"tt0944947"}}`))
	}))
	defer srv.Close()
	d, err := NewWithBaseURL("tok", srv.URL).TVDetails(context.Background(), 1399)
	if err != nil {
		t.Fatalf("TVDetails: %v", err)
	}
	if d.Status != "Ended" || d.ExternalIDs.TVDBID != 121361 || len(d.Seasons) != 1 || d.Seasons[0].EpisodeCount != 10 {
		t.Fatalf("details: %+v", d)
	}
}

func TestSearchMovie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("primary_release_year") != "1999" {
			t.Errorf("year: %q", r.URL.Query().Get("primary_release_year"))
		}
		_, _ = w.Write([]byte(`{"results":[{"id":603,"title":"The Matrix","release_date":"1999-03-31"}]}`))
	}))
	defer srv.Close()
	res, err := NewWithBaseURL("tok", srv.URL).SearchMovie(context.Background(), "Matrix", 1999)
	if err != nil {
		t.Fatalf("SearchMovie: %v", err)
	}
	if len(res) != 1 || res[0].ID != 603 {
		t.Fatalf("search: %+v", res)
	}
}

func TestIsV3Key(t *testing.T) {
	if !isV3Key("7f8450aa1f71e3e39ee6303fcff42827") {
		t.Error("32-hex should be detected as a v3 API key")
	}
	if isV3Key("eyJhbGciOiJIUzI1NiJ9.eyJhdWQiOiJ4In0.sig") {
		t.Error("a v4 JWT must NOT be detected as a v3 key")
	}
	if isV3Key("") || isV3Key("short") {
		t.Error("non-32-char strings are not v3 keys")
	}
}
