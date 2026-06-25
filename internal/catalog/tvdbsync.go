package catalog

import (
	"context"
	"fmt"

	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/metadata/tvdb"
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
	local, err := s.store.ListEpisodes(ctx, sr.ID)
	if err != nil {
		return 0, err
	}
	n := 0
	for id, te := range resolveSceneMap(tvEps, local) {
		if err := s.store.SetEpisodeSceneNumbers(ctx, id, te.season, te.episode, te.absolute); err == nil {
			n++
		}
	}
	return n, nil
}

type tvdbEp struct{ season, episode, absolute int }

// resolveSceneMap maps each local (TMDB-numbered) episode to its TheTVDB
// (season, episode, absolute). It matches by, in order:
//  1. a UNIQUE air date — reliable for weekly shows + anime;
//  2. direct season+episode — standard series whose seasons align with TVDB;
//  3. absolute number — flat TMDB numbering (anime: episode number == absolute).
//
// Air dates shared by more than one TVDB episode (a binge-drop season released on
// one day) are dropped from the air-date index: otherwise every local episode
// collapses onto the last TVDB episode with that date (the cause of "all episodes
// show S01E04 / point to one file").
func resolveSceneMap(tvEps []tvdb.Episode, local []*media.Episode) map[int64]tvdbEp {
	byAir := map[string]tvdbEp{}
	airCount := map[string]int{}
	byAbs := map[int]tvdbEp{}
	byNum := map[[2]int]tvdbEp{}
	for _, e := range tvEps {
		te := tvdbEp{season: e.SeasonNumber, episode: e.Number}
		if e.AbsoluteNumber != nil {
			te.absolute = *e.AbsoluteNumber
		}
		if e.Aired != "" {
			byAir[e.Aired] = te
			airCount[e.Aired]++
		}
		if te.absolute > 0 {
			byAbs[te.absolute] = te
		}
		byNum[[2]int{e.SeasonNumber, e.Number}] = te
	}
	for date, c := range airCount {
		if c > 1 {
			delete(byAir, date) // ambiguous: a binge-drop season shares one date
		}
	}
	out := make(map[int64]tvdbEp, len(local))
	for _, ep := range local {
		var te tvdbEp
		var ok bool
		if ep.AirDate != "" {
			te, ok = byAir[ep.AirDate]
		}
		if !ok {
			te, ok = byNum[[2]int{ep.SeasonNumber, ep.EpisodeNumber}]
		}
		if !ok {
			te, ok = byAbs[ep.EpisodeNumber]
		}
		if !ok || te.season == 0 {
			continue
		}
		out[ep.ID] = te
	}
	return out
}
