package store

import (
	"context"
	"testing"
	"time"

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

	// Items not seen since a future sweep are marked broken and drop out of usage.
	n, err := st.MarkWebDAVItemsBrokenNotSeenSince(ctx, time.Now().Add(time.Hour))
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
