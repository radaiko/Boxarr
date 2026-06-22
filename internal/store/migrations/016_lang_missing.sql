-- +goose Up
-- Set by the Plex stream check when an imported item has no acceptable audio or
-- subtitle language; drives the in-list marker + background re-search.
ALTER TABLE episode ADD COLUMN lang_missing INTEGER NOT NULL DEFAULT 0;
ALTER TABLE movie ADD COLUMN lang_missing INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE episode DROP COLUMN lang_missing;
ALTER TABLE movie DROP COLUMN lang_missing;
