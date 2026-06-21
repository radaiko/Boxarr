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
		return nil, fmt.Errorf("plex GET %s: status %d", path, resp.StatusCode)
	}
	return body, nil
}
