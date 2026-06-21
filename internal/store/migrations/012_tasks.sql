-- +goose Up
CREATE TABLE task (
    id          INTEGER PRIMARY KEY,    -- the task manager's id (kept across restarts)
    type        TEXT NOT NULL,
    label       TEXT NOT NULL DEFAULT '',
    state       TEXT NOT NULL,          -- queued | running | done | error
    current     INTEGER NOT NULL DEFAULT 0,
    total       INTEGER NOT NULL DEFAULT 0,
    details     TEXT NOT NULL DEFAULT '', -- JSON array of detail lines
    error       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL,
    started_at  TIMESTAMP,
    finished_at TIMESTAMP
);
CREATE INDEX idx_task_id_desc ON task(id DESC);

-- +goose Down
DROP TABLE task;
