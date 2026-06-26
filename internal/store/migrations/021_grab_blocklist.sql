-- +goose Up
-- Releases that failed to download (e.g. TorBox "Aborted, cannot be completed" —
-- an incomplete usenet NZB). Selection skips these so an auto-retry grabs a
-- DIFFERENT release instead of re-grabbing the broken one in a loop.
CREATE TABLE grab_blocklist (
    release_name TEXT PRIMARY KEY,
    reason       TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE grab_blocklist;
