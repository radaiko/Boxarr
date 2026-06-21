-- +goose Up
ALTER TABLE movie ADD COLUMN last_searched_at TIMESTAMP;

-- +goose Down
ALTER TABLE movie DROP COLUMN last_searched_at;
