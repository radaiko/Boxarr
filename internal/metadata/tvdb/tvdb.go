// Package tvdb is a client for TheTVDB API v4. TVDB supplements TMDB for
// scene/absolute episode ordering and for the TVDB id the Sonarr emulation keys on.
package tvdb

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is the production TVDB v4 base.
const DefaultBaseURL = "https://api4.thetvdb.com/v4"

// Client talks to TVDB v4. Tokens last ~1 month; there is no refresh endpoint,
// so the client re-logs-in pre-emptively (~2 days before expiry).
type Client struct {
	baseURL string
	apiKey  string
	pin     string
	http    *http.Client

	mu    sync.Mutex
	token string
	exp   time.Time
}

// New returns a Client for the production TVDB API.
func New(apiKey, pin string) *Client { return NewWithBaseURL(apiKey, pin, DefaultBaseURL) }

// NewWithBaseURL points the client at an arbitrary base (for tests).
func NewWithBaseURL(apiKey, pin, baseURL string) *Client {
	return &Client{baseURL: baseURL, apiKey: apiKey, pin: pin, http: &http.Client{Timeout: 30 * time.Second}}
}

// RemoteID is a cross-provider id on a series (e.g. TheMovieDB.com).
type RemoteID struct {
	ID         string `json:"id"`
	Type       int    `json:"type"`
	SourceName string `json:"sourceName"`
}

// SeriesExtended is /series/{id}/extended (trimmed).
type SeriesExtended struct {
	ID                int        `json:"id"`
	Name              string     `json:"name"`
	DefaultSeasonType int        `json:"defaultSeasonType"`
	RemoteIDs         []RemoteID `json:"remoteIds"`
}

// Episode is one EpisodeBaseRecord (trimmed). AbsoluteNumber is nullable.
type Episode struct {
	ID             int    `json:"id"`
	SeriesID       int    `json:"seriesId"`
	Name           string `json:"name"`
	Number         int    `json:"number"`
	SeasonNumber   int    `json:"seasonNumber"`
	AbsoluteNumber *int   `json:"absoluteNumber"`
	Aired          string `json:"aired"`
	Runtime        int    `json:"runtime"`
}

// SeasonType is one entry of /seasons/types.
type SeasonType struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	AlternateName string `json:"alternateName"`
}

type envelope struct {
	Status string          `json:"status"`
	Data   json.RawMessage `json:"data"`
	Links  struct {
		Next string `json:"next"`
	} `json:"links"`
}

// ensureToken logs in if there is no token or it is within 2 days of expiry.
func (c *Client) ensureToken(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.exp.Add(-48*time.Hour)) {
		return nil
	}
	return c.login(ctx)
}

// login POSTs /login and stores the token + parsed expiry. Caller holds c.mu.
func (c *Client) login(ctx context.Context) error {
	// TVDB v4 accepts two key types: user-supported keys authenticate with
	// apikey + a subscriber PIN, while legacy/negotiated keys use the apikey
	// alone. Only send "pin" when one is configured — an empty pin makes TVDB
	// treat a legacy key as a (failed) user-supported login.
	creds := map[string]string{"apikey": c.apiKey}
	if c.pin != "" {
		creds["pin"] = c.pin
	}
	payload, _ := json.Marshal(creds)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/login", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("tvdb login: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		// Surface TheTVDB's own message (it explains the 401 — bad key, PIN
		// required/invalid, etc.) instead of just the status code.
		msg := strings.TrimSpace(string(body))
		var e struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		}
		if json.Unmarshal(body, &e) == nil && e.Message != "" {
			msg = e.Message
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		if msg == "" {
			return fmt.Errorf("tvdb login: status %d", resp.StatusCode)
		}
		return fmt.Errorf("tvdb login: status %d: %s", resp.StatusCode, msg)
	}
	var env struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("decoding login: %w", err)
	}
	if env.Data.Token == "" {
		return fmt.Errorf("tvdb login returned no token")
	}
	c.token = env.Data.Token
	c.exp = jwtExpiry(env.Data.Token)
	return nil
}

// jwtExpiry extracts the exp claim from a JWT, falling back to ~28 days out.
func jwtExpiry(token string) time.Time {
	fallback := time.Now().Add(28 * 24 * time.Hour)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fallback
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fallback
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil || claims.Exp == 0 {
		return fallback
	}
	return time.Unix(claims.Exp, 0)
}

// SeriesExtended fetches a series' extended record (remoteIds, defaultSeasonType).
func (c *Client) SeriesExtended(ctx context.Context, id int) (*SeriesExtended, error) {
	var out SeriesExtended
	_, err := c.get(ctx, fmt.Sprintf("/series/%d/extended", id), &out)
	return &out, err
}

// SearchResult is one entry from /search?type=series (snake_case fields).
type SearchResult struct {
	TVDBID    string     `json:"tvdb_id"`
	Name      string     `json:"name"`
	Year      string     `json:"year"`
	Overview  string     `json:"overview"`
	ImageURL  string     `json:"image_url"`
	RemoteIDs []RemoteID `json:"remote_ids"`
}

// TMDBID returns the linked TheMovieDB id (so the result is addable to the
// TMDB-keyed catalog), or 0 if TheTVDB has no TMDB cross-reference.
func (r SearchResult) TMDBID() int64 {
	for _, ri := range r.RemoteIDs {
		if strings.Contains(strings.ToLower(ri.SourceName), "moviedb") {
			if id, err := strconv.ParseInt(strings.TrimSpace(ri.ID), 10, 64); err == nil {
				return id
			}
		}
	}
	return 0
}

// SearchSeries searches TheTVDB for series by name (better anime coverage than
// TMDB's text search).
func (c *Client) SearchSeries(ctx context.Context, query string) ([]SearchResult, error) {
	var out []SearchResult
	_, err := c.get(ctx, "/search?type=series&query="+url.QueryEscape(query), &out)
	return out, err
}

// Episodes fetches all episodes for a series in the given ordering
// (default|official|dvd|absolute|alternate|regional), following pagination.
func (c *Client) Episodes(ctx context.Context, id int, seasonType string) ([]Episode, error) {
	path := fmt.Sprintf("/series/%d/episodes/%s", id, seasonType)
	var all []Episode
	for path != "" {
		var page struct {
			Episodes []Episode `json:"episodes"`
		}
		next, err := c.get(ctx, path, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Episodes...)
		path = next
	}
	return all, nil
}

// SeasonTypes fetches the numeric-id ↔ type-string table.
func (c *Client) SeasonTypes(ctx context.Context) ([]SeasonType, error) {
	var out []SeasonType
	_, err := c.get(ctx, "/seasons/types", &out)
	return out, err
}

// get authenticates, fetches path (or a full pagination URL), unmarshals data
// into dst, and returns links.next (empty when there are no more pages). A 401
// forces one re-login + retry.
func (c *Client) get(ctx context.Context, path string, dst any) (next string, err error) {
	for attempt := 0; attempt < 2; attempt++ {
		if err := c.ensureToken(ctx); err != nil {
			return "", err
		}
		c.mu.Lock()
		token := c.token
		c.mu.Unlock()

		u := path
		if !strings.HasPrefix(u, "http") {
			u = c.baseURL + path
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return "", fmt.Errorf("building request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			return "", fmt.Errorf("GET %s: %w", path, err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			c.mu.Lock()
			c.token = "" // force re-login
			c.mu.Unlock()
			continue
		}
		if resp.StatusCode >= 400 {
			return "", fmt.Errorf("tvdb GET %s: status %d", path, resp.StatusCode)
		}
		var env envelope
		if err := json.Unmarshal(body, &env); err != nil {
			return "", fmt.Errorf("decoding %s: %w", path, err)
		}
		if err := json.Unmarshal(env.Data, dst); err != nil {
			return "", fmt.Errorf("decoding %s data: %w", path, err)
		}
		return env.Links.Next, nil
	}
	return "", fmt.Errorf("tvdb GET %s: unauthorized after re-login", path)
}
