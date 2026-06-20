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

func TestTorrentSubmitterSubmitsPendingTorrent(t *testing.T) {
	fake := &fakeTorBox{}
	w, st, _ := testWorkers(t, fake)
	ctx := context.Background()
	id, _ := st.CreateJob(ctx, &job.Job{
		State: job.StatePending, Category: "movie", NZBName: "Rel",
		Protocol: "torrent", TorrentMagnet: "magnet:?xt=urn:btih:abc", TorrentHash: "abc",
	})
	if err := w.submitTorrentsOnce(ctx); err != nil {
		t.Fatalf("submitTorrentsOnce: %v", err)
	}
	got, _ := st.GetJob(ctx, id)
	if got.State != job.StateQueued || got.TorBoxID == 0 {
		t.Fatalf("torrent not queued: state=%s tbid=%d", got.State, got.TorBoxID)
	}
	if len(fake.createdTorrents) != 1 {
		t.Errorf("expected 1 torrent submission, got %d", len(fake.createdTorrents))
	}
}

func TestUsenetSubmitterIgnoresTorrentJobs(t *testing.T) {
	fake := &fakeTorBox{}
	w, st, _ := testWorkers(t, fake)
	ctx := context.Background()
	st.CreateJob(ctx, &job.Job{State: job.StatePending, Category: "movie", NZBName: "T",
		Protocol: "torrent", TorrentMagnet: "magnet:?x"})
	if err := w.submitOnce(ctx); err != nil {
		t.Fatalf("submitOnce: %v", err)
	}
	if len(fake.created) != 0 {
		t.Error("usenet submitter must not submit torrent jobs")
	}
}

func TestTorrentSeedingState(t *testing.T) {
	fake := &fakeTorBox{}
	w, st, _ := testWorkers(t, fake)
	ctx := context.Background()
	id, _ := st.CreateJob(ctx, &job.Job{State: job.StateDownloading, Category: "movie", NZBName: "S", Protocol: "torrent"})
	j, _ := st.GetJob(ctx, id)
	j.TorBoxID = 50
	st.UpdateJob(ctx, j)
	// uploading + not yet present -> seeding (no completion).
	fake.torrentList = []torbox.TorrentDownload{{ID: 50, Name: "S", DownloadState: "uploading", Progress: 1, Size: 10}}
	if err := w.pollTorrentsOnce(ctx); err != nil {
		t.Fatalf("pollTorrentsOnce: %v", err)
	}
	got, _ := st.GetJob(ctx, id)
	if got.State != job.StateSeeding {
		t.Fatalf("state: got %s want seeding", got.State)
	}
}

func TestTorrentImportMovieEndToEnd(t *testing.T) {
	fake := &fakeTorBox{}
	w, st, cfg := testWorkers(t, fake)
	libRoot := t.TempDir()
	cfg.MovieLibraryRoot = libRoot
	ctx := context.Background()

	// Catalog movie + a linked pending->queued torrent job.
	mid, _ := st.CreateMovie(ctx, &media.Movie{TMDBID: 603, Title: "The Matrix", Year: 1999,
		Monitored: true, Status: media.MediaSearching, RootFolderPath: libRoot})
	relName := "The.Matrix.1999.1080p.BluRay"
	srcDir := filepath.Join(cfg.TorrentPath(), relName)
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	video := filepath.Join(srcDir, "matrix.mkv")
	if err := os.WriteFile(video, []byte("video-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, _ := st.CreateJob(ctx, &job.Job{
		State: job.StateQueued, Category: "movie", NZBName: relName,
		Protocol: "torrent", MediaType: "movie", MediaRef: mid,
	})
	j, _ := st.GetJob(ctx, id)
	j.TorBoxID = 60
	st.UpdateJob(ctx, j)

	fake.torrentList = []torbox.TorrentDownload{{
		ID: 60, Name: relName, Size: 11, Progress: 1,
		DownloadFinished: true, DownloadPresent: true, DownloadState: "completed",
	}}
	if err := w.pollTorrentsOnce(ctx); err != nil {
		t.Fatalf("pollTorrentsOnce: %v", err)
	}

	got, _ := st.GetJob(ctx, id)
	if got.State != job.StateImported {
		t.Fatalf("job state: got %s want imported", got.State)
	}
	// Library symlink written at the Plex-standard path.
	wantLink := filepath.Join(libRoot, "The Matrix (1999)", "The Matrix (1999).mkv")
	target, err := os.Readlink(wantLink)
	if err != nil {
		t.Fatalf("library symlink not created: %v", err)
	}
	if target != video {
		t.Errorf("symlink target: got %q want %q", target, video)
	}
	// Movie flipped to available with library_path + has_file.
	m, _ := st.GetMovie(ctx, mid)
	if m.Status != media.MediaAvailable || !m.HasFile || m.LibraryPath != wantLink {
		t.Fatalf("movie not updated: status=%s hasFile=%v lib=%q", m.Status, m.HasFile, m.LibraryPath)
	}
	// imported_symlinks recorded for the healer.
	syms, _ := st.ListImportedSymlinks(ctx)
	if len(syms) != 1 || syms[0].SymlinkPath != wantLink {
		t.Fatalf("imported symlink not recorded: %+v", syms)
	}
}

func TestDeleterTorrentBranchAndLibrarySymlink(t *testing.T) {
	fake := &fakeTorBox{}
	w, st, cfg := testWorkers(t, fake)
	libRoot := t.TempDir()
	cfg.MovieLibraryRoot = libRoot
	ctx := context.Background()

	// A deleted torrent job with a recorded library symlink.
	dir := filepath.Join(libRoot, "Movie (2024)")
	os.MkdirAll(dir, 0o755)
	src := filepath.Join(t.TempDir(), "v.mkv")
	os.WriteFile(src, []byte("v"), 0o644)
	link := filepath.Join(dir, "Movie (2024).mkv")
	os.Symlink(src, link)

	id, _ := st.CreateJob(ctx, &job.Job{State: job.StateDeleted, Category: "movie", NZBName: "Movie", Protocol: "torrent"})
	j, _ := st.GetJob(ctx, id)
	j.TorBoxID = 90
	st.UpdateJob(ctx, j)
	st.UpsertImportedSymlink(ctx, &job.ImportedSymlink{JobID: id, SymlinkPath: link, TargetPath: src})

	if err := w.deleteOnce(ctx); err != nil {
		t.Fatalf("deleteOnce: %v", err)
	}
	if len(fake.torrentControls) != 1 || fake.torrentControls[0] != "delete" {
		t.Errorf("expected a torrent delete, got usenet=%v torrent=%v", fake.controls, fake.torrentControls)
	}
	if len(fake.controls) != 0 {
		t.Error("torrent delete must not call controlusenetdownload")
	}
	if _, err := os.Lstat(link); err == nil {
		t.Error("library symlink should be removed on delete")
	}
}
