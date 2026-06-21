package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A path deleted on TorBox must not be re-added by the reconciler while the
// rclone cache still lists the folder; once the folder disappears, the tombstone
// is garbage-collected.
func TestReconcileSkipsTombstonedPath(t *testing.T) {
	w, st, cfg := testWorkers(t, &fakeTorBox{})
	cfg.WebDAVUsenetSubpath = ""
	ctx := context.Background()

	dir := filepath.Join(cfg.UsenetPath(), "Ghost.Release.2024.1080p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := st.AddDeletedPath(ctx, dir); err != nil {
		t.Fatal(err)
	}

	if err := w.reconcileOnce(ctx); err != nil {
		t.Fatalf("reconcileOnce: %v", err)
	}
	items, _ := st.ListWebDAVItems(ctx)
	for _, it := range items {
		if it.RemotePath == dir {
			t.Fatal("tombstoned path was re-added to the WebDAV list")
		}
	}
	// Folder still present → tombstone retained.
	if tombs, _ := st.ListDeletedPaths(ctx); !tombs[dir] {
		t.Fatal("tombstone cleared while the folder is still listed")
	}

	// Folder gone from the mount → next sweep GCs the tombstone.
	os.RemoveAll(dir)
	if err := w.reconcileOnce(ctx); err != nil {
		t.Fatalf("reconcileOnce 2: %v", err)
	}
	if tombs, _ := st.ListDeletedPaths(ctx); tombs[dir] {
		t.Fatal("tombstone not garbage-collected after the folder disappeared")
	}
}
