package store

import (
	"context"
	"testing"

	"github.com/radaiko/boxarr/internal/notify"
)

func TestNotificationsQueueAndRead(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if _, err := st.EnqueueNotification(ctx, &notify.Notification{Type: "grab_failed", Payload: `{"x":1}`}); err != nil {
		t.Fatalf("EnqueueNotification: %v", err)
	}
	if _, err := st.EnqueueNotification(ctx, &notify.Notification{Type: "download_completed"}); err != nil {
		t.Fatalf("EnqueueNotification (default payload): %v", err)
	}
	if n, _ := st.UnreadCount(ctx); n != 2 {
		t.Fatalf("UnreadCount = %d, want 2", n)
	}
	list, err := st.ListNotifications(ctx, true, 50)
	if err != nil {
		t.Fatalf("ListNotifications: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 unread, got %d", len(list))
	}
	// Newest-first: the download_completed (inserted last) comes first.
	if list[0].Type != "download_completed" {
		t.Errorf("expected newest-first ordering, got %s first", list[0].Type)
	}
	if list[1].Payload != `{"x":1}` {
		t.Errorf("payload roundtrip: %q", list[1].Payload)
	}
	if err := st.MarkNotificationRead(ctx, list[0].ID); err != nil {
		t.Fatalf("MarkNotificationRead: %v", err)
	}
	if n, _ := st.UnreadCount(ctx); n != 1 {
		t.Fatalf("UnreadCount after one read = %d, want 1", n)
	}
	if err := st.MarkAllNotificationsRead(ctx); err != nil {
		t.Fatalf("MarkAllNotificationsRead: %v", err)
	}
	if n, _ := st.UnreadCount(ctx); n != 0 {
		t.Fatalf("UnreadCount after read-all = %d, want 0", n)
	}
}
