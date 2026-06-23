package catalog

import (
	"context"
	"fmt"

	"github.com/radaiko/boxarr/internal/media"
)

// RefreshAllFromTVDB updates every series' episode numbering from TheTVDB (when a
// key is configured). Reported per series so the task log shows progress.
func (s *Service) RefreshAllFromTVDB(ctx context.Context, report func(string)) error {
	rep := func(f string, a ...any) {
		if report != nil {
			report(fmt.Sprintf(f, a...))
		}
	}
	if !s.set.TVDBEnabled() {
		rep("TheTVDB is not configured — add a v4 API key in Settings")
		return nil
	}
	series, err := s.store.ListSeries(ctx)
	if err != nil {
		return err
	}
	for _, sr := range series {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, err := s.refreshSeriesFromTVDB(ctx, sr)
		switch {
		case err != nil:
			rep("%s — TVDB error: %v", sr.Title, err)
		case sr.TVDBID == 0:
			rep("%s — no TVDB id, skipped", sr.Title)
		case n > 0:
			rep("%s — mapped %d episodes from TVDB", sr.Title, n)
		default:
			rep("%s — nothing to map", sr.Title)
		}
	}
	return nil
}

// refreshSeriesFromTVDB pulls the aired-season episode list from TheTVDB and
// records each local episode's scene season/episode + absolute number, matched by
// air date (then by absolute number for flat/anime numbering). Returns the count
// updated.
func (s *Service) refreshSeriesFromTVDB(ctx context.Context, sr *media.Series) (int, error) {
	if sr.TVDBID == 0 {
		return 0, nil
	}
	cl := s.set.TVDB()
	if cl == nil {
		return 0, nil
	}
	tvEps, err := cl.Episodes(ctx, int(sr.TVDBID), "official")
	if err != nil {
		return 0, err
	}
	byAir := map[string]tvdbEp{}
	byAbs := map[int]tvdbEp{}
	for _, e := range tvEps {
		te := tvdbEp{season: e.SeasonNumber, episode: e.Number}
		if e.AbsoluteNumber != nil {
			te.absolute = *e.AbsoluteNumber
		}
		if e.Aired != "" {
			byAir[e.Aired] = te
		}
		if te.absolute > 0 {
			byAbs[te.absolute] = te
		}
	}
	local, err := s.store.ListEpisodes(ctx, sr.ID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, ep := range local {
		te, ok := byAir[ep.AirDate]
		if !ok || ep.AirDate == "" {
			// Flat TMDB numbering: the episode number is the absolute number.
			te, ok = byAbs[ep.EpisodeNumber]
		}
		if !ok || te.season == 0 {
			continue
		}
		if err := s.store.SetEpisodeSceneNumbers(ctx, ep.ID, te.season, te.episode, te.absolute); err == nil {
			n++
		}
	}
	return n, nil
}

type tvdbEp struct{ season, episode, absolute int }
