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
	"github.com/radaiko/boxarr/internal/webdav"
)

func TestStorageAndWebDAV(t *testing.T) {
	torboxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"plan":2,"is_subscribed":true,"total_downloaded":500}}`))
	}))
	defer torboxSrv.Close()

	st := mkStore(t)
	ctx := context.Background()
	set := mkSettings(t, st, &config.Config{})
	_ = set.Set(ctx, settings.KeyTorBoxToken, "tok")
	_ = set.Set(ctx, settings.KeyTorBoxBaseURL, torboxSrv.URL)
	_ = st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{Name: "M", RemotePath: "/mnt/x", Size: 1000, Category: "movie", Known: true})
	_ = st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{Name: "U", RemotePath: "/mnt/y", Size: 500, Category: "unknown"})

	h := NewHandler(Deps{
		Store: st, Settings: set,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}).Router()

	// Storage.
	rec := req(t, h, http.MethodGet, "/storage", "", "127.0.0.1:1", "")
	var s struct {
		UsedBytes int64 `json:"usedBytes"`
		Plan      struct {
			Tier            int `json:"tier"`
			ConcurrentSlots int `json:"concurrentSlots"`
		} `json:"plan"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &s)
	if s.UsedBytes != 1500 {
		t.Errorf("usedBytes = %d, want 1500", s.UsedBytes)
	}
	if s.Plan.Tier != 2 || s.Plan.ConcurrentSlots != 10 {
		t.Errorf("plan derived wrong: %+v", s.Plan)
	}

	// WebDAV list + category filter.
	rec = req(t, h, http.MethodGet, "/webdav", "", "127.0.0.1:1", "")
	var all struct {
		Total int `json:"total"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &all)
	if all.Total != 2 {
		t.Errorf("webdav total = %d, want 2", all.Total)
	}
	rec = req(t, h, http.MethodGet, "/webdav?category=movie", "", "127.0.0.1:1", "")
	var movies struct {
		Items []webdavItemDTO `json:"items"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &movies)
	if len(movies.Items) != 1 || movies.Items[0].Category != "movie" {
		t.Errorf("category filter: %+v", movies.Items)
	}
}
