package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/release"
	"github.com/radaiko/boxarr/internal/torbox"
)

// seedSeries creates a series with one season of n episodes and returns its id.
func seedSeries(t *testing.T, st storeT, ctx context.Context, n int) (int64, []int64) {
	t.Helper()
	sid, err := st.CreateSeries(ctx, &media.Series{TMDBID: 1, Title: "Breaking Bad", Year: 2008,
		Monitored: true, RootFolderPath: ""})
	if err != nil {
		t.Fatal(err)
	}
	seasonID, _ := st.UpsertSeason(ctx, &media.Season{SeriesID: sid, SeasonNumber: 1, Monitored: true})
	var epIDs []int64
	for i := 1; i <= n; i++ {
		id, _ := st.UpsertEpisode(ctx, &media.Episode{
			SeriesID: sid, SeasonID: seasonID, SeasonNumber: 1, EpisodeNumber: i,
			Title: "Ep", Monitored: true, Status: media.MediaWanted,
		})
		epIDs = append(epIDs, id)
	}
	return sid, epIDs
}

// storeT is the subset of *store.Store the helper needs (keeps the signature short).
type storeT interface {
	CreateSeries(context.Context, *media.Series) (int64, error)
	UpsertSeason(context.Context, *media.Season) (int64, error)
	UpsertEpisode(context.Context, *media.Episode) (int64, error)
}

func TestTVImportSeasonPackMapsEachFile(t *testing.T) {
	fake := &fakeTorBox{}
	w, st, cfg := testWorkers(t, fake)
	libRoot := t.TempDir()
	cfg.TVLibraryRoot = libRoot
	ctx := context.Background()

	sid, _ := seedSeries(t, st, ctx, 3)

	// A season-pack release folder with 3 episode files.
	relName := "Breaking.Bad.S01.1080p.BluRay"
	srcDir := filepath.Join(cfg.TorrentPath(), relName)
	os.MkdirAll(srcDir, 0o755)
	for _, fn := range []string{
		"Breaking.Bad.S01E01.1080p.mkv",
		"Breaking.Bad.S01E02.1080p.mkv",
		"Breaking.Bad.S01E03.1080p.mkv",
	} {
		os.WriteFile(filepath.Join(srcDir, fn), []byte("v"), 0o644)
	}

	id, _ := st.CreateJob(ctx, &job.Job{
		State: job.StateQueued, Category: "series", NZBName: relName,
		Protocol: "torrent", MediaType: "season", MediaRef: sid,
	})
	j, _ := st.GetJob(ctx, id)
	j.TorBoxID = 80
	st.UpdateJob(ctx, j)
	fake.torrentList = []torbox.TorrentDownload{{
		ID: 80, Name: relName, Size: 3, Progress: 1,
		DownloadFinished: true, DownloadPresent: true, DownloadState: "completed",
	}}

	if err := w.pollTorrentsOnce(ctx); err != nil {
		t.Fatalf("pollTorrentsOnce: %v", err)
	}

	got, _ := st.GetJob(ctx, id)
	if got.State != job.StateImported {
		t.Fatalf("job state: got %s want imported", got.State)
	}
	eps, _ := st.ListEpisodes(ctx, sid)
	for _, e := range eps {
		if !e.HasFile || e.Status != media.MediaAvailable {
			t.Errorf("episode S01E%02d not imported: hasFile=%v status=%s", e.EpisodeNumber, e.HasFile, e.Status)
		}
		wantLink := filepath.Join(libRoot, "Breaking Bad (2008)", "Season 01")
		if filepath.Dir(e.LibraryPath) != wantLink {
			t.Errorf("episode %d library path %q not under %q", e.EpisodeNumber, e.LibraryPath, wantLink)
		}
	}
	// One symlink per episode file.
	syms, _ := st.ListImportedSymlinks(ctx)
	if len(syms) != 3 {
		t.Fatalf("expected 3 episode symlinks, got %d", len(syms))
	}
}

func TestTVImportSingleEpisode(t *testing.T) {
	fake := &fakeTorBox{}
	w, st, cfg := testWorkers(t, fake)
	libRoot := t.TempDir()
	cfg.TVLibraryRoot = libRoot
	ctx := context.Background()

	sid, epIDs := seedSeries(t, st, ctx, 2)
	relName := "Breaking.Bad.S01E02.1080p.WEB"
	srcDir := filepath.Join(cfg.UsenetPath(), relName)
	os.MkdirAll(srcDir, 0o755)
	os.WriteFile(filepath.Join(srcDir, "Breaking.Bad.S01E02.mkv"), []byte("v"), 0o644)

	id, _ := st.CreateJob(ctx, &job.Job{
		State: job.StateQueued, Category: "series", NZBName: relName,
		Protocol: "usenet", MediaType: "episode", MediaRef: epIDs[1], // S01E02
	})
	j, _ := st.GetJob(ctx, id)
	j.TorBoxID = 81
	st.UpdateJob(ctx, j)
	fake.list = []torbox.UsenetDownload{{
		ID: 81, Name: relName, Size: 1, Progress: 1,
		DownloadFinished: true, DownloadPresent: true,
	}}

	if err := w.pollOnce(ctx); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}
	_ = sid
	e2, _ := st.GetEpisode(ctx, epIDs[1])
	if !e2.HasFile || e2.Status != media.MediaAvailable {
		t.Fatalf("S01E02 not imported: %+v", e2)
	}
	e1, _ := st.GetEpisode(ctx, epIDs[0])
	if e1.HasFile {
		t.Error("S01E01 must not be imported by an S01E02 grab")
	}
}

func TestMatchEpisodesScene(t *testing.T) {
	// Solo Leveling: flat TMDB S01E22, mapped to TVDB scene S02E10.
	eps := []*media.Episode{
		{ID: 1, SeasonNumber: 1, EpisodeNumber: 21, SceneSeason: 2, SceneEpisode: 9},
		{ID: 2, SeasonNumber: 1, EpisodeNumber: 22, SceneSeason: 2, SceneEpisode: 10},
		{ID: 3, SeasonNumber: 1, EpisodeNumber: 23, SceneSeason: 2, SceneEpisode: 11},
	}
	p, err := release.ParseRelease("Solo.Leveling.S02E10.We.Need.A.Hero.2160p.WEB-DL.MULTI.AAC2.0.x265.Multi.Subs.AniMeitantei")
	if err != nil {
		t.Fatal(err)
	}
	m := matchEpisodes(p, eps)
	if len(m) != 1 || m[0].ID != 2 {
		t.Fatalf("S02E10 should match the episode with scene S02E10 (id 2), got %+v", m)
	}
}

func TestMatchEpisodesStandardBeatsScene(t *testing.T) {
	// A normal series: real S/E must win even if some episode has scene numbers.
	eps := []*media.Episode{
		{ID: 1, SeasonNumber: 1, EpisodeNumber: 5},
		{ID: 2, SeasonNumber: 1, EpisodeNumber: 22, SceneSeason: 1, SceneEpisode: 5},
	}
	p, _ := release.ParseRelease("Show.S01E05.1080p.WEB-DL")
	m := matchEpisodes(p, eps)
	if len(m) != 1 || m[0].ID != 1 {
		t.Fatalf("real S01E05 should win, got %+v", m)
	}
}
