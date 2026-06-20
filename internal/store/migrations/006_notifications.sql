-- +goose Up
CREATE TABLE notification (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    type       TEXT NOT NULL,
    payload    TEXT NOT NULL DEFAULT '{}',
    job_id     INTEGER REFERENCES jobs(id) ON DELETE CASCADE,
    read       INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    read_at    TIMESTAMP
);
CREATE INDEX idx_notification_read_created ON notification(read, created_at);
CREATE INDEX idx_notification_job ON notification(job_id);

-- +goose Down
DROP TABLE notification;
