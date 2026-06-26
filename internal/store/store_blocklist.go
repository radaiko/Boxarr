package store

import (
	"context"
	"fmt"
)

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
