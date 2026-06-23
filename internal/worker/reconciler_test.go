package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/torbox"
)

func TestReconcileUpsertsAndFlagsUnknown(t *testing.T) {
	fake := &fakeTorBox{}
	w, st, cfg := testWorkers(t, fake)
	cfg.WebDAVUsenetSubpath = "" // usenet+torrent collapse to one flat mount root (the default)
	ctx := context.Background()

	// A known release (matched to a completed job) and an unknown one.
	known := "Known.Movie.2024.1080p"
	knownDir := filepath.Join(cfg.UsenetPath(), known)
	os.MkdirAll(knownDir, 0o755)
	os.WriteFile(filepath.Join(knownDir, "f.mkv"), []byte("12345"), 0o644)
	id, _ := st.CreateJob(ctx, &job.Job{State: job.StateImported, Category: "movie", NZBName: known, MediaType: "movie"})
	j, _ := st.GetJob(ctx, id)
	j.StoragePath = knownDir
	st.UpdateJob(ctx, j)

	unknown := "Some.Random.Pack.2023.1080p"
	os.MkdirAll(filepath.Join(cfg.UsenetPath(), unknown), 0o755)
	os.WriteFile(filepath.Join(cfg.UsenetPath(), unknown, "x.mkv"), []byte("xy"), 0o644)

	if err := w.reconcileOnce(ctx); err != nil {
		t.Fatalf("reconcileOnce: %v", err)
	}

	items, _ := st.ListWebDAVItems(ctx)
	if len(items) != 2 {
		t.Fatalf("expected 2 webdav items, got %d", len(items))
	}
	byName := map[string]bool{} // name -> known
	for _, it := range items {
		byName[it.Name] = it.Known
	}
	if !byName[known] {
		t.Error("known release should be marked known")
	}
	if byName[unknown] {
		t.Error("unknown release should be marked unknown")
	}

	// The unknown item raised exactly one unknown_content notification.
	notes, _ := st.ListNotifications(ctx, false, 50)
	unknownNotes := 0
	for _, n := range notes {
		if n.Type == "unknown_content" {
			unknownNotes++
		}
	}
	if unknownNotes != 1 {
		t.Fatalf("expected 1 unknown_content notification, got %d", unknownNotes)
	}

	// A second sweep must not double-notify.
	if err := w.reconcileOnce(ctx); err != nil {
		t.Fatalf("reconcileOnce 2: %v", err)
	}
	notes, _ = st.ListNotifications(ctx, false, 50)
	unknownNotes = 0
	for _, n := range notes {
		if n.Type == "unknown_content" {
			unknownNotes++
		}
	}
	if unknownNotes != 1 {
		t.Errorf("reconcile must not re-notify a known-unknown item, got %d", unknownNotes)
	}
}

func TestGrabFailedNotifiesAndMarksFailed(t *testing.T) {
	fake := &fakeTorBox{}
	w, st, _ := testWorkers(t, fake)
	ctx := context.Background()

	// A movie with an in-flight torrent job that TorBox reports failed.
	mid, _ := st.CreateMovie(ctx, &media.Movie{TMDBID: 7, Title: "M", Monitored: true, Status: media.MediaSearching})
	id, _ := st.CreateJob(ctx, &job.Job{State: job.StateDownloading, Category: "movie", NZBName: "M",
		Protocol: "torrent", MediaType: "movie", MediaRef: mid})
	jj, _ := st.GetJob(ctx, id)
	jj.TorBoxID = 42
	st.UpdateJob(ctx, jj)
	fake.torrentList = []torbox.TorrentDownload{{ID: 42, Name: "M", DownloadState: "stalled (no seeds)"}}

	if err := w.pollTorrentsOnce(ctx); err != nil {
		t.Fatalf("pollTorrentsOnce: %v", err)
	}
	got, _ := st.GetJob(ctx, id)
	if got.State != job.StateFailed {
		t.Fatalf("job should be failed, got %s", got.State)
	}
	if m, _ := st.GetMovie(ctx, mid); m.Status != media.MediaFailed {
		t.Errorf("failed grab should mark movie failed (not silently retry), got %s", m.Status)
	}
	notes, _ := st.ListNotifications(ctx, false, 50)
	found := false
	for _, n := range notes {
		if n.Type == "grab_failed" {
			found = true
		}
	}
	if !found {
		t.Error("a failed grab should enqueue a grab_failed notification")
	}
}
