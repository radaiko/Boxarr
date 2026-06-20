-- +goose Up
CREATE TABLE webdav_item (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    remote_path TEXT NOT NULL UNIQUE,
    size        INTEGER NOT NULL DEFAULT 0,
    category    TEXT NOT NULL DEFAULT 'unknown',
    known       INTEGER NOT NULL DEFAULT 0,
    job_id      INTEGER REFERENCES jobs(id) ON DELETE SET NULL,
    is_broken   INTEGER NOT NULL DEFAULT 0,
    first_seen  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_webdav_item_known ON webdav_item(known);
CREATE INDEX idx_webdav_item_category ON webdav_item(category);
CREATE INDEX idx_webdav_item_job ON webdav_item(job_id);

-- +goose Down
DROP TABLE webdav_item;
