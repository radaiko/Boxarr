package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/radaiko/boxarr/internal/webdav"
)

const webdavItemColumns = `id, name, remote_path, size, category, known, job_id,
	is_broken, first_seen, last_seen, created_at`

func scanWebDAVItem(row scanner) (*webdav.WebDAVItem, error) {
	var w webdav.WebDAVItem
	var (
		jobID           sql.NullInt64
		known, isBroken int
	)
	if err := row.Scan(&w.ID, &w.Name, &w.RemotePath, &w.Size, &w.Category, &known,
		&jobID, &isBroken, &w.FirstSeen, &w.LastSeen, &w.CreatedAt); err != nil {
		return nil, err
	}
	w.Known, w.IsBroken, w.JobID = known != 0, isBroken != 0, jobID.Int64
	return &w, nil
}

// UpsertWebDAVItem inserts or refreshes one mount item keyed by remote_path,
// bumping last_seen and clearing is_broken (it was just seen). Mirrors the
// imported_symlinks upsert: the reconciler re-sees each path every sweep.
func (s *Store) UpsertWebDAVItem(ctx context.Context, w *webdav.WebDAVItem) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO webdav_item (name, remote_path, size, category, known, job_id, is_broken, last_seen)
		 VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(remote_path) DO UPDATE SET
		   name=excluded.name, size=excluded.size, category=excluded.category,
		   known=excluded.known, job_id=excluded.job_id, is_broken=0,
		   last_seen=CURRENT_TIMESTAMP`,
		w.Name, w.RemotePath, w.Size, w.Category, b2i(w.Known), nullInt(w.JobID), b2i(w.IsBroken))
	if err != nil {
		return fmt.Errorf("upserting webdav item: %w", err)
	}
	return nil
}

// ListWebDAVItems returns every mount item, sorted by name.
func (s *Store) ListWebDAVItems(ctx context.Context) ([]*webdav.WebDAVItem, error) {
	return s.webdavItems(ctx, `SELECT `+webdavItemColumns+` FROM webdav_item ORDER BY name`)
}

// ListUnknownWebDAVItems returns mount items not matched to a Boxarr job.
func (s *Store) ListUnknownWebDAVItems(ctx context.Context) ([]*webdav.WebDAVItem, error) {
	return s.webdavItems(ctx, `SELECT `+webdavItemColumns+` FROM webdav_item WHERE known=0 ORDER BY name`)
}

func (s *Store) webdavItems(ctx context.Context, query string, args ...any) ([]*webdav.WebDAVItem, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing webdav items: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*webdav.WebDAVItem
	for rows.Next() {
		w, err := scanWebDAVItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// DBNow returns the database clock as a CURRENT_TIMESTAMP-format string. The
// reconciler captures this at the start of a sweep so stale-detection compares
// like-for-like against last_seen (also set from the DB clock) — avoiding the
// timezone/format mismatch of comparing a Go local time against SQLite UTC.
func (s *Store) DBNow(ctx context.Context) (string, error) {
	var now string
	if err := s.db.QueryRowContext(ctx, `SELECT CURRENT_TIMESTAMP`).Scan(&now); err != nil {
		return "", fmt.Errorf("reading db clock: %w", err)
	}
	return now, nil
}

// MarkWebDAVItemsBrokenNotSeenSince flags items whose last_seen predates this
// sweep marker (gone from the mount) and returns how many were flagged. sweep
// must be a DB-clock timestamp from DBNow so the comparison is like-for-like.
func (s *Store) MarkWebDAVItemsBrokenNotSeenSince(ctx context.Context, sweep string) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE webdav_item SET is_broken=1 WHERE last_seen < ?`, sweep)
	if err != nil {
		return 0, fmt.Errorf("marking stale webdav items broken: %w", err)
	}
	return res.RowsAffected()
}

// GetWebDAVItemByPath returns the mount item at remotePath (or sql.ErrNoRows).
func (s *Store) GetWebDAVItemByPath(ctx context.Context, remotePath string) (*webdav.WebDAVItem, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+webdavItemColumns+` FROM webdav_item WHERE remote_path = ?`, remotePath)
	return scanWebDAVItem(row)
}

// SetWebDAVItemKnown flags a mount item known (adopt/ignore: stop re-flagging it).
func (s *Store) SetWebDAVItemKnown(ctx context.Context, remotePath string, known bool) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE webdav_item SET known = ? WHERE remote_path = ?`, b2i(known), remotePath)
	if err != nil {
		return fmt.Errorf("setting webdav item known: %w", err)
	}
	return nil
}

// PruneStaleWebDAVItems deletes broken mount rows not seen for graceDays — old
// TorBox-rotated content / one-off streams — so the table doesn't grow unbounded.
// If the content reappears on the mount the reconciler simply re-adds it. Returns
// the number of rows removed.
func (s *Store) PruneStaleWebDAVItems(ctx context.Context, graceDays int) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM webdav_item WHERE is_broken=1 AND last_seen < datetime('now', ?)`,
		fmt.Sprintf("-%d days", graceDays))
	if err != nil {
		return 0, fmt.Errorf("pruning stale webdav items: %w", err)
	}
	return res.RowsAffected()
}

// DeleteWebDAVItemByPath removes a mount item row (after deleting it on TorBox).
func (s *Store) DeleteWebDAVItemByPath(ctx context.Context, remotePath string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM webdav_item WHERE remote_path = ?`, remotePath)
	if err != nil {
		return fmt.Errorf("deleting webdav item: %w", err)
	}
	return nil
}

// AddDeletedPath tombstones a path just deleted on TorBox so the reconciler does
// not re-add it from a stale rclone listing before the mount catches up.
func (s *Store) AddDeletedPath(ctx context.Context, remotePath string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO deleted_path (remote_path) VALUES (?)
		 ON CONFLICT(remote_path) DO UPDATE SET deleted_at=CURRENT_TIMESTAMP`, remotePath)
	if err != nil {
		return fmt.Errorf("recording deleted path: %w", err)
	}
	return nil
}

// ListDeletedPaths returns the set of tombstoned paths.
func (s *Store) ListDeletedPaths(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT remote_path FROM deleted_path`)
	if err != nil {
		return nil, fmt.Errorf("listing deleted paths: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out[p] = true
	}
	return out, rows.Err()
}

// ClearDeletedPath removes a tombstone (the path was re-acquired, or has finally
// disappeared from the mount).
func (s *Store) ClearDeletedPath(ctx context.Context, remotePath string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM deleted_path WHERE remote_path = ?`, remotePath)
	if err != nil {
		return fmt.Errorf("clearing deleted path: %w", err)
	}
	return nil
}

// WebDAVUsageByCategory returns non-broken mount bytes grouped by category (FR-ST-3).
func (s *Store) WebDAVUsageByCategory(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT category, COALESCE(SUM(size),0) FROM webdav_item WHERE is_broken=0 GROUP BY category`)
	if err != nil {
		return nil, fmt.Errorf("summing webdav usage by category: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int64{}
	for rows.Next() {
		var cat string
		var n int64
		if err := rows.Scan(&cat, &n); err != nil {
			return nil, err
		}
		out[cat] = n
	}
	return out, rows.Err()
}

// WebDAVUsageBytes returns the total size of non-broken mount items (FR-ST-1).
func (s *Store) WebDAVUsageBytes(ctx context.Context) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(size), 0) FROM webdav_item WHERE is_broken=0`).Scan(&n); err != nil {
		return 0, fmt.Errorf("summing webdav usage: %w", err)
	}
	return n, nil
}
