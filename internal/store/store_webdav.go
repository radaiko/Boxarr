package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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

// MarkWebDAVItemsBrokenNotSeenSince flags items whose last_seen predates this
// sweep (gone from the mount) and returns how many were flagged.
func (s *Store) MarkWebDAVItemsBrokenNotSeenSince(ctx context.Context, sweep time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE webdav_item SET is_broken=1 WHERE last_seen < ?`, sweep)
	if err != nil {
		return 0, fmt.Errorf("marking stale webdav items broken: %w", err)
	}
	return res.RowsAffected()
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
