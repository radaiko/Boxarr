package seerr

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

	"github.com/go-chi/chi/v5"
	"github.com/radaiko/boxarr/internal/catalog"
	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/settings"
	"github.com/radaiko/boxarr/internal/store"
)

func fakeTMDBBase(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/find/"):
			_, _ = w.Write([]byte(`{"tv_results":[{"id":1396,"name":"Breaking Bad","first_air_date":"2008-01-20"}]}`))
		case strings.Contains(r.URL.Path, "/season/"):
			_, _ = w.Write([]byte(`{"season_number":1,"episodes":[{"episode_number":1,"season_number":1,"name":"Pilot","air_date":"2008-01-20"}]}`))
		case strings.HasPrefix(r.URL.Path, "/tv/"):
			_, _ = w.Write([]byte(`{"id":1396,"name":"Breaking Bad","status":"Ended","first_air_date":"2008-01-20",
				"seasons":[{"season_number":1,"episode_count":1,"air_date":"2008-01-20"}],
				"external_ids":{"tvdb_id":81189,"imdb_id":"tt0903747"}}`))
		case strings.HasPrefix(r.URL.Path, "/movie/"):
			_, _ = w.Write([]byte(`{"id":603,"title":"The Matrix","imdb_id":"tt0133093","status":"Released","release_date":"1999-03-31"}`))
		default:
			t.Errorf("unexpected tmdb path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func newSurface(t *testing.T, kind Kind) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "se.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := &config.Config{SeerrAPIKeys: []string{"secret"}, TVLibraryRoot: t.TempDir(), MovieLibraryRoot: t.TempDir(), AnimeLibraryRoot: "/data/anime"}
	set, err := settings.New(context.Background(), st, cfg)
	if err != nil {
		t.Fatal(err)
	}
	_ = set.Set(context.Background(), settings.KeyTMDBBaseURL, fakeTMDBBase(t))
	cat := catalog.New(st, set)
	deps := Deps{Store: st, Settings: set, Catalog: cat, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	return NewRouter(kind, deps), st
}

func do(h http.Handler, method, path, key, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if key != "" {
		r.Header.Set("X-Api-Key", key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestAuthRequiredAndQueryParam(t *testing.T) {
	h, _ := newSurface(t, KindSonarr)
	if rec := do(h, http.MethodGet, "/system/status", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no key should 401, got %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/system/status", "wrong", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong key should 401, got %d", rec.Code)
	}
	// apikey query param is accepted.
	rec := do(h, http.MethodGet, "/system/status?apikey=secret", "", "")
	if rec.Code != http.StatusOK {
		t.Errorf("query apikey should pass, got %d", rec.Code)
	}
}

func TestSonarrTestEndpoints(t *testing.T) {
	h, _ := newSurface(t, KindSonarr)
	// system/status advertises a 3.x version.
	rec := do(h, http.MethodGet, "/system/status", "secret", "")
	var st map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if st["appName"] != "Sonarr" || !strings.HasPrefix(st["version"].(string), "3.") {
		t.Fatalf("status: %+v", st)
	}
	// Config dropdowns.
	for _, p := range []string{"/qualityprofile", "/rootfolder", "/languageprofile"} {
		rec := do(h, http.MethodGet, p, "secret", "")
		if rec.Code != http.StatusOK || rec.Body.Len() < 3 {
			t.Errorf("%s: code=%d body=%s", p, rec.Code, rec.Body.String())
		}
	}
	// Case-insensitive path (/qualityProfile).
	if rec := do(h, http.MethodGet, "/qualityProfile", "secret", ""); rec.Code != http.StatusOK {
		t.Errorf("case-insensitive route failed: %d", rec.Code)
	}
	// tag → [].
	rec = do(h, http.MethodGet, "/tag", "secret", "")
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("tag should be [], got %s", rec.Body.String())
	}
}

func TestSonarrLookupAndAdd(t *testing.T) {
	h, st := newSurface(t, KindSonarr)
	// Lookup by tvdb — no id yet (add path).
	rec := do(h, http.MethodGet, "/series/lookup?term=tvdb:81189", "secret", "")
	var look []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &look)
	if len(look) != 1 || look[0]["title"] != "Breaking Bad" {
		t.Fatalf("lookup: %s", rec.Body.String())
	}
	if _, hasID := look[0]["id"]; hasID {
		t.Error("id must be absent before the series is added (drives the POST path)")
	}
	// Add.
	rec = do(h, http.MethodPost, "/series", "secret",
		`{"tvdbId":81189,"title":"Breaking Bad","seasons":[{"seasonNumber":1,"monitored":true}],"addOptions":{"searchForMissingEpisodes":false}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add: %d body=%s", rec.Code, rec.Body.String())
	}
	var added map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &added)
	if _, ok := added["id"].(float64); !ok || added["id"].(float64) == 0 {
		t.Fatalf("response must echo a non-zero numeric id: %s", rec.Body.String())
	}
	if sr, _ := st.GetSeriesByTMDB(context.Background(), 1396); sr == nil {
		t.Error("series should be ingested into the catalog")
	}
	// Now lookup returns the id (update path).
	rec = do(h, http.MethodGet, "/series/lookup?term=tvdb:81189", "secret", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &look)
	if _, hasID := look[0]["id"]; !hasID {
		t.Error("id must be present once the series exists")
	}
}

func TestRadarrLookupAndAdd(t *testing.T) {
	h, st := newSurface(t, KindRadarr)
	// No languageprofile on Radarr is fine; status is Radarr v3.
	rec := do(h, http.MethodGet, "/system/status", "secret", "")
	var status map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &status)
	if status["appName"] != "Radarr" {
		t.Fatalf("status app: %v", status["appName"])
	}
	// Lookup + add.
	rec = do(h, http.MethodGet, "/movie/lookup?term=tmdb:603", "secret", "")
	var look []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &look)
	if len(look) != 1 || look[0]["tmdbId"].(float64) != 603 {
		t.Fatalf("movie lookup: %s", rec.Body.String())
	}
	rec = do(h, http.MethodPost, "/movie", "secret", `{"tmdbId":603,"title":"The Matrix","addOptions":{"searchForMovie":false}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add movie: %d body=%s", rec.Code, rec.Body.String())
	}
	var added map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &added)
	if added["id"].(float64) == 0 || added["monitored"] != true {
		t.Fatalf("add movie response: %s", rec.Body.String())
	}
	if m, _ := st.GetMovieByTMDB(context.Background(), 603); m == nil {
		t.Error("movie should be ingested")
	}
}

func TestCommandIsNoOp(t *testing.T) {
	h, _ := newSurface(t, KindSonarr)
	rec := do(h, http.MethodPost, "/command", "secret", `{"name":"SeriesSearch","seriesId":1}`)
	if rec.Code != http.StatusCreated {
		t.Errorf("command should 201, got %d", rec.Code)
	}
}

// TestMountedCamelCasePath reproduces the production mount (/radarr/api/v3) and the
// camelCase endpoint Overseerr actually calls (/qualityProfile). When mounted, chi
// routes by the RouteContext's RoutePath, so lowercasing only r.URL.Path isn't
// enough — this guards the "Failed to retrieve profiles: 404" regression.
func TestMountedCamelCasePath(t *testing.T) {
	h, _ := newSurface(t, KindRadarr)
	parent := chi.NewRouter()
	parent.Mount("/radarr/api/v3", h)
	for _, p := range []string{"/radarr/api/v3/qualityProfile", "/radarr/api/v3/rootFolder", "/radarr/api/v3/system/status"} {
		r := httptest.NewRequest(http.MethodGet, p, nil)
		r.Header.Set("X-Api-Key", "secret")
		rec := httptest.NewRecorder()
		parent.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Errorf("mounted %s must resolve, got %d body=%s", p, rec.Code, rec.Body.String())
		}
	}
}

func TestSonarrAnimeRootFolderAndAdd(t *testing.T) {
	h, st := newSurface(t, KindSonarr)
	// 1) The anime library root must appear in the Sonarr root-folder list so
	// Overseerr can configure an anime root folder.
	rec := do(h, http.MethodGet, "/rootfolder", "secret", "")
	if !strings.Contains(rec.Body.String(), "/data/anime") {
		t.Fatalf("anime root folder missing from /rootfolder: %s", rec.Body.String())
	}
	// 2) Adding a series with seriesType=anime ingests it as anime (anime library).
	rec = do(h, http.MethodPost, "/series", "secret",
		`{"tvdbId":81189,"title":"Breaking Bad","seriesType":"anime","rootFolderPath":"/data/anime","seasons":[{"seasonNumber":1,"monitored":true}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add anime: %d body=%s", rec.Code, rec.Body.String())
	}
	sr, _ := st.GetSeriesByTMDB(context.Background(), 1396)
	if sr == nil || sr.SeriesType != "anime" {
		t.Fatalf("series should be anime, got %+v", sr)
	}
	if sr.RootFolderPath != "/data/anime" {
		t.Errorf("anime series root = %q, want /data/anime", sr.RootFolderPath)
	}
}
