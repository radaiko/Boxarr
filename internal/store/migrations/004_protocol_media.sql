-- +goose Up
ALTER TABLE jobs ADD COLUMN protocol TEXT NOT NULL DEFAULT 'usenet';
ALTER TABLE jobs ADD COLUMN media_type TEXT;
ALTER TABLE jobs ADD COLUMN media_ref INTEGER;
ALTER TABLE jobs ADD COLUMN torrent_magnet TEXT;
ALTER TABLE jobs ADD COLUMN torrent_hash TEXT;
ALTER TABLE jobs ADD COLUMN torrent_file BLOB;

CREATE INDEX idx_jobs_torrent_hash ON jobs(torrent_hash);
CREATE INDEX idx_jobs_media ON jobs(media_type, media_ref);

-- +goose Down
DROP INDEX idx_jobs_media;
DROP INDEX idx_jobs_torrent_hash;
ALTER TABLE jobs DROP COLUMN torrent_file;
ALTER TABLE jobs DROP COLUMN torrent_hash;
ALTER TABLE jobs DROP COLUMN torrent_magnet;
ALTER TABLE jobs DROP COLUMN media_ref;
ALTER TABLE jobs DROP COLUMN media_type;
ALTER TABLE jobs DROP COLUMN protocol;
