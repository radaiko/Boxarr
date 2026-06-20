package prowlarr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestSearchBuildsQueryAndParses(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			t.Errorf("path: %s", r.URL.Path)
		}
		if r.Header.Get("X-Api-Key") != "k" {
			t.Errorf("api key header missing: %q", r.Header.Get("X-Api-Key"))
		}
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`[
			{"title":"Movie.2024.1080p","indexer":"X","indexerId":3,"size":1073741824,
			 "protocol":"torrent","magnetUrl":"magnet:?x","seeders":42,"leechers":2,
			 "categories":[{"id":2000,"name":"Movies","subCategories":[]}],"guid":"g1"}]`))
	}))
	defer srv.Close()

	c := New(srv.URL, "k")
	rels, err := c.Search(context.Background(), SearchParams{Query: "Oppenheimer", Type: "movie", Categories: []int{2000}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(rels) != 1 || rels[0].Protocol != "torrent" || rels[0].Seeders != 42 || rels[0].IndexerID != 3 {
		t.Fatalf("parsed wrong: %+v", rels)
	}
	// Repeated-key encoding + default indexerIds=-1.
	q, _ := url.ParseQuery(gotQuery)
	if q.Get("type") != "movie" || q.Get("categories") != "2000" {
		t.Errorf("query: %s", gotQuery)
	}
	if got := q["indexerIds"]; len(got) != 1 || got[0] != "-1" {
		t.Errorf("indexerIds default should be -1, got %v", got)
	}
}

func TestSearchEmptyIsNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	rels, err := New(srv.URL, "k").Search(context.Background(), SearchParams{Query: "x"})
	if err != nil {
		t.Fatalf("empty results should not error: %v", err)
	}
	if len(rels) != 0 {
		t.Fatalf("want 0 results, got %d", len(rels))
	}
}

func TestIndexers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":1,"name":"X","enable":true,"protocol":"torrent"}]`))
	}))
	defer srv.Close()
	ix, err := New(srv.URL, "k").Indexers(context.Background())
	if err != nil {
		t.Fatalf("Indexers: %v", err)
	}
	if len(ix) != 1 || ix[0].Name != "X" || !ix[0].Enable {
		t.Fatalf("parsed wrong: %+v", ix)
	}
}

func TestNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "bad").Indexers(context.Background()); err == nil {
		t.Fatal("401 should be an error")
	}
}
