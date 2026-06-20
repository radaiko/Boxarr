-- +goose Up
CREATE TABLE series (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    tmdb_id            INTEGER NOT NULL,
    tvdb_id            INTEGER,
    imdb_id            TEXT,
    title              TEXT NOT NULL,
    sort_title         TEXT NOT NULL DEFAULT '',
    year               INTEGER,
    overview           TEXT NOT NULL DEFAULT '',
    series_type        TEXT NOT NULL DEFAULT 'standard',
    tmdb_status        TEXT NOT NULL DEFAULT '',
    monitored          INTEGER NOT NULL DEFAULT 1,
    season_folder      INTEGER NOT NULL DEFAULT 1,
    quality_profile_id INTEGER NOT NULL DEFAULT 1,
    root_folder_path   TEXT NOT NULL DEFAULT '',
    library_path       TEXT,
    poster_path        TEXT,
    backdrop_path      TEXT,
    metadata_json      TEXT,
    last_metadata_sync TIMESTAMP,
    added_at           TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_series_tmdb ON series(tmdb_id);
CREATE INDEX idx_series_tvdb ON series(tvdb_id);
CREATE INDEX idx_series_monitored ON series(monitored);

CREATE TABLE season (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id     INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    season_number INTEGER NOT NULL,
    monitored     INTEGER NOT NULL DEFAULT 1,
    episode_count INTEGER NOT NULL DEFAULT 0,
    air_date      TEXT,
    poster_path   TEXT,
    metadata_json TEXT,
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_season_series_num ON season(series_id, season_number);

CREATE TABLE episode (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id       INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    season_id       INTEGER NOT NULL REFERENCES season(id) ON DELETE CASCADE,
    season_number   INTEGER NOT NULL,
    episode_number  INTEGER NOT NULL,
    absolute_number INTEGER,
    tmdb_id         INTEGER,
    tvdb_id         INTEGER,
    title           TEXT NOT NULL DEFAULT '',
    overview        TEXT NOT NULL DEFAULT '',
    air_date        TEXT,
    runtime         INTEGER,
    still_path      TEXT,
    status          TEXT NOT NULL DEFAULT 'missing',
    monitored       INTEGER NOT NULL DEFAULT 1,
    has_file        INTEGER NOT NULL DEFAULT 0,
    job_id          INTEGER REFERENCES jobs(id) ON DELETE SET NULL,
    library_path    TEXT,
    metadata_json   TEXT,
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_episode_series_se ON episode(series_id, season_number, episode_number);
CREATE INDEX idx_episode_season ON episode(season_id);
CREATE INDEX idx_episode_status ON episode(status);
CREATE INDEX idx_episode_job ON episode(job_id);
CREATE INDEX idx_episode_absolute ON episode(series_id, absolute_number);

CREATE TABLE movie (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    tmdb_id              INTEGER NOT NULL,
    imdb_id              TEXT,
    title                TEXT NOT NULL,
    sort_title           TEXT NOT NULL DEFAULT '',
    year                 INTEGER,
    overview             TEXT NOT NULL DEFAULT '',
    tmdb_status          TEXT NOT NULL DEFAULT '',
    minimum_availability TEXT NOT NULL DEFAULT 'released',
    release_date         TEXT,
    digital_release      TEXT,
    physical_release     TEXT,
    runtime              INTEGER,
    status               TEXT NOT NULL DEFAULT 'missing',
    monitored            INTEGER NOT NULL DEFAULT 1,
    has_file             INTEGER NOT NULL DEFAULT 0,
    quality_profile_id   INTEGER NOT NULL DEFAULT 1,
    root_folder_path     TEXT NOT NULL DEFAULT '',
    library_path         TEXT,
    job_id               INTEGER REFERENCES jobs(id) ON DELETE SET NULL,
    poster_path          TEXT,
    backdrop_path        TEXT,
    metadata_json        TEXT,
    last_metadata_sync   TIMESTAMP,
    added_at             TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at           TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_movie_tmdb ON movie(tmdb_id);
CREATE INDEX idx_movie_imdb ON movie(imdb_id);
CREATE INDEX idx_movie_status ON movie(status);
CREATE INDEX idx_movie_monitored ON movie(monitored);
CREATE INDEX idx_movie_job ON movie(job_id);

-- +goose Down
DROP TABLE movie;
DROP TABLE episode;
DROP TABLE season;
DROP TABLE series;
