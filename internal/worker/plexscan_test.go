package worker

import "testing"

func TestPlexScanTarget(t *testing.T) {
	cases := []struct {
		name, dir, root string
		locs            []string
		want            string
		ok              bool
	}{
		{"basename remap (the live mismatch)", "/mnt/library/tv/Show/Season 01", "/mnt/library/tv",
			[]string{"/mnt/smedia/tv"}, "/mnt/smedia/tv/Show/Season 01", true},
		{"exact match unchanged", "/data/tv/Show", "/data/tv", []string{"/data/tv"}, "/data/tv/Show", true},
		{"single location remap", "/a/movies/M (2024)", "/a/movies", []string{"/plex/films"}, "/plex/films/M (2024)", true},
		{"trailing slash root", "/mnt/library/anime/X/S01", "/mnt/library/anime/", []string{"/mnt/smedia/anime"}, "/mnt/smedia/anime/X/S01", true},
		{"ambiguous multi → section scan", "/a/movies/M", "/a/movies", []string{"/p/x", "/p/y"}, "", false},
		{"no plex locations → section scan", "/a/movies/M", "/a/movies", nil, "", false},
		{"dir outside root → section scan", "/other/M", "/a/movies", []string{"/p/movies"}, "", false},
	}
	for _, c := range cases {
		got, ok := plexScanTarget(c.dir, c.root, c.locs)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: got (%q,%v), want (%q,%v)", c.name, got, ok, c.want, c.ok)
		}
	}
}
