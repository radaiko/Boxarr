package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/prowlarr"
	"github.com/radaiko/boxarr/internal/selection"
	"github.com/radaiko/boxarr/internal/settings"
)

// Searcher is the subset of the Prowlarr client the catalog needs for auto-search.
type Searcher interface {
	Search(ctx context.Context, p prowlarr.SearchParams) ([]prowlarr.ReleaseResource, error)
}

// SetSearcher overrides the auto-search dependency (used by tests).
func (s *Service) SetSearcher(p Searcher) { s.search = p }

// liveSearcher resolves the current Prowlarr client from settings per call.
type liveSearcher struct{ set *settings.Store }

func (l liveSearcher) Search(ctx context.Context, p prowlarr.SearchParams) ([]prowlarr.ReleaseResource, error) {
	return l.set.Prowlarr().Search(ctx, p)
}

var searchHTTP = &http.Client{Timeout: 60 * time.Second}

// SearchWantedForMovie searches Prowlarr for a monitored, file-less movie and
// grabs the best-scoring release (FR-SR-4 / Seerr searchForMovie).
func (s *Service) SearchWantedForMovie(ctx context.Context, movieID int64) error {
	if s.search == nil {
		return nil
	}
	m, err := s.store.GetMovie(ctx, movieID)
	if err != nil {
		return err
	}
	if m.HasFile || !m.Monitored {
		return nil
	}
	q := m.Title
	if m.Year > 0 {
		q = fmt.Sprintf("%s %d", m.Title, m.Year)
	}
	results, err := s.search.Search(ctx, prowlarr.SearchParams{Query: q, Type: "movie", Categories: []int{2000}})
	if err != nil {
		return err
	}
	best, ok := s.pickBest(results, "movie")
	if !ok {
		return nil // nothing acceptable; stays wanted
	}
	jb, err := s.grabBest(ctx, best, "movie", m.ID)
	if err != nil {
		return err
	}
	m.JobID = jb.ID
	if m.Status == media.MediaWanted || m.Status == media.MediaMissing {
		m.Status = media.MediaSearching
	}
	return s.store.UpdateMovie(ctx, m)
}

// SearchWantedForSeries searches+grabs the best release for each wanted episode
// of a series (FR-SR-4 / Seerr searchForMissingEpisodes).
func (s *Service) SearchWantedForSeries(ctx context.Context, seriesID int64) error {
	if s.search == nil {
		return nil
	}
	sr, err := s.store.GetSeries(ctx, seriesID)
	if err != nil {
		return err
	}
	episodes, err := s.store.ListEpisodes(ctx, seriesID)
	if err != nil {
		return err
	}
	kind := "series"
	if sr.SeriesType == "anime" {
		kind = "anime"
	}
	today := time.Now().UTC().Format("2006-01-02")
	for _, ep := range episodes {
		if ep.HasFile || !ep.Monitored || ep.AirDate == "" || ep.AirDate > today {
			continue
		}
		q := fmt.Sprintf("%s S%02dE%02d", sr.Title, ep.SeasonNumber, ep.EpisodeNumber)
		_ = s.store.MarkEpisodesSearched(ctx, ep.ID)
		results, serr := s.search.Search(ctx, prowlarr.SearchParams{Query: q, Type: "tvsearch", Categories: []int{5000}})
		if serr != nil {
			s.logSearchErr(q, serr)
			continue
		}
		best, ok := s.pickBest(results, kind)
		if !ok {
			continue
		}
		jb, gerr := s.grabBest(ctx, best, "episode", ep.ID)
		if gerr != nil {
			s.logSearchErr(q, gerr)
			continue
		}
		ep.JobID = jb.ID
		if ep.Status == media.MediaWanted || ep.Status == media.MediaMissing {
			_ = s.store.SetEpisodeStatus(ctx, ep.ID, media.MediaSearching)
		}
	}
	return nil
}

// pickBest scores results with the configured selection score and returns the
// best non-rejected release.
func (s *Service) pickBest(results []prowlarr.ReleaseResource, kind string) (prowlarr.ReleaseResource, bool) {
	cfg := s.set.SelectionConfigFor(kind)
	bestIdx, bestScore := -1, 0
	for i, rr := range results {
		rel := selection.Release{
			Title: rr.Title, Protocol: rr.Protocol, Size: rr.Size,
			Seeders: rr.Seeders, Grabs: rr.Grabs, IndexerFlags: rr.IndexerFlags,
		}
		sc := cfg.Score(rel)
		if sc < cfg.MinScore {
			continue
		}
		if bestIdx == -1 || sc > bestScore {
			bestIdx, bestScore = i, sc
		}
	}
	if bestIdx == -1 {
		return prowlarr.ReleaseResource{}, false
	}
	return results[bestIdx], true
}

// grabBest stores the chosen release's artifact, dedups, and creates a pending job.
func (s *Service) grabBest(ctx context.Context, rr prowlarr.ReleaseResource, mediaType string, mediaRef int64) (*job.Job, error) {
	if rr.Protocol == "torrent" {
		hash := strings.ToLower(rr.InfoHash)
		if hash != "" {
			if existing, _ := s.store.FindByTorrentHash(ctx, hash, mediaType); existing != nil {
				return existing, nil
			}
		}
		jb := &job.Job{
			State: job.StatePending, Category: mediaType, NZBName: rr.Title,
			Protocol: "torrent", MediaType: mediaType, MediaRef: mediaRef, TorrentHash: hash,
		}
		if rr.MagnetURL != "" {
			jb.TorrentMagnet = rr.MagnetURL
		} else if rr.DownloadURL != "" {
			b, err := fetchArtifact(ctx, rr.DownloadURL)
			if err != nil {
				return nil, err
			}
			jb.TorrentFile = b
		} else {
			return nil, fmt.Errorf("release has no magnet/download url")
		}
		return s.insertJob(ctx, jb)
	}
	if rr.DownloadURL == "" {
		return nil, fmt.Errorf("usenet release has no download url")
	}
	b, err := fetchArtifact(ctx, rr.DownloadURL)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	sha := hex.EncodeToString(sum[:])
	if existing, _ := s.store.FindBySHA256(ctx, sha, mediaType); existing != nil {
		return existing, nil
	}
	return s.insertJob(ctx, &job.Job{
		State: job.StatePending, Category: mediaType, NZBName: rr.Title,
		NZBContent: b, NZBSHA256: sha, NZBURL: rr.DownloadURL,
		Protocol: "usenet", MediaType: mediaType, MediaRef: mediaRef,
	})
}

func (s *Service) insertJob(ctx context.Context, jb *job.Job) (*job.Job, error) {
	id, err := s.store.CreateJob(ctx, jb)
	if err != nil {
		return nil, err
	}
	jb.ID = id
	return jb, nil
}

func (s *Service) logSearchErr(q string, err error) {
	// best-effort: catalog has no logger; errors surface via the caller's logs.
	_ = q
	_ = err
}

func fetchArtifact(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := searchHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}
