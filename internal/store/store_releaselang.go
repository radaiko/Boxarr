package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// UpsertReleaseLang records the verified audio/subtitle languages of a release
// (from the Plex stream check), keyed by release name. Languages are 2-letter
// codes; group is the release group (for tendency learning).
func (s *Store) UpsertReleaseLang(ctx context.Context, releaseName, group string, audio, subs []string, source string) error {
	if releaseName == "" {
		return nil
	}
	if source == "" {
		source = "plex"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO release_lang (release_name, release_group, audio_langs, sub_langs, source, detected_at)
		 VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(release_name) DO UPDATE SET
		   release_group=excluded.release_group, audio_langs=excluded.audio_langs,
		   sub_langs=excluded.sub_langs, source=excluded.source, detected_at=CURRENT_TIMESTAMP`,
		releaseName, strings.ToLower(group), csvLower(audio), csvLower(subs), source)
	if err != nil {
		return fmt.Errorf("upserting release language: %w", err)
	}
	return nil
}

// GroupsProvidingLanguage returns the set of release groups that have at least
// one recorded release carrying lang (as audio or subtitle) — i.e. groups
// empirically known to ship that language. lang is a 2-letter code.
func (s *Store) GroupsProvidingLanguage(ctx context.Context, lang string) (map[string]bool, error) {
	lang = strings.ToLower(lang)
	like := "%," + lang + ",%"
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT release_group FROM release_lang
		 WHERE release_group <> '' AND (
		   (',' || audio_langs || ',') LIKE ? OR (',' || sub_langs || ',') LIKE ?)`,
		like, like)
	if err != nil {
		return nil, fmt.Errorf("querying groups providing language: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		out[g] = true
	}
	return out, rows.Err()
}

// GroupLangStat is a release group's verified language tendency: of Total recorded
// releases by the group, InLang carry the language (as audio or subtitle).
type GroupLangStat struct {
	Group  string  `json:"group"`
	Total  int     `json:"total"`
	InLang int     `json:"inLang"`
	Ratio  float64 `json:"ratio"`
}

// GroupLanguageStats returns, per release group, how many recorded releases carry
// lang versus the group's total — the basis for "this group reliably ships
// <lang>". Sorted by InLang desc, then Ratio desc. lang is a 2-letter code.
func (s *Store) GroupLanguageStats(ctx context.Context, lang string) ([]GroupLangStat, error) {
	lang = strings.ToLower(lang)
	like := "%," + lang + ",%"
	rows, err := s.db.QueryContext(ctx,
		`SELECT release_group, COUNT(*) AS total,
		   SUM(CASE WHEN (',' || audio_langs || ',') LIKE ? OR (',' || sub_langs || ',') LIKE ?
		            THEN 1 ELSE 0 END) AS in_lang
		 FROM release_lang
		 WHERE release_group <> ''
		 GROUP BY release_group`, like, like)
	if err != nil {
		return nil, fmt.Errorf("querying group language stats: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []GroupLangStat
	for rows.Next() {
		var g GroupLangStat
		if err := rows.Scan(&g.Group, &g.Total, &g.InLang); err != nil {
			return nil, err
		}
		if g.Total > 0 {
			g.Ratio = float64(g.InLang) / float64(g.Total)
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].InLang != out[j].InLang {
			return out[i].InLang > out[j].InLang
		}
		return out[i].Ratio > out[j].Ratio
	})
	return out, rows.Err()
}

// ReleaseLang holds one verified-language record (for display/export).
type ReleaseLang struct {
	ReleaseName  string `json:"releaseName"`
	ReleaseGroup string `json:"releaseGroup"`
	AudioLangs   string `json:"audioLangs"`
	SubLangs     string `json:"subLangs"`
	Source       string `json:"source"`
}

// ListReleaseLangs returns recorded release-language entries newest-first (for
// display + a future shared/export service).
func (s *Store) ListReleaseLangs(ctx context.Context, limit int) ([]ReleaseLang, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT release_name, release_group, audio_langs, sub_langs, source
		 FROM release_lang ORDER BY detected_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("listing release languages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ReleaseLang
	for rows.Next() {
		var rl ReleaseLang
		if err := rows.Scan(&rl.ReleaseName, &rl.ReleaseGroup, &rl.AudioLangs, &rl.SubLangs, &rl.Source); err != nil {
			return nil, err
		}
		out = append(out, rl)
	}
	return out, rows.Err()
}

// GetReleaseLang returns the verified languages for a release name, or nil.
func (s *Store) GetReleaseLang(ctx context.Context, releaseName string) (*ReleaseLang, error) {
	var rl ReleaseLang
	err := s.db.QueryRowContext(ctx,
		`SELECT release_name, release_group, audio_langs, sub_langs, source FROM release_lang WHERE release_name=?`,
		releaseName).Scan(&rl.ReleaseName, &rl.ReleaseGroup, &rl.AudioLangs, &rl.SubLangs, &rl.Source)
	if err != nil {
		return nil, nil //nolint:nilerr // not found / any error → treat as no record
	}
	return &rl, nil
}

func csvLower(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	out := make([]string, 0, len(xs))
	seen := map[string]bool{}
	for _, x := range xs {
		l := strings.ToLower(strings.TrimSpace(x))
		if l != "" && !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return strings.Join(out, ",")
}
