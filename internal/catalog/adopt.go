package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/radaiko/boxarr/internal/release"
)

// ResolveAdopt identifies an unknown WebDAV release folder and ensures a catalog
// row exists for it, returning the (mediaType, mediaRef) link the worker uses to
// import it into the library. It parses the folder name, decides movie vs series,
// searches TMDB, and adds (or reuses, on ErrAlreadyExists) the catalog entry.
func (s *Service) ResolveAdopt(ctx context.Context, name string) (string, int64, error) {
	p, _ := release.ParseRelease(name)
	title, year := adoptTitle(p, name)
	if title == "" {
		return "", 0, fmt.Errorf("could not parse a title from %q", name)
	}

	if isSeriesRelease(p) {
		term := title
		cands, err := s.LookupSeries(ctx, term)
		if err != nil {
			return "", 0, fmt.Errorf("tmdb series lookup: %w", err)
		}
		if len(cands) == 0 {
			return "", 0, fmt.Errorf("no TMDB series match for %q", term)
		}
		sr, err := s.AddSeries(ctx, cands[0].TMDBID, true, nil, "standard")
		if err != nil && !errors.Is(err, ErrAlreadyExists) {
			return "", 0, fmt.Errorf("adding series: %w", err)
		}
		// Ensure the adopted season's episode rows exist even when the series row
		// already existed (AddSeries skips syncSeasons on ErrAlreadyExists) or a
		// prior add never covered this season — otherwise the importer matches no
		// episodes and the adoption hard-fails. syncSeasons idempotently upserts.
		if p != nil && p.SeasonNumber > 0 {
			if d, derr := s.set.TMDB().TVDetails(ctx, int(sr.TMDBID)); derr == nil {
				_ = s.syncSeasons(ctx, sr, d.Seasons, false, map[int]bool{p.SeasonNumber: true})
			}
		}
		return "series", sr.ID, nil
	}

	term := title
	if year > 0 {
		term = fmt.Sprintf("%s %d", title, year)
	}
	cands, err := s.LookupMovies(ctx, term)
	if err != nil {
		return "", 0, fmt.Errorf("tmdb movie lookup: %w", err)
	}
	best := pickMovieCandidate(cands, year)
	if best == nil {
		return "", 0, fmt.Errorf("no TMDB movie match for %q", term)
	}
	m, err := s.AddMovie(ctx, best.TMDBID, true)
	if err != nil && !errors.Is(err, ErrAlreadyExists) {
		return "", 0, fmt.Errorf("adding movie: %w", err)
	}
	return "movie", m.ID, nil
}

// isSeriesRelease mirrors the reconciler's guessCategory series heuristics.
func isSeriesRelease(p *release.ParsedRelease) bool {
	return p != nil && (p.SeasonNumber > 0 || p.EpisodeStart > 0 || p.IsSeasonPack ||
		p.AirDate != "" || len(p.AbsoluteEpisodes) > 0)
}

// adoptTitle returns the cleaned title + year from a parse (falling back to a
// lightly-cleaned folder name when the parser yields nothing usable).
func adoptTitle(p *release.ParsedRelease, name string) (string, int) {
	if p != nil && strings.TrimSpace(p.Title) != "" {
		return strings.TrimSpace(p.Title), p.Year
	}
	// Fallback: replace separators with spaces and take the leading words.
	cleaned := strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(name)
	return strings.TrimSpace(strings.Join(strings.Fields(cleaned), " ")), 0
}

// pickMovieCandidate prefers an exact-year match when a year is known, else the
// first result.
func pickMovieCandidate(cands []MovieCandidate, year int) *MovieCandidate {
	if len(cands) == 0 {
		return nil
	}
	if year > 0 {
		for i := range cands {
			if cands[i].Year == year {
				return &cands[i]
			}
		}
	}
	return &cands[0]
}
