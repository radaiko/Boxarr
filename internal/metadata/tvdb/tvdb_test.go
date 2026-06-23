package tvdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginAndSeriesExtended(t *testing.T) {
	var logins int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/login" && r.Method == http.MethodPost:
			logins++
			_, _ = w.Write([]byte(`{"status":"success","data":{"token":"faketoken"}}`))
		case strings.HasSuffix(r.URL.Path, "/extended"):
			if r.Header.Get("Authorization") != "Bearer faketoken" {
				t.Errorf("auth: %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"status":"success","data":{"id":1234567,"name":"Breaking Bad",
				"defaultSeasonType":1,"remoteIds":[{"id":"1396","type":12,"sourceName":"TheMovieDB.com"}]}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := NewWithBaseURL("k", "", srv.URL)
	se, err := c.SeriesExtended(context.Background(), 1234567)
	if err != nil {
		t.Fatalf("SeriesExtended: %v", err)
	}
	if se.Name != "Breaking Bad" || len(se.RemoteIDs) != 1 || se.RemoteIDs[0].SourceName != "TheMovieDB.com" {
		t.Fatalf("series: %+v", se)
	}
	// A second call reuses the token (no second login).
	if _, err := c.SeriesExtended(context.Background(), 1234567); err != nil {
		t.Fatalf("second SeriesExtended: %v", err)
	}
	if logins != 1 {
		t.Errorf("token should be cached, logins=%d", logins)
	}
}

func TestEpisodesPagination(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/login":
			_, _ = w.Write([]byte(`{"status":"success","data":{"token":"t"}}`))
		case strings.Contains(r.URL.Path, "/episodes/"):
			if r.URL.Query().Get("page") == "1" {
				_, _ = w.Write([]byte(`{"status":"success","data":{"episodes":[
					{"id":2,"number":2,"seasonNumber":1,"aired":"2008-01-27"}]},"links":{"next":""}}`))
				return
			}
			// page 0: one episode + a next link to page 1
			next := fmt.Sprintf("%s/series/1/episodes/official?page=1", srv.URL)
			_, _ = fmt.Fprintf(w, `{"status":"success","data":{"episodes":[
				{"id":1,"number":1,"seasonNumber":1,"absoluteNumber":1,"aired":"2008-01-20"}]},"links":{"next":%q}}`, next)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	eps, err := NewWithBaseURL("k", "", srv.URL).Episodes(context.Background(), 1, "official")
	if err != nil {
		t.Fatalf("Episodes: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("pagination should yield 2 episodes, got %d", len(eps))
	}
	if eps[0].AbsoluteNumber == nil || *eps[0].AbsoluteNumber != 1 {
		t.Errorf("absoluteNumber not decoded: %+v", eps[0])
	}
	if eps[1].Number != 2 {
		t.Errorf("page 2 episode wrong: %+v", eps[1])
	}
}

func TestLoginOmitsEmptyPin(t *testing.T) {
	check := func(pin string, wantPin bool) {
		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/login" {
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				_, _ = w.Write([]byte(`{"status":"success","data":{"token":"t"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"status":"success","data":{"id":1,"name":"X"}}`))
		}))
		defer srv.Close()
		if _, err := NewWithBaseURL("k", pin, srv.URL).SeriesExtended(context.Background(), 1); err != nil {
			t.Fatalf("SeriesExtended(pin=%q): %v", pin, err)
		}
		if gotBody["apikey"] != "k" {
			t.Errorf("apikey not sent: %v", gotBody)
		}
		_, has := gotBody["pin"]
		if has != wantPin {
			t.Errorf("pin=%q: body has pin=%v, want %v (body=%v)", pin, has, wantPin, gotBody)
		}
	}
	check("", false)    // legacy / negotiated key — apikey only
	check("1234", true) // user-supported key — apikey + pin
}
