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
func (s *Service) UpgradeWanted(ctx context.Context) error { return s.upgrade(ctx, false) }

// UpgradeNow forces an upgrade pass that ignores the per-item cadence (the manual
// "Search for upgrades" button) so it acts immediately.
func (s *Service) UpgradeNow(ctx context.Context) error { return s.upgrade(ctx, true) }

func (s *Service) upgrade(ctx context.Context, force bool) error {
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
		// Note: no JobID requirement — imported jobs are reaped after a day, so the
		// upgrade must run off the item's state (lang_missing), not a live job.
		if m.HasFile && m.Monitored {
			s.tryUpgradeMovie(ctx, m, now, force)
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
		scMap := sceneNumbers(eps)
		for _, ep := range eps {
			if ep.HasFile && ep.Monitored {
				s.tryUpgradeEpisode(ctx, sr, ep, kind, now, scMap[ep.ID], force)
			}
		}
	}
	return nil
}

// upgradeDue gates an upgrade re-search. A lang_missing item is actively wrong, so
// it retries on the daily interval regardless of release age (not the slow monthly
// cadence); other items follow the normal release-aged cadence. force bypasses both.
func (s *Service) upgradeDue(releaseOrAir string, last *time.Time, now time.Time, langMissing, force bool) bool {
	if force {
		return true
	}
	cad := s.cadenceFromSettings()
	if langMissing {
		return last == nil || now.Sub(*last) >= cad.dailyInterval
	}
	return searchDue(releaseOrAir, last, now, cad)
}

func (s *Service) tryUpgradeMovie(ctx context.Context, m *media.Movie, now time.Time, force bool) {
	cur, _ := s.store.GetJob(ctx, m.JobID) // may be nil — job reaped/adopted
	curName := ""
	if cur != nil {
		curName = cur.NZBName
	}
	cfg := s.set.SelectionConfigFor("movie")
	ideal := idealLangs(cfg)
	// Satisfied only if the current release reaches the ideal AND Plex didn't flag
	// the file as language-missing.
	if languageSatisfied(curName, ideal, cfg.RequireAnyLanguage) && !m.LangMissing {
		return
	}
	if !s.upgradeDue(m.ReleaseDate, m.LastSearchedAt, now, m.LangMissing, force) {
		return
	}
	// Without a known current release we can only act on a language problem (we
	// can't compare quality), so skip non-lang_missing items with no job.
	if curName == "" && !m.LangMissing {
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
	if best, ok := s.pickBest(ctx, results, "movie"); ok && s.shouldGrabUpgrade(cfg, ideal, curName, best.Title, m.LangMissing) {
		_, _ = s.grabBest(ctx, best, "movie", m.ID, true)
	}
}

func (s *Service) tryUpgradeEpisode(ctx context.Context, sr *media.Series, ep *media.Episode, kind string, now time.Time, sc sceneNum, force bool) {
	cur, _ := s.store.GetJob(ctx, ep.JobID) // may be nil — job reaped/adopted
	curName := ""
	if cur != nil {
		curName = cur.NZBName
	}
	cfg := s.set.SelectionConfigFor(kind)
	ideal := idealLangs(cfg)
	if languageSatisfied(curName, ideal, cfg.RequireAnyLanguage) && !ep.LangMissing {
		return
	}
	if !s.upgradeDue(ep.AirDate, ep.LastSearchedAt, now, ep.LangMissing, force) {
		return
	}
	if curName == "" && !ep.LangMissing {
		return
	}
	if active, _ := s.store.ActiveJobForMedia(ctx, "episode", ep.ID); active != nil {
		return
	}
	_ = s.store.MarkEpisodesSearched(ctx, ep.ID)
	results := s.episodeReleases(ctx, sr.Title, ep, kind, sc)
	if best, ok := s.pickBest(ctx, results, kind); ok && s.shouldGrabUpgrade(cfg, ideal, curName, best.Title, ep.LangMissing) {
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

// shouldGrabUpgrade decides whether to replace current with candidate. The
// candidate must reach the ideal language (DE/EN/MULTi tag — pickBest has already
// dropped releases the KB verified to lack it). Then:
//   - langMissing (current is Plex-verified unwatchable): grab any language-
//     providing release that differs from current — language beats quality here,
//     so a correct-language 720p replaces a wrong-language 1080p.
//   - otherwise (pure quality/language upgrade): require a strictly higher score,
//     so we never churn one acceptable release for an equivalent one.
func (s *Service) shouldGrabUpgrade(cfg selection.Config, ideal []string, current, candidate string, langMissing bool) bool {
	if !languageSatisfied(candidate, ideal, cfg.RequireAnyLanguage) {
		return false
	}
	if langMissing {
		return candidate != current
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
