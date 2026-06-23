package store

import (
	"context"
	"testing"

	"github.com/radaiko/boxarr/internal/webdav"
)

func TestUpsertWebDAVItemStickyKnown(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	// Adopted/known (no job — adopted items have none).
	if err := st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{Name: "Anaconda", RemotePath: "/m/Anaconda", Known: true}); err != nil {
		t.Fatal(err)
	}
	// Later sweep re-sees it but matched nothing → known=false passed in.
	if err := st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{Name: "Anaconda", RemotePath: "/m/Anaconda", Known: false}); err != nil {
		t.Fatal(err)
	}
	items, _ := st.ListWebDAVItems(ctx)
	var got *webdav.WebDAVItem
	for _, it := range items {
		if it.RemotePath == "/m/Anaconda" {
			got = it
		}
	}
	if got == nil {
		t.Fatal("item missing")
	}
	if !got.Known {
		t.Error("known must be sticky (stay true) across a later unmatched sweep")
	}
	// Control: an item that was never known stays unknown.
	_ = st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{Name: "Rando", RemotePath: "/m/Rando", Known: false})
	items, _ = st.ListWebDAVItems(ctx)
	for _, it := range items {
		if it.RemotePath == "/m/Rando" && it.Known {
			t.Error("never-known item must not become known")
		}
	}
}
