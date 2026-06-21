package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/radaiko/boxarr/internal/notify"
)

const notificationColumns = `id, type, payload, job_id, read, created_at, read_at`

func scanNotification(row scanner) (*notify.Notification, error) {
	var n notify.Notification
	var (
		jobID  sql.NullInt64
		read   int
		readAt sql.NullTime
	)
	if err := row.Scan(&n.ID, &n.Type, &n.Payload, &jobID, &read, &n.CreatedAt, &readAt); err != nil {
		return nil, err
	}
	n.JobID, n.Read = jobID.Int64, read != 0
	if readAt.Valid {
		n.ReadAt = &readAt.Time
	}
	return &n, nil
}

// EnqueueNotification inserts an unread notification and returns its id.
func (s *Store) EnqueueNotification(ctx context.Context, n *notify.Notification) (int64, error) {
	payload := n.Payload
	if payload == "" {
		payload = "{}"
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO notification (type, payload, job_id) VALUES (?, ?, ?)`,
		n.Type, payload, nullInt(n.JobID))
	if err != nil {
		return 0, fmt.Errorf("enqueuing notification: %w", err)
	}
	return res.LastInsertId()
}

// GetNotification returns one notification by id.
func (s *Store) GetNotification(ctx context.Context, id int64) (*notify.Notification, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+notificationColumns+` FROM notification WHERE id = ?`, id)
	n, err := scanNotification(row)
	if err != nil {
		return nil, fmt.Errorf("getting notification %d: %w", id, err)
	}
	return n, nil
}

// ListNotifications returns notifications newest-first, optionally unread-only.
// A limit <= 0 falls back to 50.
func (s *Store) ListNotifications(ctx context.Context, unreadOnly bool, limit int) ([]*notify.Notification, error) {
	if limit <= 0 {
		limit = 50
	}
	where := ""
	if unreadOnly {
		where = "WHERE read=0"
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+notificationColumns+` FROM notification `+where+` ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("listing notifications: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*notify.Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// UnreadCount returns the number of unread notifications (the UI badge count).
func (s *Store) UnreadCount(ctx context.Context) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification WHERE read=0`).Scan(&n); err != nil {
		return 0, fmt.Errorf("counting unread notifications: %w", err)
	}
	return n, nil
}

// MarkNotificationRead marks one notification read.
func (s *Store) MarkNotificationRead(ctx context.Context, id int64) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE notification SET read=1, read_at=CURRENT_TIMESTAMP WHERE id=?`, id); err != nil {
		return fmt.Errorf("marking notification %d read: %w", id, err)
	}
	return nil
}

// MarkAllNotificationsRead marks every unread notification read.
func (s *Store) MarkAllNotificationsRead(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE notification SET read=1, read_at=CURRENT_TIMESTAMP WHERE read=0`); err != nil {
		return fmt.Errorf("marking all notifications read: %w", err)
	}
	return nil
}
