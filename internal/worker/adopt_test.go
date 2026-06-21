package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/torbox"
	"github.com/radaiko/boxarr/internal/webdav"
)

func TestAdoptUnknownImportsToLibrary(t *testing.T) {
	fake := &fakeTorBox{}
	w, st, cfg := testWorkers(t, fake)
	libRoot := t.TempDir()
	cfg.MovieLibraryRoot = libRoot
	ctx := context.Background()

	// The unknown content already on the mount.
	relName := "The.Matrix.1999.1080p.BluRay"
	srcDir := filepath.Join(cfg.WebDAVMountRoot, relName) // mount root (usenet subpath "usenet" but adopt uses remotePath directly)
	os.MkdirAll(srcDir, 0o755)
	video := filepath.Join(srcDir, "matrix.mkv")
	os.WriteFile(video, []byte("video"), 0o644)
	_ = st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{Name: relName, RemotePath: srcDir, Category: "unknown"})

	// Resolver creates the movie row and returns ("movie", id).
	var movieID int64
	w.SetAdoptResolver(resolverFunc(func(c context.Context, name string) (string, int64, error) {
		id, err := st.CreateMovie(c, &media.Movie{TMDBID: 603, Title: "The Matrix", Year: 1999,
			Monitored: true, Status: media.MediaMissing, RootFolderPath: libRoot})
		movieID = id
		return "movie", id, err
	}))
	// A matching TorBox torrent so the adopted job links its download.
	fake.torrentList = []torbox.TorrentDownload{{ID: 77, Name: relName, Hash: "abc"}}

	if err := w.AdoptUnknown(ctx, srcDir, relName); err != nil {
		t.Fatalf("AdoptUnknown: %v", err)
	}

	// Movie is now available with a library symlink.
	m, _ := st.GetMovie(ctx, movieID)
	if m.Status != media.MediaAvailable || !m.HasFile {
		t.Fatalf("movie not imported: status=%s hasFile=%v", m.Status, m.HasFile)
	}
	wantLink := filepath.Join(libRoot, "The Matrix (1999)", "The Matrix (1999).mkv")
	if target, err := os.Readlink(wantLink); err != nil || target != video {
		t.Fatalf("library symlink wrong: target=%q err=%v", target, err)
	}
	// The job is imported, links the existing TorBox download, and item is known.
	jobs, _ := st.JobsByState(ctx, job.StateImported)
	if len(jobs) != 1 || jobs[0].TorBoxID != 77 || jobs[0].Protocol != "torrent" {
		t.Fatalf("adopted job not linked: %+v", jobs)
	}
	it, _ := st.GetWebDAVItemByPath(ctx, srcDir)
	if !it.Known {
		t.Error("adopted item should be marked known")
	}
}

func TestAdoptUnknownNoResolver(t *testing.T) {
	w, _, _ := testWorkers(t, &fakeTorBox{})
	if err := w.AdoptUnknown(context.Background(), "/mnt/x", "X"); err == nil {
		t.Error("adopt without a resolver should error")
	}
}

func TestAdoptUnknownRollsBackOnImportFailure(t *testing.T) {
	fake := &fakeTorBox{}
	w, st, cfg := testWorkers(t, fake)
	cfg.MovieLibraryRoot = t.TempDir()
	ctx := context.Background()

	// A mount folder with NO video file → importMovie fails (no playable file).
	relName := "Empty.Movie.2024.1080p"
	srcDir := filepath.Join(cfg.WebDAVMountRoot, relName)
	os.MkdirAll(srcDir, 0o755)

	var movieID int64
	w.SetAdoptResolver(resolverFunc(func(c context.Context, name string) (string, int64, error) {
		id, err := st.CreateMovie(c, &media.Movie{TMDBID: 9, Title: "Empty Movie", Year: 2024,
			Monitored: true, Status: media.MediaMissing})
		movieID = id
		return "movie", id, err
	}))

	if err := w.AdoptUnknown(ctx, srcDir, relName); err == nil {
		t.Fatal("adopt of a folder with no video should fail")
	}
	// Rollback: the placeholder job is gone and the movie is not falsely available.
	if jobs, _ := st.JobsByState(ctx, job.StateImported); len(jobs) != 0 {
		t.Errorf("no job should survive a failed adopt, got %d", len(jobs))
	}
	if m, _ := st.GetMovie(ctx, movieID); m.HasFile || m.Status == media.MediaAvailable {
		t.Errorf("movie must not be marked available after a failed adopt: %+v", m)
	}
}

// resolverFunc adapts a func to the AdoptResolver interface.
type resolverFunc func(ctx context.Context, name string) (string, int64, error)

func (f resolverFunc) ResolveAdopt(ctx context.Context, name string) (string, int64, error) {
	return f(ctx, name)
}
