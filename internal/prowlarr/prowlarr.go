// Package prowlarr is a client for the Prowlarr indexer-aggregator API v1.
package prowlarr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Client talks to a Prowlarr instance.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New returns a Client for the Prowlarr instance at baseURL (e.g. http://host:9696).
func New(baseURL, apiKey string) *Client {
	return &Client{baseURL: baseURL, apiKey: apiKey, http: &http.Client{Timeout: 30 * time.Second}}
}

// SearchParams describes an interactive search. IndexerIDs defaults to {-1} (all).
type SearchParams struct {
	Query      string
	Type       string // "search" | "tvsearch" | "movie"
	Categories []int  // repeated keys
	IndexerIDs []int  // repeated keys; default {-1}
	Limit      int    // 0 -> omit (Prowlarr defaults 100)
	Offset     int
}

// Category is a Newznab/Torznab category (recursive).
type Category struct {
	ID            int        `json:"id"`
	Name          string     `json:"name"`
	SubCategories []Category `json:"subCategories"`
}

// ReleaseResource is one search result (trimmed to the fields Boxarr consumes).
type ReleaseResource struct {
	Title        string     `json:"title"`
	Indexer      string     `json:"indexer"`
	IndexerID    int        `json:"indexerId"`
	Size         int64      `json:"size"`
	Files        int        `json:"files"`
	Grabs        int        `json:"grabs"`
	Protocol     string     `json:"protocol"` // "torrent" | "usenet" | "unknown"
	DownloadURL  string     `json:"downloadUrl"`
	MagnetURL    string     `json:"magnetUrl"`
	InfoURL      string     `json:"infoUrl"`
	InfoHash     string     `json:"infoHash"`
	Seeders      int        `json:"seeders"`
	Leechers     int        `json:"leechers"`
	PublishDate  string     `json:"publishDate"`
	IndexerFlags []string   `json:"indexerFlags"`
	Categories   []Category `json:"categories"`
	FileName     string     `json:"fileName"`
	GUID         string     `json:"guid"`
}

// IndexerResource is one configured indexer (trimmed).
type IndexerResource struct {
	ID           int          `json:"id"`
	Name         string       `json:"name"`
	Enable       bool         `json:"enable"`
	Protocol     string       `json:"protocol"`
	Privacy      string       `json:"privacy"`
	Priority     int          `json:"priority"`
	Capabilities Capabilities `json:"capabilities"`
}

// Capabilities is the subset of an indexer's capabilities Boxarr uses.
type Capabilities struct {
	Categories []Category `json:"categories"`
}

// Search runs an interactive search and returns the releases. Prowlarr returns
// an empty array (not an error) when individual indexers fail.
func (c *Client) Search(ctx context.Context, p SearchParams) ([]ReleaseResource, error) {
	v := url.Values{}
	v.Set("query", p.Query)
	typ := p.Type
	if typ == "" {
		typ = "search"
	}
	v.Set("type", typ)
	for _, cat := range p.Categories {
		v.Add("categories", strconv.Itoa(cat))
	}
	ids := p.IndexerIDs
	if len(ids) == 0 {
		ids = []int{-1} // all indexers
	}
	for _, id := range ids {
		v.Add("indexerIds", strconv.Itoa(id))
	}
	if p.Limit > 0 {
		v.Set("limit", strconv.Itoa(p.Limit))
	}
	if p.Offset > 0 {
		v.Set("offset", strconv.Itoa(p.Offset))
	}
	body, err := c.do(ctx, "/api/v1/search?"+v.Encode())
	if err != nil {
		return nil, err
	}
	var out []ReleaseResource
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decoding search results: %w", err)
	}
	return out, nil
}

// Indexers returns the configured indexers.
func (c *Client) Indexers(ctx context.Context) ([]IndexerResource, error) {
	body, err := c.do(ctx, "/api/v1/indexer")
	if err != nil {
		return nil, err
	}
	var out []IndexerResource
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decoding indexers: %w", err)
	}
	return out, nil
}

// do issues a GET with the X-Api-Key header and returns the response body,
// surfacing non-2xx responses as an error.
func (c *Client) do(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("prowlarr GET %s: status %d", path, resp.StatusCode)
	}
	return body, nil
}
