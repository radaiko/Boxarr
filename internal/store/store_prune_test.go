package store

import (
	"context"
	"testing"

	"github.com/radaiko/boxarr/internal/webdav"
)

func TestPruneStaleWebDAVItems(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	// A current (seen-now) broken item and a fresh tracked item should survive.
	_ = st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{Name: "fresh", RemotePath: "/m/fresh", IsBroken: true})
	_ = st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{Name: "live", RemotePath: "/m/live"})
	// An old broken item (last_seen backdated 30 days) should be pruned.
	_ = st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{Name: "old", RemotePath: "/m/old", IsBroken: true})
	if _, err := st.db.ExecContext(ctx,
		`UPDATE webdav_item SET is_broken=1, last_seen=datetime('now','-30 days') WHERE remote_path='/m/old'`); err != nil {
		t.Fatal(err)
	}
	n, err := st.PruneStaleWebDAVItems(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pruned %d, want 1", n)
	}
	items, _ := st.ListWebDAVItems(ctx)
	got := map[string]bool{}
	for _, it := range items {
		got[it.Name] = true
	}
	if got["old"] {
		t.Error("old broken item should have been pruned")
	}
	if !got["live"] {
		t.Error("live item should survive")
	}
}
