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

	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/store"
	"github.com/radaiko/boxarr/internal/torbox"
)

func newV1(t *testing.T, apiKey string, tb *torbox.Client) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "v1.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	h := NewHandler(Deps{
		Store: st, Cfg: &config.Config{APIKey: apiKey}, TorBox: tb,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Version: "test",
	})
	return h.Router(), st
}

func req(t *testing.T, h http.Handler, method, path, key, remote, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if key != "" {
		r.Header.Set("X-Api-Key", key)
	}
	if remote != "" {
		r.RemoteAddr = remote
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestAuthLoopbackBypassAndRejection(t *testing.T) {
	h, _ := newV1(t, "", nil) // no key configured
	if rec := req(t, h, http.MethodGet, "/status", "", "127.0.0.1:5555", ""); rec.Code != http.StatusOK {
		t.Errorf("loopback w/o key should be 200, got %d", rec.Code)
	}
	if rec := req(t, h, http.MethodGet, "/status", "", "192.0.2.7:5555", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("non-loopback w/o key should be 401, got %d", rec.Code)
	}
}

func TestAuthRequiresKeyWhenSet(t *testing.T) {
	h, _ := newV1(t, "secret", nil)
	if rec := req(t, h, http.MethodGet, "/status", "", "127.0.0.1:5555", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("loopback must still need the key once set, got %d", rec.Code)
	}
	if rec := req(t, h, http.MethodGet, "/status", "wrong", "127.0.0.1:5555", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong key should be 401, got %d", rec.Code)
	}
	if rec := req(t, h, http.MethodGet, "/status", "secret", "192.0.2.7:5555", ""); rec.Code != http.StatusOK {
		t.Errorf("correct key should be 200, got %d", rec.Code)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	h, _ := newV1(t, "", nil)
	rec := req(t, h, http.MethodPut, "/settings", "", "127.0.0.1:1", `{"settings":{"prowlarr.url":"http://x:9696"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT settings: %d body=%s", rec.Code, rec.Body.String())
	}
	rec = req(t, h, http.MethodGet, "/settings", "", "127.0.0.1:1", "")
	var got struct {
		Settings   map[string]string `json:"settings"`
		Configured map[string]bool   `json:"configured"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Settings["prowlarr.url"] != "http://x:9696" {
		t.Fatalf("settings did not persist: %+v", got.Settings)
	}
}

func TestStatusCounts(t *testing.T) {
	h, st := newV1(t, "", nil)
	if _, err := st.CreateMovie(context.Background(), &media.Movie{TMDBID: 1, Title: "M"}); err != nil {
		t.Fatal(err)
	}
	rec := req(t, h, http.MethodGet, "/status", "", "127.0.0.1:1", "")
	var got struct {
		Version string         `json:"version"`
		Counts  map[string]int `json:"counts"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Version != "test" || got.Counts["movies"] != 1 {
		t.Fatalf("status: %+v", got)
	}
}

func TestAccountProxiesUserMe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"plan":2,"is_subscribed":true,"total_downloaded":123}}`))
	}))
	defer srv.Close()
	h, _ := newV1(t, "", torbox.NewWithBaseURL("tok", srv.URL))
	rec := req(t, h, http.MethodGet, "/account", "", "127.0.0.1:1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("account: %d", rec.Code)
	}
	var got struct {
		Plan                   int   `json:"plan"`
		MonthlyDownloadedBytes int64 `json:"monthlyDownloadedBytes"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Plan != 2 || got.MonthlyDownloadedBytes != 123 {
		t.Fatalf("account body: %+v", got)
	}
}
