package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/radaiko/boxarr/internal/media"
)

// b2i renders a bool as the 0/1 SQLite stores for boolean columns.
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// qualify prefixes every comma-separated column in cols with prefix (e.g. "e.")
// so a column list can be reused in a JOIN without ambiguity.
func qualify(prefix, cols string) string {
	parts := strings.Split(cols, ",")
	for i := range parts {
		parts[i] = prefix + strings.TrimSpace(parts[i])
	}
	return strings.Join(parts, ", ")
}

type scanner interface{ Scan(...any) error }

// ───────────────────────── series ─────────────────────────

const seriesColumns = `id, tmdb_id, tvdb_id, imdb_id, title, sort_title, year,
	overview, series_type, tmdb_status, monitored, season_folder,
	quality_profile_id, root_folder_path, library_path, poster_path,
	backdrop_path, metadata_json, last_metadata_sync, added_at, created_at, updated_at`

func scanSeries(row scanner) (*media.Series, error) {
	var s media.Series
	var (
		tvdbID, year                          sql.NullInt64
		imdb, libPath, poster, backdrop, meta sql.NullString
		lastSync                              sql.NullTime
		monitored, seasonFolder               int
	)
	if err := row.Scan(&s.ID, &s.TMDBID, &tvdbID, &imdb, &s.Title, &s.SortTitle, &year,
		&s.Overview, &s.SeriesType, &s.TMDBStatus, &monitored, &seasonFolder,
		&s.QualityProfileID, &s.RootFolderPath, &libPath, &poster, &backdrop, &meta,
		&lastSync, &s.AddedAt, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	s.TVDBID, s.Year = tvdbID.Int64, int(year.Int64)
	s.IMDBID, s.LibraryPath = imdb.String, libPath.String
	s.PosterPath, s.BackdropPath, s.MetadataJSON = poster.String, backdrop.String, meta.String
	s.Monitored, s.SeasonFolder = monitored != 0, seasonFolder != 0
	if lastSync.Valid {
		s.LastMetadataSync = &lastSync.Time
	}
	return &s, nil
}

// CreateSeries inserts s (caller should GetSeriesByTMDB first; tmdb_id is UNIQUE).
func (s *Store) CreateSeries(ctx context.Context, m *media.Series) (int64, error) {
	if m.SeriesType == "" {
		m.SeriesType = "standard"
	}
	if m.QualityProfileID == 0 {
		m.QualityProfileID = 1
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO series (tmdb_id, tvdb_id, imdb_id, title, sort_title, year,
		 overview, series_type, tmdb_status, monitored, season_folder,
		 quality_profile_id, root_folder_path, library_path, poster_path,
		 backdrop_path, metadata_json, last_metadata_sync)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.TMDBID, nullInt(m.TVDBID), nullStr(m.IMDBID), m.Title, m.SortTitle, nullInt(int64(m.Year)),
		m.Overview, m.SeriesType, m.TMDBStatus, b2i(m.Monitored), b2i(m.SeasonFolder),
		m.QualityProfileID, m.RootFolderPath, nullStr(m.LibraryPath), nullStr(m.PosterPath),
		nullStr(m.BackdropPath), nullStr(m.MetadataJSON), nullTime(m.LastMetadataSync))
	if err != nil {
		return 0, fmt.Errorf("inserting series: %w", err)
	}
	return res.LastInsertId()
}

func (s *Store) seriesWhere(ctx context.Context, where string, args ...any) (*media.Series, error) {
	m, err := scanSeries(s.db.QueryRowContext(ctx,
		`SELECT `+seriesColumns+` FROM series WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying series: %w", err)
	}
	return m, nil
}

// GetSeries loads one series by id (error if absent).
func (s *Store) GetSeries(ctx context.Context, id int64) (*media.Series, error) {
	m, err := s.seriesWhere(ctx, "id=?", id)
	if err == nil && m == nil {
		return nil, fmt.Errorf("series %d not found", id)
	}
	return m, err
}

// GetSeriesByTMDB returns the series with the given TMDB id, or nil if absent.
func (s *Store) GetSeriesByTMDB(ctx context.Context, tmdbID int64) (*media.Series, error) {
	return s.seriesWhere(ctx, "tmdb_id=?", tmdbID)
}

// GetSeriesByTVDB returns the series with the given TVDB id, or nil if absent.
func (s *Store) GetSeriesByTVDB(ctx context.Context, tvdbID int64) (*media.Series, error) {
	return s.seriesWhere(ctx, "tvdb_id=?", tvdbID)
}

// UpdateSeries persists every mutable column of m.
func (s *Store) UpdateSeries(ctx context.Context, m *media.Series) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE series SET tmdb_id=?, tvdb_id=?, imdb_id=?, title=?, sort_title=?,
		 year=?, overview=?, series_type=?, tmdb_status=?, monitored=?, season_folder=?,
		 quality_profile_id=?, root_folder_path=?, library_path=?, poster_path=?,
		 backdrop_path=?, metadata_json=?, last_metadata_sync=?, updated_at=CURRENT_TIMESTAMP
		 WHERE id=?`,
		m.TMDBID, nullInt(m.TVDBID), nullStr(m.IMDBID), m.Title, m.SortTitle,
		nullInt(int64(m.Year)), m.Overview, m.SeriesType, m.TMDBStatus, b2i(m.Monitored),
		b2i(m.SeasonFolder), m.QualityProfileID, m.RootFolderPath, nullStr(m.LibraryPath),
		nullStr(m.PosterPath), nullStr(m.BackdropPath), nullStr(m.MetadataJSON),
		nullTime(m.LastMetadataSync), m.ID)
	if err != nil {
		return fmt.Errorf("updating series %d: %w", m.ID, err)
	}
	return nil
}

// ListSeries returns all series, sorted by sort_title.
func (s *Store) ListSeries(ctx context.Context) ([]*media.Series, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+seriesColumns+` FROM series ORDER BY sort_title`)
	if err != nil {
		return nil, fmt.Errorf("listing series: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*media.Series
	for rows.Next() {
		m, err := scanSeries(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteSeries removes a series; seasons and episodes cascade.
func (s *Store) DeleteSeries(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM series WHERE id=?`, id); err != nil {
		return fmt.Errorf("deleting series %d: %w", id, err)
	}
	return nil
}

// ───────────────────────── season ─────────────────────────

const seasonColumns = `id, series_id, season_number, monitored, episode_count,
	air_date, poster_path, metadata_json, created_at, updated_at`

func scanSeason(row scanner) (*media.Season, error) {
	var m media.Season
	var (
		airDate, poster, meta sql.NullString
		monitored             int
	)
	if err := row.Scan(&m.ID, &m.SeriesID, &m.SeasonNumber, &monitored, &m.EpisodeCount,
		&airDate, &poster, &meta, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	m.Monitored = monitored != 0
	m.AirDate, m.PosterPath, m.MetadataJSON = airDate.String, poster.String, meta.String
	return &m, nil
}

// ResetImportLinks reverts catalog rows imported under a job back to the
// not-acquired state (used to roll back a failed adopt/import): movie and
// episode rows referencing jobID lose has_file/library_path/job_id and return to
// status 'missing'.
func (s *Store) ResetImportLinks(ctx context.Context, jobID int64) error {
	for _, table := range []string{"movie", "episode"} {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE `+table+` SET has_file=0, status='missing', library_path='', job_id=0 WHERE job_id=?`,
			jobID); err != nil {
			return fmt.Errorf("resetting %s import links: %w", table, err)
		}
	}
	return nil
}

// UpsertSeason inserts a season or refreshes its metadata (preserving monitored).
func (s *Store) UpsertSeason(ctx context.Context, m *media.Season) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO season (series_id, season_number, monitored, episode_count,
		 air_date, poster_path, metadata_json)
		 VALUES (?,?,?,?,?,?,?)
		 ON CONFLICT(series_id, season_number) DO UPDATE SET
		   episode_count=excluded.episode_count, air_date=excluded.air_date,
		   poster_path=excluded.poster_path, metadata_json=excluded.metadata_json,
		   updated_at=CURRENT_TIMESTAMP
		 RETURNING id`,
		m.SeriesID, m.SeasonNumber, b2i(m.Monitored), m.EpisodeCount,
		nullStr(m.AirDate), nullStr(m.PosterPath), nullStr(m.MetadataJSON)).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upserting season: %w", err)
	}
	return id, nil
}

// ListSeasons returns a series' seasons, sorted by season number.
func (s *Store) ListSeasons(ctx context.Context, seriesID int64) ([]*media.Season, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+seasonColumns+` FROM season WHERE series_id=? ORDER BY season_number`, seriesID)
	if err != nil {
		return nil, fmt.Errorf("listing seasons: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*media.Season
	for rows.Next() {
		m, err := scanSeason(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetSeasonMonitored flips one season's monitored flag.
func (s *Store) SetSeasonMonitored(ctx context.Context, id int64, monitored bool) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE season SET monitored=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		b2i(monitored), id); err != nil {
		return fmt.Errorf("setting season %d monitored: %w", id, err)
	}
	return nil
}

// ───────────────────────── episode ─────────────────────────

const episodeColumns = `id, series_id, season_id, season_number, episode_number,
	absolute_number, tmdb_id, tvdb_id, title, overview, air_date, runtime,
	still_path, status, monitored, has_file, job_id, library_path,
	metadata_json, last_searched_at, created_at, updated_at, lang_missing,
	scene_season, scene_episode`

func scanEpisode(row scanner) (*media.Episode, error) {
	var m media.Episode
	var (
		absNum, tmdbID, tvdbID, runtime, jobID sql.NullInt64
		airDate, still, libPath, meta          sql.NullString
		lastSearched                           sql.NullTime
		status                                 string
		monitored, hasFile, langMissing        int
	)
	if err := row.Scan(&m.ID, &m.SeriesID, &m.SeasonID, &m.SeasonNumber, &m.EpisodeNumber,
		&absNum, &tmdbID, &tvdbID, &m.Title, &m.Overview, &airDate, &runtime,
		&still, &status, &monitored, &hasFile, &jobID, &libPath, &meta,
		&lastSearched, &m.CreatedAt, &m.UpdatedAt, &langMissing,
		&m.SceneSeason, &m.SceneEpisode); err != nil {
		return nil, err
	}
	m.AbsoluteNumber, m.TMDBID, m.TVDBID = int(absNum.Int64), tmdbID.Int64, tvdbID.Int64
	m.Runtime, m.JobID = int(runtime.Int64), jobID.Int64
	m.AirDate, m.StillPath, m.LibraryPath, m.MetadataJSON = airDate.String, still.String, libPath.String, meta.String
	if lastSearched.Valid {
		m.LastSearchedAt = &lastSearched.Time
	}
	m.Status = media.MediaStatus(status)
	m.Monitored, m.HasFile, m.LangMissing = monitored != 0, hasFile != 0, langMissing != 0
	return &m, nil
}

// SetEpisodeLangMissing flags whether an episode lacks an acceptable language.
func (s *Store) SetEpisodeLangMissing(ctx context.Context, id int64, missing bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE episode SET lang_missing=? WHERE id=?`, b2iCat(missing), id)
	if err != nil {
		return fmt.Errorf("setting episode lang_missing: %w", err)
	}
	return nil
}

// SetMovieLangMissing flags whether a movie lacks an acceptable language.
func (s *Store) SetMovieLangMissing(ctx context.Context, id int64, missing bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE movie SET lang_missing=? WHERE id=?`, b2iCat(missing), id)
	if err != nil {
		return fmt.Errorf("setting movie lang_missing: %w", err)
	}
	return nil
}

// LangMissingCounts returns how many movies and episodes are flagged as lacking an
// acceptable language (the Plex stream-check found no required/preferred track) —
// the dashboard "wrong language" summary.
func (s *Store) LangMissingCounts(ctx context.Context) (movies, episodes int64, err error) {
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM movie WHERE lang_missing=1`).Scan(&movies); err != nil {
		return 0, 0, fmt.Errorf("counting lang-missing movies: %w", err)
	}
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM episode WHERE lang_missing=1`).Scan(&episodes); err != nil {
		return 0, 0, fmt.Errorf("counting lang-missing episodes: %w", err)
	}
	return movies, episodes, nil
}

func b2iCat(b bool) int {
	if b {
		return 1
	}
	return 0
}

// MarkEpisodesSearched stamps last_searched_at=now on the given episodes.
func (s *Store) MarkEpisodesSearched(ctx context.Context, ids ...int64) error {
	for _, id := range ids {
		if _, err := s.db.ExecContext(ctx,
			`UPDATE episode SET last_searched_at=CURRENT_TIMESTAMP WHERE id=?`, id); err != nil {
			return fmt.Errorf("marking episode searched: %w", err)
		}
	}
	return nil
}

// UpsertEpisode inserts an episode or refreshes its metadata. The DO UPDATE path
// deliberately preserves the lifecycle columns (status, monitored, has_file,
// job_id, library_path) so a metadata sync never clobbers acquisition state.
func (s *Store) UpsertEpisode(ctx context.Context, m *media.Episode) (int64, error) {
	st := m.Status
	if st == "" {
		st = media.MediaMissing
	}
	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO episode (series_id, season_id, season_number, episode_number,
		 absolute_number, tmdb_id, tvdb_id, title, overview, air_date, runtime,
		 still_path, status, monitored, has_file, job_id, library_path, metadata_json)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(series_id, season_number, episode_number) DO UPDATE SET
		   season_id=excluded.season_id, absolute_number=excluded.absolute_number,
		   tmdb_id=excluded.tmdb_id, tvdb_id=excluded.tvdb_id, title=excluded.title,
		   overview=excluded.overview, air_date=excluded.air_date, runtime=excluded.runtime,
		   still_path=excluded.still_path, metadata_json=excluded.metadata_json,
		   updated_at=CURRENT_TIMESTAMP
		 RETURNING id`,
		m.SeriesID, m.SeasonID, m.SeasonNumber, m.EpisodeNumber, nullInt(int64(m.AbsoluteNumber)),
		nullInt(m.TMDBID), nullInt(m.TVDBID), m.Title, m.Overview, nullStr(m.AirDate),
		nullInt(int64(m.Runtime)), nullStr(m.StillPath), string(st), b2i(m.Monitored),
		b2i(m.HasFile), nullInt(m.JobID), nullStr(m.LibraryPath), nullStr(m.MetadataJSON)).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upserting episode: %w", err)
	}
	return id, nil
}

// GetEpisode loads one episode by id (error if absent).
func (s *Store) GetEpisode(ctx context.Context, id int64) (*media.Episode, error) {
	m, err := scanEpisode(s.db.QueryRowContext(ctx, `SELECT `+episodeColumns+` FROM episode WHERE id=?`, id))
	if err != nil {
		return nil, fmt.Errorf("getting episode %d: %w", id, err)
	}
	return m, nil
}

// ListEpisodes returns a series' episodes, sorted by season then episode number.
func (s *Store) ListEpisodes(ctx context.Context, seriesID int64) ([]*media.Episode, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+episodeColumns+` FROM episode WHERE series_id=? ORDER BY season_number, episode_number`, seriesID)
	if err != nil {
		return nil, fmt.Errorf("listing episodes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*media.Episode
	for rows.Next() {
		m, err := scanEpisode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// UpdateEpisode persists every mutable column of m (including lifecycle columns).
func (s *Store) UpdateEpisode(ctx context.Context, m *media.Episode) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE episode SET season_id=?, season_number=?, episode_number=?, absolute_number=?,
		 tmdb_id=?, tvdb_id=?, title=?, overview=?, air_date=?, runtime=?, still_path=?,
		 status=?, monitored=?, has_file=?, job_id=?, library_path=?, metadata_json=?,
		 updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		m.SeasonID, m.SeasonNumber, m.EpisodeNumber, nullInt(int64(m.AbsoluteNumber)),
		nullInt(m.TMDBID), nullInt(m.TVDBID), m.Title, m.Overview, nullStr(m.AirDate),
		nullInt(int64(m.Runtime)), nullStr(m.StillPath), string(m.Status), b2i(m.Monitored),
		b2i(m.HasFile), nullInt(m.JobID), nullStr(m.LibraryPath), nullStr(m.MetadataJSON), m.ID)
	if err != nil {
		return fmt.Errorf("updating episode %d: %w", m.ID, err)
	}
	return nil
}

// SetEpisodeSceneNumbers stores the TVDB-derived scene season/episode + absolute
// number for an episode (used by the anime search to map TMDB's flat numbering).
func (s *Store) SetEpisodeSceneNumbers(ctx context.Context, id int64, season, episode, absolute int) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE episode SET scene_season=?, scene_episode=?, absolute_number=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		season, episode, nullInt(int64(absolute)), id); err != nil {
		return fmt.Errorf("setting episode scene numbers: %w", err)
	}
	return nil
}

// SetEpisodeStatus is the narrow, targeted status flip the reconciler uses.
func (s *Store) SetEpisodeStatus(ctx context.Context, id int64, st media.MediaStatus) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE episode SET status=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		string(st), id); err != nil {
		return fmt.Errorf("setting episode %d status: %w", id, err)
	}
	return nil
}

// WantedEpisodes returns monitored, file-less, aired episodes whose series and
// season are also monitored (air-date-aware via lexicographic ISO compare).
func (s *Store) WantedEpisodes(ctx context.Context) ([]*media.Episode, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+qualify("e.", episodeColumns)+` FROM episode e
		 JOIN series sr ON sr.id = e.series_id
		 JOIN season sn ON sn.id = e.season_id
		 WHERE e.monitored=1 AND sn.monitored=1 AND sr.monitored=1
		   AND e.has_file=0
		   AND e.status IN ('wanted','missing','expired_broken')
		   AND e.air_date IS NOT NULL AND e.air_date <> '' AND e.air_date <= date('now')
		 ORDER BY e.air_date`)
	if err != nil {
		return nil, fmt.Errorf("querying wanted episodes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*media.Episode
	for rows.Next() {
		m, err := scanEpisode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ───────────────────────── movie ─────────────────────────

const movieColumns = `id, tmdb_id, imdb_id, title, sort_title, year, overview,
	tmdb_status, minimum_availability, release_date, digital_release,
	physical_release, runtime, status, monitored, has_file, quality_profile_id,
	root_folder_path, library_path, job_id, poster_path, backdrop_path,
	metadata_json, last_metadata_sync, added_at, created_at, updated_at, last_searched_at, lang_missing,
	alt_titles`

func scanMovie(row scanner) (*media.Movie, error) {
	var m media.Movie
	var (
		year, runtime, jobID                              sql.NullInt64
		imdb, relDate, digital, physical, libPath, poster sql.NullString
		backdrop, meta                                    sql.NullString
		lastSync, lastSearched                            sql.NullTime
		status                                            string
		monitored, hasFile, langMissing                   int
		altTitles                                         sql.NullString
	)
	if err := row.Scan(&m.ID, &m.TMDBID, &imdb, &m.Title, &m.SortTitle, &year, &m.Overview,
		&m.TMDBStatus, &m.MinimumAvailability, &relDate, &digital, &physical, &runtime,
		&status, &monitored, &hasFile, &m.QualityProfileID, &m.RootFolderPath, &libPath,
		&jobID, &poster, &backdrop, &meta, &lastSync, &m.AddedAt, &m.CreatedAt, &m.UpdatedAt, &lastSearched, &langMissing,
		&altTitles); err != nil {
		return nil, err
	}
	if altTitles.String != "" {
		m.AltTitles = strings.Split(altTitles.String, "\n")
	}
	m.Year, m.Runtime, m.JobID = int(year.Int64), int(runtime.Int64), jobID.Int64
	m.IMDBID, m.ReleaseDate, m.DigitalRelease, m.PhysicalRelease = imdb.String, relDate.String, digital.String, physical.String
	m.LibraryPath, m.PosterPath, m.BackdropPath, m.MetadataJSON = libPath.String, poster.String, backdrop.String, meta.String
	m.Status = media.MediaStatus(status)
	m.Monitored, m.HasFile, m.LangMissing = monitored != 0, hasFile != 0, langMissing != 0
	if lastSync.Valid {
		m.LastMetadataSync = &lastSync.Time
	}
	if lastSearched.Valid {
		m.LastSearchedAt = &lastSearched.Time
	}
	return &m, nil
}

// SetMovieAltTitles stores a movie's alternative/original titles (newline-joined)
// for cross-language mount matching.
func (s *Store) SetMovieAltTitles(ctx context.Context, id int64, titles []string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE movie SET alt_titles=? WHERE id=?`, strings.Join(titles, "\n"), id); err != nil {
		return fmt.Errorf("setting movie alt titles: %w", err)
	}
	return nil
}

// MarkMovieSearched stamps last_searched_at=now on a movie (for the search cadence).
func (s *Store) MarkMovieSearched(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE movie SET last_searched_at=CURRENT_TIMESTAMP WHERE id=?`, id); err != nil {
		return fmt.Errorf("marking movie searched: %w", err)
	}
	return nil
}

// CreateMovie inserts m (caller should GetMovieByTMDB first; tmdb_id is UNIQUE).
func (s *Store) CreateMovie(ctx context.Context, m *media.Movie) (int64, error) {
	if m.MinimumAvailability == "" {
		m.MinimumAvailability = "released"
	}
	if m.QualityProfileID == 0 {
		m.QualityProfileID = 1
	}
	if m.Status == "" {
		m.Status = media.MediaMissing
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO movie (tmdb_id, imdb_id, title, sort_title, year, overview,
		 tmdb_status, minimum_availability, release_date, digital_release,
		 physical_release, runtime, status, monitored, has_file, quality_profile_id,
		 root_folder_path, library_path, job_id, poster_path, backdrop_path,
		 metadata_json, last_metadata_sync)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.TMDBID, nullStr(m.IMDBID), m.Title, m.SortTitle, nullInt(int64(m.Year)), m.Overview,
		m.TMDBStatus, m.MinimumAvailability, nullStr(m.ReleaseDate), nullStr(m.DigitalRelease),
		nullStr(m.PhysicalRelease), nullInt(int64(m.Runtime)), string(m.Status), b2i(m.Monitored),
		b2i(m.HasFile), m.QualityProfileID, m.RootFolderPath, nullStr(m.LibraryPath), nullInt(m.JobID),
		nullStr(m.PosterPath), nullStr(m.BackdropPath), nullStr(m.MetadataJSON), nullTime(m.LastMetadataSync))
	if err != nil {
		return 0, fmt.Errorf("inserting movie: %w", err)
	}
	return res.LastInsertId()
}

func (s *Store) movieWhere(ctx context.Context, where string, args ...any) (*media.Movie, error) {
	m, err := scanMovie(s.db.QueryRowContext(ctx, `SELECT `+movieColumns+` FROM movie WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying movie: %w", err)
	}
	return m, nil
}

// GetMovie loads one movie by id (error if absent).
func (s *Store) GetMovie(ctx context.Context, id int64) (*media.Movie, error) {
	m, err := s.movieWhere(ctx, "id=?", id)
	if err == nil && m == nil {
		return nil, fmt.Errorf("movie %d not found", id)
	}
	return m, err
}

// GetMovieByTMDB returns the movie with the given TMDB id, or nil if absent.
func (s *Store) GetMovieByTMDB(ctx context.Context, tmdbID int64) (*media.Movie, error) {
	return s.movieWhere(ctx, "tmdb_id=?", tmdbID)
}

// UpdateMovie persists every mutable column of m.
func (s *Store) UpdateMovie(ctx context.Context, m *media.Movie) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE movie SET tmdb_id=?, imdb_id=?, title=?, sort_title=?, year=?, overview=?,
		 tmdb_status=?, minimum_availability=?, release_date=?, digital_release=?,
		 physical_release=?, runtime=?, status=?, monitored=?, has_file=?, quality_profile_id=?,
		 root_folder_path=?, library_path=?, job_id=?, poster_path=?, backdrop_path=?,
		 metadata_json=?, last_metadata_sync=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		m.TMDBID, nullStr(m.IMDBID), m.Title, m.SortTitle, nullInt(int64(m.Year)), m.Overview,
		m.TMDBStatus, m.MinimumAvailability, nullStr(m.ReleaseDate), nullStr(m.DigitalRelease),
		nullStr(m.PhysicalRelease), nullInt(int64(m.Runtime)), string(m.Status), b2i(m.Monitored),
		b2i(m.HasFile), m.QualityProfileID, m.RootFolderPath, nullStr(m.LibraryPath), nullInt(m.JobID),
		nullStr(m.PosterPath), nullStr(m.BackdropPath), nullStr(m.MetadataJSON), nullTime(m.LastMetadataSync), m.ID)
	if err != nil {
		return fmt.Errorf("updating movie %d: %w", m.ID, err)
	}
	return nil
}

// ListMovies returns all movies, sorted by sort_title.
func (s *Store) ListMovies(ctx context.Context) ([]*media.Movie, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+movieColumns+` FROM movie ORDER BY sort_title`)
	if err != nil {
		return nil, fmt.Errorf("listing movies: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*media.Movie
	for rows.Next() {
		m, err := scanMovie(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteMovie removes one movie row.
func (s *Store) DeleteMovie(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM movie WHERE id=?`, id); err != nil {
		return fmt.Errorf("deleting movie %d: %w", id, err)
	}
	return nil
}

// SetMovieStatus is the narrow, targeted status flip the reconciler uses.
func (s *Store) SetMovieStatus(ctx context.Context, id int64, st media.MediaStatus) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE movie SET status=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		string(st), id); err != nil {
		return fmt.Errorf("setting movie %d status: %w", id, err)
	}
	return nil
}

// WantedMovies returns monitored, file-less, released movies (air-date-aware).
func (s *Store) WantedMovies(ctx context.Context) ([]*media.Movie, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+movieColumns+` FROM movie
		 WHERE monitored=1 AND has_file=0 AND status IN ('wanted','missing','expired_broken')
		   AND release_date IS NOT NULL AND release_date <> '' AND release_date <= date('now')
		 ORDER BY release_date`)
	if err != nil {
		return nil, fmt.Errorf("querying wanted movies: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*media.Movie
	for rows.Next() {
		m, err := scanMovie(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
