package catalog

import (
	"context"
	"testing"

	"github.com/radaiko/boxarr/internal/prowlarr"
)

func TestPickBestSkipsBlocklisted(t *testing.T) {
	cat, st, _ := newCatalog(t, selCfg())
	ctx := context.Background()
	results := []prowlarr.ReleaseResource{
		{Title: "Movie.2024.German.DL.2160p.BluRay-TOP", Protocol: "usenet", Size: 8_000_000_000},
		{Title: "Movie.2024.German.DL.1080p.BluRay-ALT", Protocol: "usenet", Size: 4_000_000_000},
	}
	// Without a blocklist, the higher-scoring 2160p release wins.
	if best, ok := cat.pickBest(ctx, results, "movie"); !ok || best.Title != results[0].Title {
		t.Fatalf("baseline should pick the top release, got %q ok=%v", best.Title, ok)
	}
	// Blocklisting it (a failed download) makes pickBest skip to the next release.
	_ = st.BlocklistGrab(ctx, "Movie.2024.German.DL.2160p.BluRay-TOP", "incomplete")
	best, ok := cat.pickBest(ctx, results, "movie")
	if !ok || best.Title != results[1].Title {
		t.Fatalf("blocklisted release must be skipped; got %q ok=%v", best.Title, ok)
	}
}
