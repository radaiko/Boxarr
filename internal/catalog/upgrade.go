package catalog

import (
	"context"
	"fmt"
	"time"

	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/prowlarr"
	"github.com/radaiko/boxarr/internal/release"
	"github.com/radaiko/boxarr/internal/selection"
)

// UpgradeWanted re-searches already-imported items whose current release lacks
// the ideal language and replaces them with a better one (e.g. German over the
// English fallback). It is release-gated by the same escalating cadence as
// acquisition and stops once the ideal language is present.
func (s *Service) UpgradeWanted(ctx context.Context) error {
	if s.search == nil {
		return nil
	}
	now := time.Now()
	movies, err := s.store.ListMovies(ctx)
	if err != nil {
		return err
	}
	for _, m := range movies {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if m.HasFile && m.Monitored && m.JobID != 0 {
			s.tryUpgradeMovie(ctx, m, now)
		}
	}
	series, err := s.store.ListSeries(ctx)
	if err != nil {
		return err
	}
	for _, sr := range series {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		kind := "series"
		if sr.SeriesType == "anime" {
			kind = "anime"
		}
		eps, _ := s.store.ListEpisodes(ctx, sr.ID)
		for _, ep := range eps {
			if ep.HasFile && ep.Monitored && ep.JobID != 0 {
				s.tryUpgradeEpisode(ctx, sr, ep, kind, now)
			}
		}
	}
	return nil
}

func (s *Service) tryUpgradeMovie(ctx context.Context, m *media.Movie, now time.Time) {
	cur, err := s.store.GetJob(ctx, m.JobID)
	if err != nil || cur == nil {
		return
	}
	if languageSatisfied("movie", cur.NZBName) || !searchDue(m.ReleaseDate, m.LastSearchedAt, now) {
		return
	}
	if active, _ := s.store.ActiveJobForMedia(ctx, "movie", m.ID); active != nil {
		return // an upgrade (or grab) is already in flight
	}
	_ = s.store.MarkMovieSearched(ctx, m.ID)
	q := m.Title
	if m.Year > 0 {
		q = fmt.Sprintf("%s %d", m.Title, m.Year)
	}
	results, err := s.search.Search(ctx, prowlarr.SearchParams{Query: q, Type: "movie", Categories: []int{2000}})
	if err != nil {
		return
	}
	cfg := s.set.SelectionConfigFor("movie")
	if best, ok := s.pickBest(results, "movie"); ok && s.isUpgrade(cfg, "movie", cur.NZBName, best.Title) {
		_, _ = s.grabBest(ctx, best, "movie", m.ID, true)
	}
}

func (s *Service) tryUpgradeEpisode(ctx context.Context, sr *media.Series, ep *media.Episode, kind string, now time.Time) {
	cur, err := s.store.GetJob(ctx, ep.JobID)
	if err != nil || cur == nil {
		return
	}
	if languageSatisfied(kind, cur.NZBName) || !searchDue(ep.AirDate, ep.LastSearchedAt, now) {
		return
	}
	if active, _ := s.store.ActiveJobForMedia(ctx, "episode", ep.ID); active != nil {
		return
	}
	_ = s.store.MarkEpisodesSearched(ctx, ep.ID)
	q := fmt.Sprintf("%s S%02dE%02d", sr.Title, ep.SeasonNumber, ep.EpisodeNumber)
	results, err := s.search.Search(ctx, prowlarr.SearchParams{Query: q, Type: "tvsearch", Categories: []int{5000}})
	if err != nil {
		return
	}
	cfg := s.set.SelectionConfigFor(kind)
	if best, ok := s.pickBest(results, kind); ok && s.isUpgrade(cfg, kind, cur.NZBName, best.Title) {
		_, _ = s.grabBest(ctx, best, "episode", ep.ID, true)
	}
}

// isUpgrade reports whether candidate is a worthwhile replacement for current:
// it must reach the ideal language (which current lacks) and score strictly
// higher, so we never churn one acceptable release for an equivalent one.
func (s *Service) isUpgrade(cfg selection.Config, kind, current, candidate string) bool {
	if !languageSatisfied(kind, candidate) {
		return false
	}
	return cfg.Score(selection.Release{Title: candidate}) > cfg.Score(selection.Release{Title: current})
}

// languageSatisfied reports whether a release already carries the ideal language
// for its type. Movies/series want German (the upgrade target). For anime, German
// and English are equivalent per the language rules, so anime never triggers a
// language upgrade (subtitle handling is Plex's job, not a re-grab).
func languageSatisfied(kind, name string) bool {
	if kind == "anime" {
		return true
	}
	for _, l := range release.DetectLanguages(name) {
		if l == "DE" {
			return true
		}
	}
	return false
}
