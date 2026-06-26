package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/radaiko/boxarr/internal/release"
)

// GroupFailedGrabCounts returns, per release group (lower-cased), how many of its
// releases are blocklisted (failed to download). Used both for the Database UI
// "most failed grabs" view and to penalize failure-prone groups in scoring.
func (s *Store) GroupFailedGrabCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT release_name FROM grab_blocklist`)
	if err != nil {
		return nil, fmt.Errorf("counting failed grabs by group: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if p, perr := release.ParseRelease(name); perr == nil && p != nil && p.Group != "" {
			out[strings.ToLower(p.Group)]++
		}
	}
	return out, rows.Err()
}

// BlocklistGrab records a release that failed to download so selection won't grab
// it again (an auto-retry then picks a different release). Keyed by release name;
// re-recording just refreshes the reason/timestamp.
func (s *Store) BlocklistGrab(ctx context.Context, releaseName, reason string) error {
	if releaseName == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO grab_blocklist (release_name, reason, created_at)
		 VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(release_name) DO UPDATE SET reason=excluded.reason, created_at=CURRENT_TIMESTAMP`,
		releaseName, reason)
	if err != nil {
		return fmt.Errorf("blocklisting grab: %w", err)
	}
	return nil
}

// BlocklistEntry is one blocklisted (failed-to-download) release, for display.
type BlocklistEntry struct {
	ReleaseName string `json:"releaseName"`
	Reason      string `json:"reason"`
	CreatedAt   string `json:"createdAt"`
}

// ListBlocklistedGrabs returns blocklisted releases newest-first (for the UI).
func (s *Store) ListBlocklistedGrabs(ctx context.Context, limit int) ([]BlocklistEntry, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT release_name, reason, CAST(created_at AS TEXT)
		 FROM grab_blocklist ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("listing grab blocklist: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []BlocklistEntry
	for rows.Next() {
		var e BlocklistEntry
		if err := rows.Scan(&e.ReleaseName, &e.Reason, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// RemoveBlocklistedGrab un-blocklists a release so selection may grab it again.
func (s *Store) RemoveBlocklistedGrab(ctx context.Context, releaseName string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM grab_blocklist WHERE release_name=?`, releaseName); err != nil {
		return fmt.Errorf("removing grab blocklist entry: %w", err)
	}
	return nil
}

// BlocklistedGrabs returns the set of release names that failed to download, so
// selection can skip them.
func (s *Store) BlocklistedGrabs(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT release_name FROM grab_blocklist`)
	if err != nil {
		return nil, fmt.Errorf("listing grab blocklist: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
}
