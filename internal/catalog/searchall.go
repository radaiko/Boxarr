package catalog

import (
	"context"
	"fmt"

	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/prowlarr"
)

// SearchAllMissing force-searches every monitored, released/aired, file-less item
// (ignoring the per-item cadence — this is a manual "do it now" action) and
// reports each outcome via report, so the user can see exactly what happened.
// Returns how many were grabbed and how many were searched.
func (s *Service) SearchAllMissing(ctx context.Context, report func(string)) (grabbed, searched int, err error) {
	rep := func(f string, a ...any) {
		if report != nil {
			report(fmt.Sprintf(f, a...))
		}
	}
	if s.search == nil {
		rep("Prowlarr is not configured — nothing to search")
		return 0, 0, nil
	}
	movies, err := s.store.WantedMovies(ctx)
	if err != nil {
		return grabbed, searched, err
	}
	for _, m := range movies {
		if ctx.Err() != nil {
			return grabbed, searched, ctx.Err()
		}
		if m.JobID != 0 {
			continue // already has an in-flight grab
		}
		searched++
		if s.forceSearchMovie(ctx, m, rep) {
			grabbed++
		}
	}
	eps, err := s.store.WantedEpisodes(ctx)
	if err != nil {
		return grabbed, searched, err
	}
	seriesCache := map[int64]*media.Series{}
	for _, ep := range eps {
		if ctx.Err() != nil {
			return grabbed, searched, ctx.Err()
		}
		sr := seriesCache[ep.SeriesID]
		if sr == nil {
			if sr, _ = s.store.GetSeries(ctx, ep.SeriesID); sr == nil {
				continue
			}
			seriesCache[ep.SeriesID] = sr
		}
		searched++
		if s.forceSearchEpisode(ctx, sr, ep, rep) {
			grabbed++
		}
	}
	if searched == 0 {
		rep("Nothing missing — every monitored item already has a file")
	}
	return grabbed, searched, nil
}

func (s *Service) forceSearchMovie(ctx context.Context, m *media.Movie, rep func(string, ...any)) bool {
	q := m.Title
	if m.Year > 0 {
		q = fmt.Sprintf("%s %d", m.Title, m.Year)
	}
	results, err := s.search.Search(ctx, prowlarr.SearchParams{Query: q, Type: "movie", Categories: []int{2000}})
	if err != nil {
		rep("%s — search error: %v", m.Title, err)
		return false
	}
	best, ok := s.pickBest(ctx, results, "movie")
	if !ok {
		rep("%s — no acceptable release (%d candidates)", m.Title, len(results))
		return false
	}
	jb, gerr := s.grabBest(ctx, best, "movie", m.ID, false)
	if gerr != nil {
		rep("%s — grab failed: %v", m.Title, gerr)
		return false
	}
	m.JobID = jb.ID
	if m.Status != media.MediaAvailable {
		m.Status = media.MediaQueued
	}
	_ = s.store.UpdateMovie(ctx, m)
	_ = s.store.MarkMovieSearched(ctx, m.ID)
	rep("%s — grabbed %s", m.Title, best.Title)
	return true
}

func (s *Service) forceSearchEpisode(ctx context.Context, sr *media.Series, ep *media.Episode, rep func(string, ...any)) bool {
	kind := "series"
	if sr.SeriesType == "anime" {
		kind = "anime"
	}
	label := fmt.Sprintf("%s S%02dE%02d", sr.Title, ep.SeasonNumber, ep.EpisodeNumber)
	results, err := s.search.Search(ctx, prowlarr.SearchParams{Query: label, Type: "tvsearch", Categories: []int{5000}})
	if err != nil {
		rep("%s — search error: %v", label, err)
		return false
	}
	best, ok := s.pickBest(ctx, results, kind)
	if !ok {
		rep("%s — no acceptable release (%d candidates)", label, len(results))
		return false
	}
	jb, gerr := s.grabBest(ctx, best, "episode", ep.ID, false)
	if gerr != nil {
		rep("%s — grab failed: %v", label, gerr)
		return false
	}
	ep.JobID = jb.ID
	if ep.Status != media.MediaAvailable {
		_ = s.store.SetEpisodeStatus(ctx, ep.ID, media.MediaQueued)
	}
	_ = s.store.MarkEpisodesSearched(ctx, ep.ID)
	rep("%s — grabbed %s", label, best.Title)
	return true
}
