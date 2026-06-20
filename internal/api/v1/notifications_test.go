package v1

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/notify"
	"github.com/radaiko/boxarr/internal/store"
)

func TestNotificationsListAndRead(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "n.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	_, _ = st.EnqueueNotification(ctx, &notify.Notification{Type: "grab_failed", Payload: `{"error":"stalled"}`})
	_, _ = st.EnqueueNotification(ctx, &notify.Notification{Type: "download_completed", Payload: `{"title":"X"}`})

	h := NewHandler(Deps{Store: st, Cfg: &config.Config{},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).Router()

	// List with unread badge.
	rec := req(t, h, http.MethodGet, "/notifications", "", "127.0.0.1:1", "")
	var list struct {
		Items       []notificationDTO `json:"items"`
		UnreadCount int               `json:"unreadCount"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Items) != 2 || list.UnreadCount != 2 {
		t.Fatalf("list: items=%d unread=%d", len(list.Items), list.UnreadCount)
	}
	// Newest-first; payload decoded.
	if list.Items[0].Type != "download_completed" || list.Items[1].Payload["error"] != "stalled" {
		t.Fatalf("ordering/payload wrong: %+v", list.Items)
	}
	// unread-count endpoint.
	rec = req(t, h, http.MethodGet, "/notifications/unread-count", "", "127.0.0.1:1", "")
	var uc struct {
		UnreadCount int `json:"unreadCount"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &uc)
	if uc.UnreadCount != 2 {
		t.Errorf("unread-count = %d, want 2", uc.UnreadCount)
	}
	// Mark one read.
	if rec := req(t, h, http.MethodPut, "/notifications/"+itoa(list.Items[0].ID)+"/read", "", "127.0.0.1:1", ""); rec.Code != http.StatusOK {
		t.Errorf("mark read: %d", rec.Code)
	}
	// Mark all read.
	if rec := req(t, h, http.MethodPut, "/notifications/read-all", "", "127.0.0.1:1", ""); rec.Code != http.StatusOK {
		t.Errorf("read-all: %d", rec.Code)
	}
	if n, _ := st.UnreadCount(ctx); n != 0 {
		t.Errorf("unread after read-all = %d, want 0", n)
	}
}
