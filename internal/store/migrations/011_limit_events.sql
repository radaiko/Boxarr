-- +goose Up
CREATE TABLE limit_event (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    kind       TEXT NOT NULL,          -- rate_limit | cooldown | daily_cap
    detail     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_limit_event_created ON limit_event(created_at);

-- +goose Down
DROP TABLE limit_event;
