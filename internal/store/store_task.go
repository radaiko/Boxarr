package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/radaiko/boxarr/internal/task"
)

// SaveTask upserts a background task (best-effort persistence so the Activity
// history survives restarts). Satisfies task.Sink.
func (s *Store) SaveTask(t task.Task) {
	det, _ := json.Marshal(t.Details)
	_, err := s.db.ExecContext(context.Background(),
		`INSERT INTO task (id, type, label, state, current, total, details, error, created_at, started_at, finished_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET
		   state=excluded.state, current=excluded.current, total=excluded.total,
		   details=excluded.details, error=excluded.error,
		   started_at=excluded.started_at, finished_at=excluded.finished_at`,
		t.ID, t.Type, t.Label, t.State, t.Current, t.Total, string(det), t.Error,
		t.CreatedAt, t.StartedAt, t.FinishedAt)
	if err != nil {
		return
	}
	_, _ = s.db.ExecContext(context.Background(),
		`DELETE FROM task WHERE id NOT IN (SELECT id FROM task ORDER BY id DESC LIMIT 200)`)
}

// ListTasks returns recent tasks newest-first (for restoring Activity history).
func (s *Store) ListTasks(ctx context.Context, limit int) ([]task.Task, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, type, label, state, current, total, details, error, created_at, started_at, finished_at
		 FROM task ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []task.Task
	for rows.Next() {
		var t task.Task
		var details string
		var started, finished sql.NullTime
		if err := rows.Scan(&t.ID, &t.Type, &t.Label, &t.State, &t.Current, &t.Total,
			&details, &t.Error, &t.CreatedAt, &started, &finished); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(details), &t.Details)
		if started.Valid {
			t.StartedAt = &started.Time
		}
		if finished.Valid {
			t.FinishedAt = &finished.Time
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// MarkRunningTasksInterrupted flags tasks left queued/running by a previous run
// as errored (the in-process work didn't survive the restart).
func (s *Store) MarkRunningTasksInterrupted(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE task SET state='error', error='interrupted by restart', finished_at=CURRENT_TIMESTAMP
		 WHERE state IN ('queued','running')`)
	if err != nil {
		return fmt.Errorf("marking interrupted tasks: %w", err)
	}
	return nil
}
