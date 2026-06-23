package catalog

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/metadata/tmdb"
)

// SeriesCandidate is a TV lookup result the SPA can add.
type SeriesCandidate struct {
	TMDBID     int64  `json:"tmdbId"`
	TVDBID     int64  `json:"tvdbId,omitempty"`
	Title      string `json:"title"`
	Year       int    `json:"year"`
	Overview   string `json:"overview"`
	PosterPath string `json:"posterPath"`
	InLibrary  bool   `json:"inLibrary"`
	LibraryID  int64  `json:"libraryId,omitempty"`
}

// LookupSeries resolves a term to TV candidates. The term may be free text,
// "tmdb:{id}", or "tvdb:{id}" (resolved to TMDB via /find).
func (s *Service) LookupSeries(ctx context.Context, term string) ([]SeriesCandidate, error) {
	term = strings.TrimSpace(term)
	if rest, ok := strings.CutPrefix(term, "tvdb:"); ok {
		id, err := strconv.Atoi(strings.TrimSpace(rest))
		if err != nil {
			return nil, fmt.Errorf("invalid tvdb id %q", rest)
		}
		found, ferr := s.set.TMDB().FindByTVDB(ctx, id)
		if ferr != nil {
			return nil, ferr
		}
		if len(found.TVResults) == 0 {
			return nil, nil
		}
		return s.tvCandidate(ctx, int64(found.TVResults[0].ID))
	}
	if rest, ok := strings.CutPrefix(term, "tmdb:"); ok {
		id, err := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid tmdb id %q", rest)
		}
		return s.tvCandidate(ctx, id)
	}
	results, err := s.set.TMDB().SearchTV(ctx, term, 0)
	if err != nil {
		return nil, err
	}
	out := make([]SeriesCandidate, 0, len(results))
	for _, r := range results {
		c := SeriesCandidate{TMDBID: int64(r.ID), Title: r.Name, Year: yearOf(r.FirstAirDate),
			Overview: r.Overview, PosterPath: r.PosterPath}
		s.markSeriesInLibrary(ctx, &c)
		out = append(out, c)
	}
	return out, nil
}

func (s *Service) tvCandidate(ctx context.Context, tmdbID int64) ([]SeriesCandidate, error) {
	d, err := s.set.TMDB().TVDetails(ctx, int(tmdbID))
	if err != nil {
		return nil, err
	}
	c := SeriesCandidate{TMDBID: int64(d.ID), TVDBID: int64(d.ExternalIDs.TVDBID), Title: d.Name,
		Year: yearOf(d.FirstAirDate), Overview: d.Overview, PosterPath: d.PosterPath}
	s.markSeriesInLibrary(ctx, &c)
	return []SeriesCandidate{c}, nil
}

func (s *Service) markSeriesInLibrary(ctx context.Context, c *SeriesCandidate) {
	if sr, _ := s.store.GetSeriesByTMDB(ctx, c.TMDBID); sr != nil {
		c.InLibrary, c.LibraryID = true, sr.ID
	}
}

// AddSeries ingests a series by TMDB id (resolving TVDB cross-id), persisting
// its seasons and episodes from TMDB. monitoredSeasons (nil = all aired seasons)
// controls which seasons are monitored. Returns the created series.
func (s *Service) AddSeries(ctx context.Context, tmdbID int64, monitored bool, monitoredSeasons []int, seriesType string) (*media.Series, error) {
	if existing, _ := s.store.GetSeriesByTMDB(ctx, tmdbID); existing != nil {
		return existing, ErrAlreadyExists
	}
	d, err := s.set.TMDB().TVDetails(ctx, int(tmdbID))
	if err != nil {
		return nil, fmt.Errorf("tmdb tv details: %w", err)
	}
	// Anime is a series subtype with its own library root; everything else is
	// "standard" and lands in the TV library.
	root := s.set.TVLibraryRoot()
	if seriesType != "anime" {
		seriesType = "standard"
	} else {
		root = s.set.AnimeLibraryRoot()
	}
	sr := &media.Series{
		TMDBID: int64(d.ID), TVDBID: int64(d.ExternalIDs.TVDBID), IMDBID: d.ExternalIDs.IMDBID,
		Title: d.Name, SortTitle: strings.ToLower(d.Name), Year: yearOf(d.FirstAirDate),
		Overview: d.Overview, SeriesType: seriesType, TMDBStatus: d.Status,
		Monitored: monitored, SeasonFolder: true, QualityProfileID: 1,
		RootFolderPath: root, PosterPath: d.PosterPath, BackdropPath: d.BackdropPath,
	}
	now := time.Now()
	sr.LastMetadataSync = &now
	sid, err := s.store.CreateSeries(ctx, sr)
	if err != nil {
		// Lost a create race on the unique tmdb_id index (concurrent adopt): if a
		// row now exists, treat it as already-present rather than a hard error.
		if existing, _ := s.store.GetSeriesByTMDB(ctx, tmdbID); existing != nil {
			return existing, ErrAlreadyExists
		}
		return nil, fmt.Errorf("creating series: %w", err)
	}
	sr.ID = sid

	monSet := map[int]bool{}
	for _, n := range monitoredSeasons {
		monSet[n] = true
	}
	if err := s.syncSeasons(ctx, sr, d.Seasons, monitoredSeasons == nil, monSet); err != nil {
		return nil, err
	}
	// Best-effort: pull authoritative scene/absolute numbering from TheTVDB so the
	// anime search uses real data from the start (no-op without a TVDB key/id).
	if _, err := s.refreshSeriesFromTVDB(ctx, sr); err != nil {
		s.logSearchErr("tvdb refresh "+sr.Title, err)
	}
	return sr, nil
}

// syncSeasons fetches each season's episodes from TMDB and upserts them. When
// monitorAll is true every non-special season is monitored; otherwise only the
// season numbers in monSet are.
func (s *Service) syncSeasons(ctx context.Context, sr *media.Series, seasons []tmdb.SeasonSummary, monitorAll bool, monSet map[int]bool) error {
	for _, ss := range seasons {
		if ss.SeasonNumber == 0 {
			continue // skip Specials by default
		}
		monitored := monitorAll || monSet[ss.SeasonNumber]
		seasonID, err := s.store.UpsertSeason(ctx, &media.Season{
			SeriesID: sr.ID, SeasonNumber: ss.SeasonNumber, Monitored: monitored,
			EpisodeCount: ss.EpisodeCount, AirDate: ss.AirDate, PosterPath: ss.PosterPath,
		})
		if err != nil {
			return fmt.Errorf("upserting season %d: %w", ss.SeasonNumber, err)
		}
		sd, err := s.set.TMDB().TVSeason(ctx, int(sr.TMDBID), ss.SeasonNumber)
		if err != nil {
			return fmt.Errorf("tmdb season %d: %w", ss.SeasonNumber, err)
		}
		for _, ep := range sd.Episodes {
			status := media.MediaMissing
			if monitored && ep.AirDate != "" && ep.AirDate <= time.Now().UTC().Format("2006-01-02") {
				status = media.MediaWanted
			}
			if _, err := s.store.UpsertEpisode(ctx, &media.Episode{
				SeriesID: sr.ID, SeasonID: seasonID, SeasonNumber: ep.SeasonNumber,
				EpisodeNumber: ep.EpisodeNumber, Title: ep.Name, Overview: ep.Overview,
				AirDate: ep.AirDate, Runtime: ep.Runtime, StillPath: ep.StillPath,
				Monitored: monitored, Status: status,
			}); err != nil {
				return fmt.Errorf("upserting episode S%dE%d: %w", ep.SeasonNumber, ep.EpisodeNumber, err)
			}
		}
	}
	return nil
}
