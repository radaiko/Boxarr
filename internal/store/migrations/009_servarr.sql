-- +goose Up
CREATE TABLE quality_profile (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    is_default INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE root_folder (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    path       TEXT NOT NULL UNIQUE,
    media_kind TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE tag (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    label      TEXT NOT NULL UNIQUE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Seed stable ids for the Seerr emulation (05). Paths are the BOXARR_*_LIBRARY_ROOT
-- defaults (08); UpsertRootFolder re-points them if the operator overrides.
INSERT INTO quality_profile (id, name, is_default) VALUES (1, 'Any', 1);
INSERT INTO root_folder (id, path, media_kind) VALUES (1, '/data/tv', 'tv');
INSERT INTO root_folder (id, path, media_kind) VALUES (2, '/data/movies', 'movie');

-- +goose Down
DROP TABLE tag;
DROP TABLE root_folder;
DROP TABLE quality_profile;
