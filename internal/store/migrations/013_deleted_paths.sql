-- +goose Up
-- Tombstones: a deleted mount path that the rclone cache may still list for a
-- while. The reconciler skips re-adding tombstoned paths until they actually
-- disappear from the mount (then the tombstone is cleared).
CREATE TABLE deleted_path (
    remote_path TEXT PRIMARY KEY,
    deleted_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE deleted_path;
