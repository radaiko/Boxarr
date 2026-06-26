package catalog

import (
	"context"
	"log/slog"
	"time"

	"github.com/radaiko/boxarr/internal/media"
)

// AutoSearchWanted searches+grabs the best release for every monitored, aired,
// file-less movie and episode (FR-SR-4). No-op when no Searcher is wired.
func (s *Service) AutoSearchWanted(ctx context.Context) error {
	if s.search == nil {
		return nil
	}
	// Auto-retry: return failed items to the wanted pool so they're re-searched.
	// The grab blocklist ensures a different release is grabbed, not the broken one.
	if n, err := s.store.ResetFailedForRetry(ctx); err == nil && n > 0 {
		slog.Default().Info("auto-retry: reset failed items to wanted for re-search", "count", n)
	}
	movies, err := s.store.WantedMovies(ctx)
	if err != nil {
		return err
	}
	for _, m := range movies {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if m.JobID != 0 {
			continue // already has an in-flight/linked grab
		}
		_ = s.SearchWantedForMovie(ctx, m.ID)
	}
	// Group wanted episodes by series so each series is searched once.
	episodes, err := s.store.WantedEpisodes(ctx)
	if err != nil {
		return err
	}
	seen := map[int64]bool{}
	for _, ep := range episodes {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if seen[ep.SeriesID] {
			continue
		}
		seen[ep.SeriesID] = true
		_ = s.SearchWantedForSeries(ctx, ep.SeriesID)
	}
	return nil
}

// RefreshMetadata re-fetches TMDB metadata for every catalog item to pick up new
// seasons/episodes (FR-CAT-5). Upserts preserve lifecycle columns, so acquisition
// state is never clobbered. Newly-aired monitored episodes become wanted.
func (s *Service) RefreshMetadata(ctx context.Context) error {
	series, err := s.store.ListSeries(ctx)
	if err != nil {
		return err
	}
	for _, sr := range series {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		d, derr := s.set.TMDB().TVDetails(ctx, int(sr.TMDBID))
		if derr != nil {
			continue // transient; try again next cycle
		}
		// Monitor set = the currently-monitored season numbers.
		seasons, _ := s.store.ListSeasons(ctx, sr.ID)
		monSet := map[int]bool{}
		for _, sn := range seasons {
			if sn.Monitored {
				monSet[sn.SeasonNumber] = true
			}
		}
		_ = s.syncSeasons(ctx, sr, d.Seasons, false, monSet)
		// Promote newly-aired monitored episodes to wanted.
		s.promoteAiredEpisodes(ctx, sr.ID)
		now := time.Now()
		sr.TMDBStatus = d.Status
		sr.LastMetadataSync = &now
		_ = s.store.UpdateSeries(ctx, sr)
	}
	return nil
}

// promoteAiredEpisodes flips monitored, aired, file-less episodes from missing to
// wanted (the air-date-aware promotion FR-CAT-4).
func (s *Service) promoteAiredEpisodes(ctx context.Context, seriesID int64) {
	episodes, err := s.store.ListEpisodes(ctx, seriesID)
	if err != nil {
		return
	}
	today := time.Now().UTC().Format("2006-01-02")
	for _, ep := range episodes {
		if ep.Monitored && !ep.HasFile && ep.Status == media.MediaMissing &&
			ep.AirDate != "" && ep.AirDate <= today {
			_ = s.store.SetEpisodeStatus(ctx, ep.ID, media.MediaWanted)
		}
	}
}
