package plex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSections(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/library/sections" {
			t.Errorf("path: %s", r.URL.Path)
		}
		if r.Header.Get("X-Plex-Token") != "tok" {
			t.Errorf("token header: %q", r.Header.Get("X-Plex-Token"))
		}
		_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[
			{"key":"1","type":"movie","title":"Movies","Location":[{"id":1,"path":"/data/movies"}]},
			{"key":"2","type":"show","title":"TV","Location":[{"id":2,"path":"/data/tv"}]}]}}`))
	}))
	defer srv.Close()
	secs, err := New(srv.URL, "tok").Sections(context.Background())
	if err != nil {
		t.Fatalf("Sections: %v", err)
	}
	if len(secs) != 2 || secs[0].Type != "movie" || secs[1].Locations[0].Path != "/data/tv" {
		t.Fatalf("sections: %+v", secs)
	}
}

func TestScanPathEncodesPath(t *testing.T) {
	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK) // async, empty body
	}))
	defer srv.Close()
	err := New(srv.URL, "tok").ScanPath(context.Background(), "2", "/data/tv/Breaking Bad (2008)/Season 01")
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	// quote_plus: spaces -> '+', '(' -> %28
	if want := "path=%2Fdata%2Ftv%2FBreaking+Bad+%282008%29%2FSeason+01"; gotRawQuery != want {
		t.Errorf("query = %q, want %q", gotRawQuery, want)
	}
}

func TestScanSectionAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("full scan should have no query, got %q", r.URL.RawQuery)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if err := New(srv.URL, "tok").ScanSection(context.Background(), "2"); err != nil {
		t.Fatalf("ScanSection: %v", err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer bad.Close()
	if err := New(bad.URL, "bad").ScanSection(context.Background(), "2"); err == nil {
		t.Fatal("401 should be an error")
	}
}
