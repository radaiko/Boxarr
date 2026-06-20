// Package catalog ingests titles from TMDB into Boxarr's store (the "what the
// user wants" layer). Phase 1 covers movies; series ingest lands in Phase 2.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/metadata/tmdb"
	"github.com/radaiko/boxarr/internal/store"
)

// ErrAlreadyExists is returned by AddMovie when the TMDB id is already cataloged.
var ErrAlreadyExists = errors.New("already in catalog")

// Service ingests TMDB titles into the store.
type Service struct {
	store *store.Store
	tmdb  *tmdb.Client
	cfg   *config.Config
}

// New constructs a catalog Service.
func New(st *store.Store, t *tmdb.Client, cfg *config.Config) *Service {
	return &Service{store: st, tmdb: t, cfg: cfg}
}

// MovieCandidate is a lookup result the SPA can add.
type MovieCandidate struct {
	TMDBID     int64  `json:"tmdbId"`
	Title      string `json:"title"`
	Year       int    `json:"year"`
	Overview   string `json:"overview"`
	PosterPath string `json:"posterPath"`
	InLibrary  bool   `json:"inLibrary"`
	LibraryID  int64  `json:"libraryId,omitempty"`
}

// LookupMovies resolves a search term to candidates. The term may be free text,
// "tmdb:{id}", or "imdb:{id}" (the latter falls through to a TMDB title search).
func (s *Service) LookupMovies(ctx context.Context, term string) ([]MovieCandidate, error) {
	term = strings.TrimSpace(term)
	if rest, ok := strings.CutPrefix(term, "tmdb:"); ok {
		id, err := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid tmdb id %q", rest)
		}
		d, err := s.tmdb.MovieDetails(ctx, int(id))
		if err != nil {
			return nil, err
		}
		c := MovieCandidate{TMDBID: int64(d.ID), Title: d.Title, Year: yearOf(d.ReleaseDate),
			Overview: d.Overview, PosterPath: d.PosterPath}
		s.markInLibrary(ctx, &c)
		return []MovieCandidate{c}, nil
	}
	results, err := s.tmdb.SearchMovie(ctx, strings.TrimPrefix(term, "imdb:"), 0)
	if err != nil {
		return nil, err
	}
	out := make([]MovieCandidate, 0, len(results))
	for _, r := range results {
		c := MovieCandidate{TMDBID: int64(r.ID), Title: r.Title, Year: yearOf(r.ReleaseDate),
			Overview: r.Overview, PosterPath: r.PosterPath}
		s.markInLibrary(ctx, &c)
		out = append(out, c)
	}
	return out, nil
}

func (s *Service) markInLibrary(ctx context.Context, c *MovieCandidate) {
	if m, _ := s.store.GetMovieByTMDB(ctx, c.TMDBID); m != nil {
		c.InLibrary, c.LibraryID = true, m.ID
	}
}

// AddMovie ingests a movie by TMDB id and returns the created catalog row.
// Returns ErrAlreadyExists if the TMDB id is already present.
func (s *Service) AddMovie(ctx context.Context, tmdbID int64, monitored bool) (*media.Movie, error) {
	if existing, _ := s.store.GetMovieByTMDB(ctx, tmdbID); existing != nil {
		return existing, ErrAlreadyExists
	}
	d, err := s.tmdb.MovieDetails(ctx, int(tmdbID))
	if err != nil {
		return nil, fmt.Errorf("tmdb movie details: %w", err)
	}
	m := &media.Movie{
		TMDBID:              int64(d.ID),
		IMDBID:              d.IMDBID,
		Title:               d.Title,
		SortTitle:           strings.ToLower(d.Title),
		Year:                yearOf(d.ReleaseDate),
		Overview:            d.Overview,
		TMDBStatus:          d.Status,
		MinimumAvailability: "released",
		ReleaseDate:         d.ReleaseDate,
		Runtime:             d.Runtime,
		Monitored:           monitored,
		QualityProfileID:    1,
		RootFolderPath:      s.cfg.MovieLibraryRoot,
		PosterPath:          d.PosterPath,
		BackdropPath:        d.BackdropPath,
	}
	m.Status = movieStatus(m)
	id, err := s.store.CreateMovie(ctx, m)
	if err != nil {
		return nil, fmt.Errorf("creating movie: %w", err)
	}
	m.ID = id
	return m, nil
}

// movieStatus computes the initial MediaStatus: wanted when monitored + released,
// else missing (air-date-aware via lexicographic ISO compare).
func movieStatus(m *media.Movie) media.MediaStatus {
	if m.Monitored && m.ReleaseDate != "" && m.ReleaseDate <= time.Now().UTC().Format("2006-01-02") {
		return media.MediaWanted
	}
	return media.MediaMissing
}

func yearOf(date string) int {
	if len(date) >= 4 {
		if y, err := strconv.Atoi(date[:4]); err == nil {
			return y
		}
	}
	return 0
}
