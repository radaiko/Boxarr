package catalog

import (
	"context"
	"fmt"
	"strings"
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
	cfg := s.set.SelectionConfigFor("movie")
	ideal := idealLangs(cfg)
	// Satisfied only if the release name reaches the ideal AND Plex didn't flag
	// the actual file as language-missing.
	satisfied := languageSatisfied(cur.NZBName, ideal, cfg.RequireAnyLanguage) && !m.LangMissing
	if satisfied || !searchDue(m.ReleaseDate, m.LastSearchedAt, now, s.cadenceFromSettings()) {
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
	if best, ok := s.pickBest(results, "movie"); ok && s.isUpgrade(cfg, ideal, cur.NZBName, best.Title) {
		_, _ = s.grabBest(ctx, best, "movie", m.ID, true)
	}
}

func (s *Service) tryUpgradeEpisode(ctx context.Context, sr *media.Series, ep *media.Episode, kind string, now time.Time) {
	cur, err := s.store.GetJob(ctx, ep.JobID)
	if err != nil || cur == nil {
		return
	}
	cfg := s.set.SelectionConfigFor(kind)
	ideal := idealLangs(cfg)
	// Anime is normally language-satisfied (DE==EN), but if Plex flagged the file
	// as having no acceptable language, re-search it.
	satisfied := languageSatisfied(cur.NZBName, ideal, cfg.RequireAnyLanguage) && !ep.LangMissing
	if satisfied || !searchDue(ep.AirDate, ep.LastSearchedAt, now, s.cadenceFromSettings()) {
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
	if best, ok := s.pickBest(results, kind); ok && s.isUpgrade(cfg, ideal, cur.NZBName, best.Title) {
		_, _ = s.grabBest(ctx, best, "episode", ep.ID, true)
	}
}

// idealLangs is the language goal for a type from settings: the preferred
// languages, or the required ones when no preferred list is set.
func idealLangs(cfg selection.Config) []string {
	if len(cfg.PreferredLanguages) > 0 {
		return cfg.PreferredLanguages
	}
	return cfg.RequiredLanguages
}

// isUpgrade reports whether candidate is a worthwhile replacement for current:
// it must reach the ideal language (which current lacks) and score strictly
// higher, so we never churn one acceptable release for an equivalent one.
func (s *Service) isUpgrade(cfg selection.Config, ideal []string, current, candidate string) bool {
	if !languageSatisfied(candidate, ideal, cfg.RequireAnyLanguage) {
		return false
	}
	return cfg.Score(selection.Release{Title: candidate}) > cfg.Score(selection.Release{Title: current})
}

// languageSatisfied reports whether a release already carries the configured ideal
// language. With requireAny (e.g. anime: German OR English), any preferred
// language counts; otherwise the top preferred language is the target (so an
// English movie is not satisfied when German is preferred first). No preferred
// languages configured → always satisfied (nothing to upgrade toward).
func languageSatisfied(name string, ideal []string, requireAny bool) bool {
	if len(ideal) == 0 {
		return true
	}
	langs := release.DetectLanguages(name)
	has := func(code string) bool {
		for _, l := range langs {
			if strings.EqualFold(l, code) {
				return true
			}
		}
		return false
	}
	if requireAny {
		for _, code := range ideal {
			if has(code) {
				return true
			}
		}
		return false
	}
	return has(ideal[0])
}
