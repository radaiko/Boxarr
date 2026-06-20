package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/radaiko/boxarr/internal/servarr"
)

// GetSetting returns the value for key and whether it was found.
func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("getting setting %q: %w", key, err)
	}
	return v, true, nil
}

// SetSetting writes (or overwrites) one setting.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP`,
		key, value)
	if err != nil {
		return fmt.Errorf("setting %q: %w", key, err)
	}
	return nil
}

// AllSettings returns every setting as a key→value map (settings-UI bulk read).
func (s *Store) AllSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("listing settings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// ListQualityProfiles returns the seeded quality profiles (Seerr GET /qualityprofile).
func (s *Store) ListQualityProfiles(ctx context.Context) ([]*servarr.QualityProfile, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, is_default, created_at FROM quality_profile ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("listing quality profiles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*servarr.QualityProfile
	for rows.Next() {
		var p servarr.QualityProfile
		var isDefault int
		if err := rows.Scan(&p.ID, &p.Name, &isDefault, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.IsDefault = isDefault != 0
		out = append(out, &p)
	}
	return out, rows.Err()
}

// ListRootFolders returns seeded root folders; kind "" returns all, else "tv"/"movie".
func (s *Store) ListRootFolders(ctx context.Context, kind string) ([]*servarr.RootFolder, error) {
	query := `SELECT id, path, media_kind, created_at FROM root_folder`
	var args []any
	if kind != "" {
		query += ` WHERE media_kind=?`
		args = append(args, kind)
	}
	query += ` ORDER BY id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing root folders: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*servarr.RootFolder
	for rows.Next() {
		var f servarr.RootFolder
		if err := rows.Scan(&f.ID, &f.Path, &f.MediaKind, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &f)
	}
	return out, rows.Err()
}

// ListTags returns every tag (Seerr GET /tag; usually empty).
func (s *Store) ListTags(ctx context.Context) ([]*servarr.Tag, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, label, created_at FROM tag ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("listing tags: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*servarr.Tag
	for rows.Next() {
		var tg servarr.Tag
		if err := rows.Scan(&tg.ID, &tg.Label, &tg.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &tg)
	}
	return out, rows.Err()
}

// UpsertRootFolder re-points a seeded root folder (settings UI) keyed by id.
func (s *Store) UpsertRootFolder(ctx context.Context, id int64, path, kind string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO root_folder (id, path, media_kind) VALUES (?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET path=excluded.path, media_kind=excluded.media_kind`,
		id, path, kind)
	if err != nil {
		return fmt.Errorf("upserting root folder %d: %w", id, err)
	}
	return nil
}

// CreateTag lazily creates a tag (if Seerr ever references one) and returns its id.
func (s *Store) CreateTag(ctx context.Context, label string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO tag (label) VALUES (?)`, label)
	if err != nil {
		return 0, fmt.Errorf("creating tag %q: %w", label, err)
	}
	return res.LastInsertId()
}
