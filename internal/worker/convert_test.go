package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/media"
)

func TestConvertSeriesToAnimeMovesFiles(t *testing.T) {
	w, st, cfg := testWorkers(t, &fakeTorBox{})
	tvRoot, animeRoot := t.TempDir(), t.TempDir()
	cfg.TVLibraryRoot, cfg.AnimeLibraryRoot = tvRoot, animeRoot
	ctx := context.Background()

	// The real downloaded file on the mount + its library symlink under tvRoot.
	mnt := t.TempDir()
	target := filepath.Join(mnt, "Frieren.S01E12", "ep.mkv")
	_ = os.MkdirAll(filepath.Dir(target), 0o755)
	_ = os.WriteFile(target, []byte("v"), 0o644)

	sid, err := st.CreateSeries(ctx, &media.Series{
		TMDBID: 1, Title: "Frieren", SeriesType: "standard", RootFolderPath: tvRoot, Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	seasonID, _ := st.UpsertSeason(ctx, &media.Season{SeriesID: sid, SeasonNumber: 1, Monitored: true})
	libPath := filepath.Join(tvRoot, "Frieren", "Season 01", "Frieren - S01E12.mkv")
	_ = os.MkdirAll(filepath.Dir(libPath), 0o755)
	if err := os.Symlink(target, libPath); err != nil {
		t.Fatal(err)
	}
	jid, _ := st.CreateJob(ctx, &job.Job{State: job.StateImported, NZBName: "Frieren.S01E12", MediaType: "episode"})
	epID, _ := st.UpsertEpisode(ctx, &media.Episode{
		SeriesID: sid, SeasonID: seasonID, SeasonNumber: 1, EpisodeNumber: 12, TMDBID: 100,
		HasFile: true, LibraryPath: libPath, JobID: jid})
	if err := st.UpsertImportedSymlink(ctx, &job.ImportedSymlink{JobID: jid, SymlinkPath: libPath, TargetPath: target}); err != nil {
		t.Fatal(err)
	}

	if err := w.ConvertSeriesType(ctx, sid, "anime"); err != nil {
		t.Fatalf("ConvertSeriesType: %v", err)
	}

	sr, _ := st.GetSeries(ctx, sid)
	if sr.SeriesType != "anime" || sr.RootFolderPath != animeRoot {
		t.Fatalf("series not converted: type=%q root=%q", sr.SeriesType, sr.RootFolderPath)
	}
	newPath := filepath.Join(animeRoot, "Frieren", "Season 01", "Frieren - S01E12.mkv")
	if got, lerr := os.Readlink(newPath); lerr != nil || got != target {
		t.Errorf("new symlink wrong: path=%q target=%q err=%v", newPath, got, lerr)
	}
	if _, lerr := os.Lstat(libPath); !os.IsNotExist(lerr) {
		t.Error("old symlink should be removed")
	}
	ep, _ := st.GetEpisode(ctx, epID)
	if ep.LibraryPath != newPath {
		t.Errorf("episode library path = %q, want %q", ep.LibraryPath, newPath)
	}
}
