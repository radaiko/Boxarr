package v1

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/notify"
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
