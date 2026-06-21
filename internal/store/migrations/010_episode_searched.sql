-- +goose Up
ALTER TABLE episode ADD COLUMN last_searched_at TIMESTAMP;

-- +goose Down
ALTER TABLE episode DROP COLUMN last_searched_at;
