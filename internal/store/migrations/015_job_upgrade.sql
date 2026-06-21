-- +goose Up
-- Marks a grab as a language/quality upgrade of an already-imported item, so the
-- submitter doesn't retire it as a duplicate of the existing file.
ALTER TABLE jobs ADD COLUMN is_upgrade INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE jobs DROP COLUMN is_upgrade;
