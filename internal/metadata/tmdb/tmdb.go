// Package tmdb is a client for The Movie Database (TMDB) API v3. TMDB is
// Boxarr's primary metadata provider for both movies and TV.
package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// DefaultBaseURL is the production TMDB API v3 base (trailing slash; paths are relative).
const DefaultBaseURL = "https://api.themoviedb.org/3"

// Client talks to TMDB. The image Configuration is fetched once and cached.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
	cfg     atomic.Pointer[Configuration]
}

// New returns a Client for the production TMDB API using a v4 Read Access Token.
func New(token string) *Client { return NewWithBaseURL(token, DefaultBaseURL) }

// NewWithBaseURL points the client at an arbitrary base (for tests).
func NewWithBaseURL(token, baseURL string) *Client {
	return &Client{baseURL: baseURL, token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

// Configuration holds the image base URL and size buckets.
type Configuration struct {
	Images struct {
		SecureBaseURL string   `json:"secure_base_url"`
		PosterSizes   []string `json:"poster_sizes"`
		BackdropSizes []string `json:"backdrop_sizes"`
		StillSizes    []string `json:"still_sizes"`
	} `json:"images"`
}

// ExternalIDs are a title's cross-provider ids (tvdb_id is an int, may be null).
type ExternalIDs struct {
	TVDBID int    `json:"tvdb_id"`
	IMDBID string `json:"imdb_id"`
}

// TVResult / MovieResult are search/find list entries.
type TVResult struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	FirstAirDate string `json:"first_air_date"`
	Overview     string `json:"overview"`
	PosterPath   string `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
}
type MovieResult struct {
	ID           int    `json:"id"`
	Title        string `json:"title"`
	ReleaseDate  string `json:"release_date"`
	Overview     string `json:"overview"`
	PosterPath   string `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
}

// FindResult is the /find response (only tv/movie results are consumed).
type FindResult struct {
	MovieResults []MovieResult `json:"movie_results"`
	TVResults    []TVResult    `json:"tv_results"`
}

// SeasonSummary is one entry of a series' seasons[] summary.
type SeasonSummary struct {
	SeasonNumber int    `json:"season_number"`
	Name         string `json:"name"`
	EpisodeCount int    `json:"episode_count"`
	AirDate      string `json:"air_date"`
	PosterPath   string `json:"poster_path"`
}

// TVDetails is /tv/{id} with external_ids appended.
type TVDetails struct {
	ID               int             `json:"id"`
	Name             string          `json:"name"`
	Overview         string          `json:"overview"`
	Status           string          `json:"status"`
	FirstAirDate     string          `json:"first_air_date"`
	LastAirDate      string          `json:"last_air_date"`
	NumberOfSeasons  int             `json:"number_of_seasons"`
	NumberOfEpisodes int             `json:"number_of_episodes"`
	PosterPath       string          `json:"poster_path"`
	BackdropPath     string          `json:"backdrop_path"`
	Seasons          []SeasonSummary `json:"seasons"`
	ExternalIDs      ExternalIDs     `json:"external_ids"`
}

// EpisodeDetail is one episode of a season.
type EpisodeDetail struct {
	EpisodeNumber int    `json:"episode_number"`
	SeasonNumber  int    `json:"season_number"`
	Name          string `json:"name"`
	AirDate       string `json:"air_date"`
	Overview      string `json:"overview"`
	StillPath     string `json:"still_path"`
	Runtime       int    `json:"runtime"`
}

// SeasonDetails is /tv/{id}/season/{n}.
type SeasonDetails struct {
	SeasonNumber int             `json:"season_number"`
	Episodes     []EpisodeDetail `json:"episodes"`
}

// ReleaseDates is the /movie append_to_response=release_dates payload.
type ReleaseDates struct {
	Results []struct {
		ISO31661     string `json:"iso_3166_1"`
		ReleaseDates []struct {
			Type        int    `json:"type"` // 3=theatrical,4=digital,5=physical
			ReleaseDate string `json:"release_date"`
		} `json:"release_dates"`
	} `json:"results"`
}

// MovieDetails is /movie/{id} with release_dates + alternative_titles appended.
type MovieDetails struct {
	ID                int          `json:"id"`
	Title             string       `json:"title"`
	OriginalTitle     string       `json:"original_title"`
	IMDBID            string       `json:"imdb_id"`
	Status            string       `json:"status"`
	ReleaseDate       string       `json:"release_date"`
	Runtime           int          `json:"runtime"`
	Overview          string       `json:"overview"`
	PosterPath        string       `json:"poster_path"`
	BackdropPath      string       `json:"backdrop_path"`
	ReleaseDates      ReleaseDates `json:"release_dates"`
	AlternativeTitles struct {
		Titles []struct {
			ISO31661 string `json:"iso_3166_1"`
			Title    string `json:"title"`
		} `json:"titles"`
	} `json:"alternative_titles"`
}

// AltTitles returns the distinct alternative + original titles for cross-language
// matching (e.g. a German release of an English-catalogued movie).
func (d *MovieDetails) AltTitles() []string {
	seen := map[string]bool{}
	var out []string
	add := func(t string) {
		t = strings.TrimSpace(t)
		if t != "" && !seen[strings.ToLower(t)] {
			seen[strings.ToLower(t)] = true
			out = append(out, t)
		}
	}
	add(d.OriginalTitle)
	for _, t := range d.AlternativeTitles.Titles {
		add(t.Title)
	}
	return out
}

// Configuration fetches (once) and caches the image configuration.
func (c *Client) Configuration(ctx context.Context) (*Configuration, error) {
	if cfg := c.cfg.Load(); cfg != nil {
		return cfg, nil
	}
	var cfg Configuration
	if err := c.get(ctx, "/configuration", &cfg); err != nil {
		return nil, err
	}
	c.cfg.Store(&cfg)
	return &cfg, nil
}

// FindByTVDB resolves a series-level TVDB id to TMDB results.
func (c *Client) FindByTVDB(ctx context.Context, tvdbID int) (*FindResult, error) {
	var out FindResult
	err := c.get(ctx, fmt.Sprintf("/find/%d?external_source=tvdb_id", tvdbID), &out)
	return &out, err
}

// TVDetails fetches a series header + seasons[] + external_ids.
func (c *Client) TVDetails(ctx context.Context, id int) (*TVDetails, error) {
	var out TVDetails
	err := c.get(ctx, fmt.Sprintf("/tv/%d?append_to_response=external_ids", id), &out)
	return &out, err
}

// TVSeason fetches a season's episodes.
func (c *Client) TVSeason(ctx context.Context, id, season int) (*SeasonDetails, error) {
	var out SeasonDetails
	err := c.get(ctx, fmt.Sprintf("/tv/%d/season/%d", id, season), &out)
	return &out, err
}

// TVExternalIDs fetches a series' cross-provider ids.
func (c *Client) TVExternalIDs(ctx context.Context, id int) (*ExternalIDs, error) {
	var out ExternalIDs
	err := c.get(ctx, fmt.Sprintf("/tv/%d/external_ids", id), &out)
	return &out, err
}

// MovieDetails fetches a movie header + release_dates.
func (c *Client) MovieDetails(ctx context.Context, id int) (*MovieDetails, error) {
	var out MovieDetails
	err := c.get(ctx, fmt.Sprintf("/movie/%d?append_to_response=release_dates,alternative_titles", id), &out)
	return &out, err
}

// MovieExternalIDs fetches a movie's cross-provider ids.
func (c *Client) MovieExternalIDs(ctx context.Context, id int) (*ExternalIDs, error) {
	var out ExternalIDs
	err := c.get(ctx, fmt.Sprintf("/movie/%d/external_ids", id), &out)
	return &out, err
}

// SearchTV searches series by name (year optional; 0 to omit).
func (c *Client) SearchTV(ctx context.Context, query string, year int) ([]TVResult, error) {
	v := url.Values{"query": {query}}
	if year > 0 {
		v.Set("first_air_date_year", strconv.Itoa(year))
	}
	var out struct {
		Results []TVResult `json:"results"`
	}
	err := c.get(ctx, "/search/tv?"+v.Encode(), &out)
	return out.Results, err
}

// SearchMovie searches movies by name (year optional; 0 to omit).
func (c *Client) SearchMovie(ctx context.Context, query string, year int) ([]MovieResult, error) {
	v := url.Values{"query": {query}}
	if year > 0 {
		v.Set("primary_release_year", strconv.Itoa(year))
	}
	var out struct {
		Results []MovieResult `json:"results"`
	}
	err := c.get(ctx, "/search/movie?"+v.Encode(), &out)
	return out.Results, err
}

// ImageURL reconstructs a full image URL from the cached configuration. Returns
// "" if filePath is empty or the configuration has not been fetched yet.
func (c *Client) ImageURL(size, filePath string) string {
	cfg := c.cfg.Load()
	if cfg == nil || filePath == "" {
		return ""
	}
	return cfg.Images.SecureBaseURL + size + filePath
}

func (c *Client) get(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("tmdb GET %s: status %d", path, resp.StatusCode)
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("decoding %s: %w", path, err)
	}
	return nil
}
