package v1

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/settings"
)

func defaultSelectionCfg() *config.Config {
	return &config.Config{
		MovieLibraryRoot:                    "/data/movies",
		SelectPreferredResolutions:          []string{"2160p", "1080p", "720p"},
		SelectPreferredQualities:            []string{"WEB-DL", "BluRay"},
		SelectMinSeeders:                    1,
		SelectWeightResolution:              400,
		SelectWeightQuality:                 200,
		SelectWeightProtocolCachedTorrent:   300,
		SelectWeightProtocolUsenet:          200,
		SelectWeightProtocolUncachedTorrent: 100,
		SelectWeightHealth:                  100,
		SelectSeedSaturation:                100,
	}
}

func TestFreeSearchRanksReleases(t *testing.T) {
	prowlarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[
			{"title":"Movie.2024.1080p.WEB-DL.x264-GRP","indexer":"U","indexerId":5,"protocol":"usenet",
			 "size":1500000000,"grabs":100,"downloadUrl":"http://u/x.nzb","guid":"u1"},
			{"title":"Movie.2024.2160p.BluRay.x265-GRP","indexer":"T","indexerId":3,"protocol":"torrent",
			 "size":8000000000,"seeders":80,"leechers":3,"magnetUrl":"magnet:?xt=urn:btih:abc","infoHash":"ABC","guid":"t1"}]`))
	}))
	defer prowlarrSrv.Close()
	torboxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// "abc" is cached.
		_, _ = w.Write([]byte(`{"success":true,"data":[{"name":"X","size":1,"hash":"abc"}]}`))
	}))
	defer torboxSrv.Close()

	st := mkStore(t)
	set := mkSettings(t, st, defaultSelectionCfg())
	ctx := context.Background()
	_ = set.Set(ctx, settings.KeyProwlarrURL, prowlarrSrv.URL)
	_ = set.Set(ctx, settings.KeyProwlarrAPIKey, "k")
	_ = set.Set(ctx, settings.KeyTorBoxToken, "tok")
	_ = set.Set(ctx, settings.KeyTorBoxBaseURL, torboxSrv.URL)
	h := NewHandler(Deps{
		Store: st, Settings: set,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}).Router()

	rec := req(t, h, http.MethodGet, "/search?q=Movie+2024&type=movie", "", "127.0.0.1:1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("search: %d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Items []releaseDTO `json:"items"`
		Total int          `json:"total"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Total != 2 {
		t.Fatalf("want 2 results, got %d", got.Total)
	}
	// The cached 2160p torrent should rank first; its cached flag must be true.
	first := got.Items[0]
	if first.Protocol != "torrent" || first.Cached == nil || !*first.Cached {
		t.Fatalf("expected cached torrent ranked first, got %+v", first)
	}
	if first.ReleaseID == "" {
		t.Error("releaseId should be populated for grab")
	}
	// releaseId round-trips to the grab ref.
	g, err := decodeReleaseID(first.ReleaseID)
	if err != nil || g.Protocol != "torrent" || g.MagnetURL == "" {
		t.Fatalf("decodeReleaseID: %+v err=%v", g, err)
	}
}
