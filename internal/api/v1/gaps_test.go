package v1

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/notify"
	"github.com/radaiko/boxarr/internal/settings"
	"github.com/radaiko/boxarr/internal/webdav"
)

func TestUnknownContentIgnoreAction(t *testing.T) {
	st := mkStore(t)
	ctx := context.Background()
	set := mkSettings(t, st, &config.Config{})
	// An unknown mount item + its notification.
	_ = st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{Name: "Junk", RemotePath: "/mnt/junk", Category: "unknown"})
	nid, _ := st.EnqueueNotification(ctx, &notify.Notification{
		Type: "unknown_content", Payload: `{"name":"Junk","remotePath":"/mnt/junk"}`,
	})
	h := NewHandler(Deps{Store: st, Settings: set,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).Router()

	rec := req(t, h, http.MethodPost, "/notifications/"+itoa(nid)+"/action", "", "127.0.0.1:1", `{"action":"ignore"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("ignore action: %d body=%s", rec.Code, rec.Body.String())
	}
	// Item is now known; notification read.
	it, _ := st.GetWebDAVItemByPath(ctx, "/mnt/junk")
	if !it.Known {
		t.Error("ignore should mark the item known")
	}
	if n, _ := st.UnreadCount(ctx); n != 0 {
		t.Errorf("notification should be read, unread=%d", n)
	}
}

type fakeAdopter struct {
	called bool
	path   string
	name   string
}

func (f *fakeAdopter) AdoptUnknown(_ context.Context, remotePath, name string) error {
	f.called, f.path, f.name = true, remotePath, name
	return nil
}

func TestUnknownContentAdoptAction(t *testing.T) {
	st := mkStore(t)
	ctx := context.Background()
	set := mkSettings(t, st, &config.Config{})
	_ = st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{Name: "Movie.2024", RemotePath: "/mnt/m", Category: "movie"})
	nid, _ := st.EnqueueNotification(ctx, &notify.Notification{
		Type: "unknown_content", Payload: `{"name":"Movie.2024","remotePath":"/mnt/m"}`,
	})
	ad := &fakeAdopter{}
	h := NewHandler(Deps{Store: st, Settings: set, Adopter: ad,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).Router()

	rec := req(t, h, http.MethodPost, "/notifications/"+itoa(nid)+"/action", "", "127.0.0.1:1", `{"action":"adopt"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("adopt action: %d body=%s", rec.Code, rec.Body.String())
	}
	if !ad.called || ad.path != "/mnt/m" || ad.name != "Movie.2024" {
		t.Fatalf("adopter not invoked correctly: %+v", ad)
	}
	if n, _ := st.UnreadCount(ctx); n != 0 {
		t.Errorf("notification should be read after adopt, unread=%d", n)
	}
}

func TestUnknownContentDeleteRemovesMountFolder(t *testing.T) {
	st := mkStore(t)
	ctx := context.Background()
	// Fake TorBox returning empty mylists (so no API match → folder-removal path).
	tb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
	}))
	defer tb.Close()

	mountRoot := t.TempDir()
	rel := filepath.Join(mountRoot, "usenet", "Chicago.Fire.S01E15.GERMAN")
	if err := os.MkdirAll(rel, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rel, "ep.mkv"), []byte("v"), 0o644); err != nil {
		t.Fatal(err)
	}

	set := mkSettings(t, st, &config.Config{})
	_ = set.Set(ctx, settings.KeyWebDAVMountRoot, mountRoot)
	_ = set.Set(ctx, settings.KeyTorBoxToken, "x")
	_ = set.Set(ctx, settings.KeyTorBoxBaseURL, tb.URL)
	_ = st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{Name: "Chicago.Fire.S01E15.GERMAN", RemotePath: rel, Category: "series"})
	nid, _ := st.EnqueueNotification(ctx, &notify.Notification{
		Type: "unknown_content", Payload: `{"name":"Chicago.Fire.S01E15.GERMAN","remotePath":"` + rel + `"}`,
	})
	h := NewHandler(Deps{Store: st, Settings: set, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).Router()

	rec := req(t, h, http.MethodPost, "/notifications/"+itoa(nid)+"/action", "", "127.0.0.1:1", `{"action":"delete"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(rel); !os.IsNotExist(err) {
		t.Error("the mount folder should be removed so it doesn't reappear")
	}
	if it, _ := st.GetWebDAVItemByPath(ctx, rel); it != nil {
		t.Error("the webdav row should be gone")
	}
}

func TestRemoveMountFolderRefusesOutsideRoot(t *testing.T) {
	st := mkStore(t)
	ctx := context.Background()
	mountRoot := t.TempDir()
	outside := t.TempDir() // a sibling dir NOT under the mount root
	keep := filepath.Join(outside, "important")
	_ = os.MkdirAll(keep, 0o755)

	set := mkSettings(t, st, &config.Config{})
	_ = set.Set(ctx, settings.KeyWebDAVMountRoot, mountRoot)
	h := NewHandler(Deps{Store: st, Settings: set, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	h.removeMountFolder(keep) // path outside the mount root
	if _, err := os.Stat(keep); err != nil {
		t.Error("removeMountFolder must refuse paths outside the mount root")
	}
}

func TestSeasonMonitorToggleCascades(t *testing.T) {
	h, st := newV1Series(t)
	ctx := context.Background()
	// Ingest Breaking Bad (fake TMDB) so seasons/episodes exist.
	rec := req(t, h, http.MethodPost, "/series", "", "127.0.0.1:1", `{"tmdbId":1396}`)
	var sd seriesDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &sd)

	// Unmonitor season 1 → its episodes go unmonitored + missing.
	rec = req(t, h, http.MethodPut, "/series/"+itoa(sd.ID)+"/seasons/1/monitored", "", "127.0.0.1:1", `{"monitored":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("season toggle: %d body=%s", rec.Code, rec.Body.String())
	}
	eps, _ := st.ListEpisodes(ctx, sd.ID)
	for _, e := range eps {
		if e.SeasonNumber == 1 && (e.Monitored || e.Status == media.MediaWanted) {
			t.Errorf("S01E%02d should be unmonitored+not-wanted, got monitored=%v status=%s",
				e.EpisodeNumber, e.Monitored, e.Status)
		}
	}
}
