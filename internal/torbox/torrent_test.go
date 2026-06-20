package torbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewWithBaseURL("tok", srv.URL)
}

func TestCreateTorrentMagnet(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/torrents/createtorrent" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("auth header: %q", got)
		}
		_ = r.ParseMultipartForm(1 << 20)
		if r.FormValue("magnet") == "" {
			t.Error("magnet field not sent")
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"torrent_id":"12345","hash":"abc"}}`))
	})
	res, err := c.CreateTorrent(context.Background(), TorrentCreateRequest{Magnet: "magnet:?xt=urn:btih:abc"})
	if err != nil {
		t.Fatalf("CreateTorrent: %v", err)
	}
	if res.TorrentID != 12345 || res.Hash != "abc" { // FlexInt decodes the quoted id
		t.Fatalf("result: %+v", res)
	}
}

func TestListTorrents(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":[
			{"id":7,"hash":"h","name":"Movie.2024","size":1073741824,
			 "download_state":"uploading","download_finished":true,"download_present":true,
			 "progress":1.0,"seeds":42,"eta":0}]}`))
	})
	list, err := c.ListTorrents(context.Background())
	if err != nil {
		t.Fatalf("ListTorrents: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1, got %d", len(list))
	}
	d := list[0]
	if d.ID != 7 || d.Name != "Movie.2024" || d.Seeds != 42 || !d.DownloadFinished || !d.DownloadPresent {
		t.Fatalf("decoded wrong: %+v", d)
	}
	if d.ProgressPct() != 100 {
		t.Errorf("ProgressPct = %d, want 100", d.ProgressPct())
	}
	if d.Failed() {
		t.Error("uploading torrent must not be Failed()")
	}
}

func TestTorrentFailedStalled(t *testing.T) {
	if !(TorrentDownload{DownloadState: "stalled (no seeds)"}).Failed() {
		t.Error("'stalled (no seeds)' must be Failed()")
	}
	if !(TorrentDownload{DownloadState: "failed (something)"}).Failed() {
		t.Error("'failed (...)' must be Failed()")
	}
}

func TestControlTorrent(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/torrents/controltorrent" {
			t.Errorf("path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"success":true,"data":null}`))
	})
	if err := c.ControlTorrent(context.Background(), 7, "delete"); err != nil {
		t.Fatalf("ControlTorrent: %v", err)
	}
}

func TestCheckCachedList(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("format"); got != "list" {
			t.Errorf("format = %q, want list", got)
		}
		_, _ = w.Write([]byte(`{"success":true,"data":[{"name":"X","size":100,"hash":"abc"}]}`))
	})
	got, err := c.CheckCached(context.Background(), []string{"abc", "def"})
	if err != nil {
		t.Fatalf("CheckCached: %v", err)
	}
	if len(got) != 1 || got[0].Hash != "abc" {
		t.Fatalf("checkcached: %+v", got)
	}
}

func TestCheckCachedObjectFallback(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"abc":{"name":"X","size":100}}}`))
	})
	got, err := c.CheckCached(context.Background(), []string{"abc"})
	if err != nil {
		t.Fatalf("CheckCached: %v", err)
	}
	if len(got) != 1 || got[0].Hash != "abc" { // hash backfilled from the map key
		t.Fatalf("object fallback: %+v", got)
	}
}

func TestUserMe(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/me" {
			t.Errorf("path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"id":42,"plan":2,"is_subscribed":true,
			"cooldown_until":"2026-06-21T00:00:00Z","total_downloaded":10737418240}}`))
	})
	u, err := c.UserMe(context.Background())
	if err != nil {
		t.Fatalf("UserMe: %v", err)
	}
	if u.Plan != 2 || !u.IsSubscribed || u.TotalDownloaded != 10737418240 {
		t.Fatalf("user: %+v", u)
	}
}
