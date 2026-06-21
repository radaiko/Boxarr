package worker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/release"
)

func TestSanitizePathComponent(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Avengers: Endgame", "Avengers Endgame"},
		{"a/b\\c", "a b c"},
		{"...", "untitled"},
		{"", "untitled"},
		{"Movie\x00Name", "Movie Name"},
		{"  spaced  ", "spaced"},
	}
	for _, c := range cases {
		if got := sanitizePathComponent(c.in); got != c.want {
			t.Errorf("sanitizePathComponent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAtomicReplaceSymlinkForcesAbsoluteTarget(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "movie.mkv")
	// A relative target (the bug: would be unresolvable from Plex).
	if err := atomicReplaceSymlink(link, "some/relative/target.mkv"); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(target) {
		t.Errorf("symlink target %q is not absolute", target)
	}
}

func TestMatchEpisodesMultiEpisode(t *testing.T) {
	eps := []*media.Episode{
		{SeasonNumber: 1, EpisodeNumber: 1}, {SeasonNumber: 1, EpisodeNumber: 2},
		{SeasonNumber: 1, EpisodeNumber: 3},
	}
	out := matchEpisodes(&release.ParsedRelease{SeasonNumber: 1, EpisodeStart: 1, EpisodeEnd: 2}, eps)
	if len(out) != 2 || out[0].EpisodeNumber != 1 || out[1].EpisodeNumber != 2 {
		t.Fatalf("multi-ep match = %+v", out)
	}
}

func TestMatchEpisodesAnimeAbsolute(t *testing.T) {
	eps := []*media.Episode{
		{SeasonNumber: 1, EpisodeNumber: 12, AbsoluteNumber: 12},
		{SeasonNumber: 2, EpisodeNumber: 1, AbsoluteNumber: 13},
	}
	out := matchEpisodes(&release.ParsedRelease{AbsoluteEpisodes: []int{13}}, eps)
	if len(out) != 1 || out[0].SeasonNumber != 2 || out[0].EpisodeNumber != 1 {
		t.Fatalf("anime absolute match = %+v", out)
	}
}

func TestMatchEpisodesDaily(t *testing.T) {
	eps := []*media.Episode{
		{SeasonNumber: 2024, EpisodeNumber: 100, AirDate: "2024-03-01"},
		{SeasonNumber: 2024, EpisodeNumber: 101, AirDate: "2024-03-02"},
	}
	out := matchEpisodes(&release.ParsedRelease{AirDate: "2024-03-02"}, eps)
	if len(out) != 1 || out[0].EpisodeNumber != 101 {
		t.Fatalf("daily match = %+v", out)
	}
}

func TestTVLinkPathMultiEpisodeRange(t *testing.T) {
	w := &Workers{}
	eps := []*media.Episode{
		{SeasonNumber: 1, EpisodeNumber: 1}, {SeasonNumber: 1, EpisodeNumber: 2},
	}
	p := w.tvLinkPath("/lib", "Show (2020)", "Show", eps, ".mkv")
	want := filepath.Join("/lib", "Show (2020)", "Season 01", "Show - S01E01-E02.mkv")
	if p != want {
		t.Errorf("tvLinkPath = %q, want %q", p, want)
	}
	// Single episode: no range suffix.
	if p := w.tvLinkPath("/lib", "Show (2020)", "Show", eps[:1], ".mkv"); filepath.Base(p) != "Show - S01E01.mkv" {
		t.Errorf("single-ep base = %q", filepath.Base(p))
	}
}

func TestMediaStatusForJob(t *testing.T) {
	cases := []struct {
		state job.State
		want  media.MediaStatus
		ok    bool
	}{
		{job.StatePending, media.MediaQueued, true},
		{job.StateSubmitting, media.MediaQueued, true},
		{job.StateQueued, media.MediaQueued, true},
		{job.StateDownloading, media.MediaDownloading, true},
		{job.StateSeeding, media.MediaDownloading, true},
		{job.StateImported, "", false},
		{job.StateFailed, "", false},
	}
	for _, c := range cases {
		got, ok := mediaStatusForJob(c.state)
		if ok != c.ok || got != c.want {
			t.Errorf("mediaStatusForJob(%s) = (%q,%v), want (%q,%v)", c.state, got, ok, c.want, c.ok)
		}
	}
}
