// Package plex is a thin client for the Plex Media Server HTTP API, covering
// Boxarr's needs: list libraries and trigger (partial) scans after import.
package plex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to a Plex Media Server.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New returns a Client for the Plex server at baseURL (e.g. http://host:32400).
func New(baseURL, token string) *Client {
	return &Client{baseURL: baseURL, token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

// Location is one watched path of a library section.
type Location struct {
	ID   int    `json:"id"`
	Path string `json:"path"`
}

// Section is one Plex library (Type is "movie" or "show").
type Section struct {
	Key       string     `json:"key"`
	Type      string     `json:"type"`
	Title     string     `json:"title"`
	Locations []Location `json:"Location"`
}

// Sections lists the server's libraries.
func (c *Client) Sections(ctx context.Context) ([]Section, error) {
	body, err := c.do(ctx, "/library/sections")
	if err != nil {
		return nil, err
	}
	var env struct {
		MediaContainer struct {
			Directory []Section `json:"Directory"`
		} `json:"MediaContainer"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decoding sections: %w", err)
	}
	return env.MediaContainer.Directory, nil
}

// Setting is one library preference (from /library/sections/{id}/prefs). Plex
// returns value as a string, number, or bool depending on the pref, so it is
// captured raw and interpreted via Truthy.
type Setting struct {
	ID    string          `json:"id"`
	Label string          `json:"label"`
	Value json.RawMessage `json:"value"`
	Type  string          `json:"type"`
}

// Truthy reports whether the setting is enabled (non-zero / true).
func (s Setting) Truthy() bool {
	v := strings.Trim(string(s.Value), `"`)
	switch v {
	case "", "0", "false", "null":
		return false
	default:
		return true
	}
}

// SectionPrefs returns a library's preferences (used to detect expensive
// per-library analysis like intro/credits markers and preview thumbnails).
func (c *Client) SectionPrefs(ctx context.Context, sectionID string) ([]Setting, error) {
	body, err := c.do(ctx, "/library/sections/"+sectionID+"/prefs")
	if err != nil {
		return nil, err
	}
	var env struct {
		MediaContainer struct {
			Setting []Setting `json:"Setting"`
		} `json:"MediaContainer"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decoding prefs: %w", err)
	}
	return env.MediaContainer.Setting, nil
}

// ScanPath triggers a partial scan of one folder within a section. The path is
// the path as Plex sees it (same absolute path as Boxarr under the same-mount
// deployment contract). The scan is async; any 2xx is success.
func (c *Client) ScanPath(ctx context.Context, sectionID, path string) error {
	// url.QueryEscape gives quote_plus semantics (spaces -> '+'), matching
	// what Plex's scanner expects.
	p := "/library/sections/" + sectionID + "/refresh?path=" + url.QueryEscape(path)
	_, err := c.do(ctx, p)
	return err
}

// ScanSection triggers a full scan of a section (fallback when a partial scan
// is unsupported or did not surface the item).
func (c *Client) ScanSection(ctx context.Context, sectionID string) error {
	_, err := c.do(ctx, "/library/sections/"+sectionID+"/refresh")
	return err
}

// metaItem is the subset of a Plex metadata record Boxarr reads.
type metaItem struct {
	RatingKey   string `json:"ratingKey"`
	Title       string `json:"title"`
	Year        int    `json:"year"`
	Index       int    `json:"index"`       // episode number
	ParentIndex int    `json:"parentIndex"` // season number
	Media       []struct {
		Part []struct {
			ID     int      `json:"id"`
			Stream []Stream `json:"Stream"`
		} `json:"Part"`
	} `json:"Media"`
}

func decodeMeta(body []byte) ([]metaItem, error) {
	var env struct {
		MediaContainer struct {
			Metadata []metaItem `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decoding metadata: %w", err)
	}
	return env.MediaContainer.Metadata, nil
}

// LibItem is a minimal Plex library entry (movie or episode).
type LibItem struct {
	RatingKey string
	Title     string
	Year      int
	Season    int // parentIndex (episodes)
	Episode   int // index (episodes)
}

func toLibItems(items []metaItem) []LibItem {
	out := make([]LibItem, 0, len(items))
	for _, m := range items {
		out = append(out, LibItem{RatingKey: m.RatingKey, Title: m.Title, Year: m.Year, Season: m.ParentIndex, Episode: m.Index})
	}
	return out
}

// SectionItems lists a library section's items (typ: 1=movie, 2=show). Fetched
// once per sweep so per-item lookups don't each hit the server.
func (c *Client) SectionItems(ctx context.Context, sectionID string, typ int) ([]LibItem, error) {
	body, err := c.do(ctx, "/library/sections/"+sectionID+"/all?type="+itoa(typ))
	if err != nil {
		return nil, err
	}
	items, err := decodeMeta(body)
	if err != nil {
		return nil, err
	}
	return toLibItems(items), nil
}

// ShowEpisodes returns every episode of a show (by ratingKey).
func (c *Client) ShowEpisodes(ctx context.Context, showRatingKey string) ([]LibItem, error) {
	body, err := c.do(ctx, "/library/metadata/"+showRatingKey+"/allLeaves")
	if err != nil {
		return nil, err
	}
	items, err := decodeMeta(body)
	if err != nil {
		return nil, err
	}
	return toLibItems(items), nil
}

// ItemStreams returns the first part's id and its audio + subtitle streams.
func (c *Client) ItemStreams(ctx context.Context, ratingKey string) (partID int, audio, subs []Stream, err error) {
	body, err := c.do(ctx, "/library/metadata/"+ratingKey)
	if err != nil {
		return 0, nil, nil, err
	}
	items, err := decodeMeta(body)
	if err != nil {
		return 0, nil, nil, err
	}
	if len(items) == 0 || len(items[0].Media) == 0 || len(items[0].Media[0].Part) == 0 {
		return 0, nil, nil, fmt.Errorf("no media part for %s", ratingKey)
	}
	part := items[0].Media[0].Part[0]
	for _, s := range part.Stream {
		switch s.StreamType {
		case 2:
			audio = append(audio, s)
		case 3:
			subs = append(subs, s)
		}
	}
	return part.ID, audio, subs, nil
}

// CurrentSelection returns the id of the currently-selected audio + subtitle
// stream (0 = none selected).
func CurrentSelection(audio, subs []Stream) (audioID, subID int) {
	for _, s := range audio {
		if s.Selected {
			audioID = s.ID
		}
	}
	for _, s := range subs {
		if s.Selected {
			subID = s.ID
		}
	}
	return audioID, subID
}

// SetDefaultStreams sets the default audio (and subtitle, 0 = disable) for a part.
func (c *Client) SetDefaultStreams(ctx context.Context, partID, audioStreamID, subtitleStreamID int) error {
	q := url.Values{}
	if audioStreamID > 0 {
		q.Set("audioStreamID", itoa(audioStreamID))
	}
	q.Set("subtitleStreamID", itoa(subtitleStreamID)) // 0 disables subtitles
	q.Set("allParts", "1")
	return c.put(ctx, "/library/parts/"+itoa(partID)+"?"+q.Encode())
}

func itoa(n int) string { return strconv.Itoa(n) }

func (c *Client) put(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("X-Plex-Token", c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("PUT %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return plexStatusErr("PUT", path, resp.StatusCode)
	}
	return nil
}

func (c *Client) do(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("X-Plex-Token", c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, plexStatusErr("GET", path, resp.StatusCode)
	}
	return body, nil
}

// plexStatusErr describes a non-2xx Plex response, with an actionable hint for the
// common 401 — an invalid or expired Plex token.
func plexStatusErr(method, path string, status int) error {
	if status == http.StatusUnauthorized {
		return fmt.Errorf("plex %s %s: unauthorized (401) — Plex token invalid or expired; reconnect Plex in Settings", method, path)
	}
	return fmt.Errorf("plex %s %s: status %d", method, path, status)
}
