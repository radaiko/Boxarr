// Package prowlarr is a client for the Prowlarr indexer-aggregator API v1.
package prowlarr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

// artifactBrowserUA mimics a real browser so Cloudflare-fronted indexers (reached
// via Prowlarr's redirect) allow the download. The default Go user-agent trips
// Cloudflare's bot check and gets a 403 challenge page; this is the same identity
// FlareSolverr presents for the *arr stack.
const artifactBrowserUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

var artifactHTTP = &http.Client{Timeout: 60 * time.Second}

// FetchArtifact downloads a release artifact (.nzb / .torrent) from a Prowlarr
// download URL. Prowlarr 301-redirects usenet downloads to the real indexer, which
// is often behind Cloudflare, so we present a browser identity (User-Agent + Accept
// headers, which Go preserves across the cross-host redirect). When the URL points
// at the configured Prowlarr, we also send its API key. Returns a clear error when
// Cloudflare blocks the request so the cause is obvious in logs/toasts.
func FetchArtifact(ctx context.Context, rawURL, prowlarrBaseURL, apiKey string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", artifactBrowserUA)
	req.Header.Set("Accept", "application/x-nzb,application/x-bittorrent,application/octet-stream,*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if base := strings.TrimRight(prowlarrBaseURL, "/"); base != "" && apiKey != "" && strings.HasPrefix(rawURL, base) {
		req.Header.Set("X-Api-Key", apiKey)
	}
	resp, err := artifactHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	// resp.Request is the final request after redirects — the host that actually
	// served the response (Prowlarr redirects out to the indexer for NZBs).
	finalHost := ""
	if resp.Request != nil && resp.Request.URL != nil {
		finalHost = resp.Request.URL.Host
	}
	ct := resp.Header.Get("Content-Type")
	cf := looksCloudflare(body, resp.Header)
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		slog.Default().Warn("artifact download failed", "finalHost", finalHost,
			"status", resp.StatusCode, "contentType", ct, "cloudflare", cf, "body", msg)
		if cf {
			return nil, fmt.Errorf("status %d: blocked by Cloudflare at %s — that indexer needs FlareSolverr or a Cloudflare allow-rule", resp.StatusCode, finalHost)
		}
		if msg != "" {
			return nil, fmt.Errorf("status %d (%s): %s", resp.StatusCode, finalHost, msg)
		}
		return nil, fmt.Errorf("status %d from %s", resp.StatusCode, finalHost)
	}
	// A 200 that is really an HTML error/challenge page, not a file.
	if strings.Contains(ct, "text/html") {
		slog.Default().Warn("artifact download returned HTML, not a file", "finalHost", finalHost, "contentType", ct, "cloudflare", cf)
		if cf {
			return nil, fmt.Errorf("blocked by Cloudflare at %s (challenge page) — that indexer needs FlareSolverr or a Cloudflare allow-rule", finalHost)
		}
		return nil, fmt.Errorf("got an HTML page from %s (not an NZB/torrent — check the indexer download/auth)", finalHost)
	}
	return body, nil
}

// looksCloudflare reports whether a response is a Cloudflare block/challenge page
// (mirrors Prowlarr's CloudFlareDetectionService markers).
func looksCloudflare(body []byte, hdr http.Header) bool {
	if strings.Contains(strings.ToLower(hdr.Get("Server")), "cloudflare") {
		return true
	}
	b := strings.ToLower(string(body))
	return strings.Contains(b, "just a moment") ||
		strings.Contains(b, "attention required") ||
		strings.Contains(b, "error code: 1020") ||
		strings.Contains(b, "/cdn-cgi/") ||
		strings.Contains(b, "no-js ie6 oldie")
}
