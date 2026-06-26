-- +goose Up
-- Track whether we've already raised an unknown_content notification for a WebDAV
-- item, so it's surfaced ONCE and doesn't re-fire every reconcile after the user
-- clears notifications (which previously deleted the only dedup record).
ALTER TABLE webdav_item ADD COLUMN notified INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE webdav_item DROP COLUMN notified;
