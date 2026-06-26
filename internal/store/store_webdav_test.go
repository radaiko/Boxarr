package store

import (
	"context"
	"testing"

	"github.com/radaiko/boxarr/internal/webdav"
)

func TestWebDAVUpsertAndUsage(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	w := &webdav.WebDAVItem{Name: "X", RemotePath: "/mnt/torbox/X", Size: 100, Category: "movie", Known: true}
	if err := st.UpsertWebDAVItem(ctx, w); err != nil {
		t.Fatalf("UpsertWebDAVItem: %v", err)
	}
	w.Size = 250 // re-seen, larger
	if err := st.UpsertWebDAVItem(ctx, w); err != nil {
		t.Fatalf("UpsertWebDAVItem (conflict): %v", err)
	}
	items, _ := st.ListWebDAVItems(ctx)
	if len(items) != 1 {
		t.Fatalf("remote_path UNIQUE should upsert, got %d rows", len(items))
	}
	if got, _ := st.WebDAVUsageBytes(ctx); got != 250 {
		t.Fatalf("usage = %d, want 250", got)
	}

	// An unknown item shows up in the unknown list and counts toward usage.
	if err := st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{
		Name: "Y", RemotePath: "/mnt/torbox/Y", Size: 50, Category: "unknown"}); err != nil {
		t.Fatalf("UpsertWebDAVItem unknown: %v", err)
	}
	if unk, _ := st.ListUnknownWebDAVItems(ctx); len(unk) != 1 || unk[0].Name != "Y" {
		t.Fatalf("ListUnknownWebDAVItems: %+v", unk)
	}

	// Items not seen since a future sweep marker are marked broken and drop out
	// of usage. "9999-..." is safely after any real last_seen.
	n, err := st.MarkWebDAVItemsBrokenNotSeenSince(ctx, "9999-01-01 00:00:00")
	if err != nil {
		t.Fatalf("MarkWebDAVItemsBrokenNotSeenSince: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 items marked broken, got %d", n)
	}
	if got, _ := st.WebDAVUsageBytes(ctx); got != 0 {
		t.Fatalf("usage after all-broken = %d, want 0", got)
	}
}

// Regression: an item seen during the current sweep must NOT be marked broken.
// (Previously a Go local time vs SQLite UTC CURRENT_TIMESTAMP mismatch flagged
// every fresh item, hiding all mount content.)
func TestWebDAVFreshItemsNotMarkedBroken(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	sweep, err := st.DBNow(ctx) // captured at sweep start, DB clock
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{
		Name: "Fresh", RemotePath: "/mnt/torbox/Fresh", Size: 10, Category: "movie"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MarkWebDAVItemsBrokenNotSeenSince(ctx, sweep); err != nil {
		t.Fatal(err)
	}
	it, err := st.GetWebDAVItemByPath(ctx, "/mnt/torbox/Fresh")
	if err != nil {
		t.Fatal(err)
	}
	if it.IsBroken {
		t.Error("a just-seen item must not be flagged broken")
	}
}

func TestWebDAVItemNotifiedSurvivesUpsert(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	_ = st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{Name: "X", RemotePath: "/mnt/x", Category: "unknown"})
	if done, _ := st.WasWebDAVItemNotified(ctx, "/mnt/x"); done {
		t.Fatal("fresh item should not be notified")
	}
	if err := st.MarkWebDAVItemNotified(ctx, "/mnt/x"); err != nil {
		t.Fatal(err)
	}
	if done, _ := st.WasWebDAVItemNotified(ctx, "/mnt/x"); !done {
		t.Fatal("should be notified after mark")
	}
	// A later reconcile sweep re-upserts the same item — the notified flag must
	// persist so the notification isn't raised again.
	_ = st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{Name: "X", RemotePath: "/mnt/x", Category: "unknown"})
	if done, _ := st.WasWebDAVItemNotified(ctx, "/mnt/x"); !done {
		t.Error("notified flag must survive re-upsert")
	}
}
