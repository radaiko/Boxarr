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

func TestConnectionProbes(t *testing.T) {
	ctx := context.Background()
	st := mkStore(t)
	set := mkSettings(t, st, &config.Config{})

	// Fakes for TorBox + TMDB (via base-URL override) and Prowlarr/Plex (URL).
	torboxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"plan":2}}`))
	}))
	defer torboxSrv.Close()
	prowlarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":1,"name":"X"},{"id":2,"name":"Y"}]`))
	}))
	defer prowlarrSrv.Close()

	_ = set.Set(ctx, settings.KeyTorBoxBaseURL, torboxSrv.URL)

	h := NewHandler(Deps{Store: st, Settings: set,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).Router()

	// TorBox: token from settings, base from override → ok.
	_ = set.Set(ctx, settings.KeyTorBoxToken, "tok")
	rec := req(t, h, http.MethodPost, "/settings/test/torbox", "", "127.0.0.1:1", `{}`)
	var res struct {
		OK     bool   `json:"ok"`
		Detail string `json:"detail"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if !res.OK {
		t.Fatalf("torbox test should pass, got %s", rec.Body.String())
	}

	// Prowlarr: posted url+key (test-before-save).
	rec = req(t, h, http.MethodPost, "/settings/test/prowlarr", "", "127.0.0.1:1",
		`{"url":"`+prowlarrSrv.URL+`","apiKey":"k"}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if !res.OK || res.Detail != "2 indexers reachable" {
		t.Fatalf("prowlarr test: %s", rec.Body.String())
	}

	// Unreachable Plex → ok:false (200, not an HTTP error).
	rec = req(t, h, http.MethodPost, "/settings/test/plex", "", "127.0.0.1:1",
		`{"url":"http://127.0.0.1:1","token":"x"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("a failed probe must still be HTTP 200, got %d", rec.Code)
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.OK {
		t.Error("plex probe to a dead host should report ok:false")
	}

	// Unknown service → 400.
	if rec := req(t, h, http.MethodPost, "/settings/test/bogus", "", "127.0.0.1:1", `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown service should 400, got %d", rec.Code)
	}
}
