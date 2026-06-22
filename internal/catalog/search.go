package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
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
	// Escalating, release-gated cadence: hourly for the first 48h after release,
	// then daily for two weeks, then monthly — until acquired. A never-searched
	// item (e.g. a fresh Seerr request) searches immediately.
	if !searchDue(m.ReleaseDate, m.LastSearchedAt, time.Now(), s.cadenceFromSettings()) {
		return nil
	}
	_ = s.store.MarkMovieSearched(ctx, m.ID)
	q := m.Title
	if m.Year > 0 {
		q = fmt.Sprintf("%s %d", m.Title, m.Year)
	}
	results, err := s.search.Search(ctx, prowlarr.SearchParams{Query: q, Type: "movie", Categories: []int{2000}})
	if err != nil {
		return err
	}
	best, ok := s.pickBest(ctx, results, "movie")
	if !ok {
		return nil // nothing acceptable; stays wanted
	}
	jb, err := s.grabBest(ctx, best, "movie", m.ID, false)
	if err != nil {
		return err
	}
	m.JobID = jb.ID
	// A release was grabbed and a job created — it's queued on TorBox now (the
	// poller flips it to downloading once it starts), no longer just searching.
	if m.Status != media.MediaAvailable {
		m.Status = media.MediaQueued
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
	cad := s.cadenceFromSettings()
	for _, ep := range episodes {
		if ep.HasFile || !ep.Monitored || ep.AirDate == "" || ep.AirDate > today {
			continue
		}
		// Release-gated escalating cadence (configurable in Settings).
		if !searchDue(ep.AirDate, ep.LastSearchedAt, time.Now(), cad) {
			continue
		}
		q := fmt.Sprintf("%s S%02dE%02d", sr.Title, ep.SeasonNumber, ep.EpisodeNumber)
		_ = s.store.MarkEpisodesSearched(ctx, ep.ID)
		results, serr := s.search.Search(ctx, prowlarr.SearchParams{Query: q, Type: "tvsearch", Categories: []int{5000}})
		if serr != nil {
			s.logSearchErr(q, serr)
			continue
		}
		best, ok := s.pickBest(ctx, results, kind)
		if !ok {
			continue
		}
		jb, gerr := s.grabBest(ctx, best, "episode", ep.ID, false)
		if gerr != nil {
			s.logSearchErr(q, gerr)
			continue
		}
		ep.JobID = jb.ID
		if ep.Status != media.MediaAvailable {
			_ = s.store.SetEpisodeStatus(ctx, ep.ID, media.MediaQueued)
		}
	}
	return nil
}

// cadence is the per-item, release-aged search schedule (from settings).
type cadence struct {
	fastWindow, fastInterval   time.Duration
	dailyWindow, dailyInterval time.Duration
	slowInterval               time.Duration
}

// cadenceFromSettings reads the configurable cadence (with built-in fallbacks).
func (s *Service) cadenceFromSettings() cadence {
	return cadence{
		fastWindow: s.set.CadenceFastWindow(), fastInterval: s.set.CadenceFastInterval(),
		dailyWindow: s.set.CadenceDailyWindow(), dailyInterval: s.set.CadenceDailyInterval(),
		slowInterval: s.set.CadenceSlowInterval(),
	}
}

// searchDue implements the release-aged search cadence: within fastWindow of the
// release use fastInterval, within dailyWindow use dailyInterval, else
// slowInterval. A never-searched item (last == nil) is always due, so fresh
// requests search at once.
func searchDue(releaseDate string, last *time.Time, now time.Time, cad cadence) bool {
	if last == nil {
		return true
	}
	interval := cad.slowInterval
	if releaseDate != "" {
		if rd, err := time.Parse("2006-01-02", releaseDate); err == nil {
			switch age := now.Sub(rd); {
			case age < cad.fastWindow:
				interval = cad.fastInterval
			case age < cad.dailyWindow:
				interval = cad.dailyInterval
			}
		}
	}
	return now.Sub(*last) >= interval
}

// pickBest scores results with the configured selection score and returns the
// best non-rejected release — skipping, in score order, any release the language
// knowledge base has already verified to lack the wanted language (so re-searches
// work *down* from the top instead of re-grabbing a name-lies-about-German
// release). If every candidate is known-bad it still returns the top one.
func (s *Service) pickBest(ctx context.Context, results []prowlarr.ReleaseResource, kind string) (prowlarr.ReleaseResource, bool) {
	cfg := s.set.SelectionConfigFor(kind)
	ideal := idealLangs(cfg)
	type cand struct {
		rr prowlarr.ReleaseResource
		sc int
	}
	var cands []cand
	for _, rr := range results {
		sc := cfg.Score(selection.Release{
			Title: rr.Title, Protocol: rr.Protocol, Size: rr.Size,
			Seeders: rr.Seeders, Grabs: rr.Grabs, IndexerFlags: rr.IndexerFlags,
		})
		if sc < cfg.MinScore {
			continue
		}
		cands = append(cands, cand{rr, sc})
	}
	if len(cands) == 0 {
		return prowlarr.ReleaseResource{}, false
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].sc > cands[j].sc })
	for _, c := range cands {
		if !s.verifiedLacksLang(ctx, c.rr.Title, ideal, cfg.RequireAnyLanguage) {
			return c.rr, true
		}
	}
	return cands[0].rr, true // all known-bad — fall back to the highest score
}

// verifiedLacksLang reports whether the KB has recorded this release's real
// languages and they don't include the wanted language (the top preferred, or any
// preferred when requireAny). Unrecorded releases are never skipped.
func (s *Service) verifiedLacksLang(ctx context.Context, name string, ideal []string, requireAny bool) bool {
	if len(ideal) == 0 {
		return false
	}
	rl, _ := s.store.GetReleaseLang(ctx, name)
	if rl == nil {
		return false
	}
	have := rl.AudioLangs + "," + rl.SubLangs
	has := func(code string) bool {
		for _, l := range strings.Split(have, ",") {
			if strings.EqualFold(strings.TrimSpace(l), code) {
				return true
			}
		}
		return false
	}
	if requireAny {
		for _, code := range ideal {
			if has(code) {
				return false
			}
		}
		return true
	}
	return !has(ideal[0])
}

// grabBest stores the chosen release's artifact, dedups, and creates a pending job.
// upgrade=true marks the job as a language/quality replacement of an imported item.
func (s *Service) grabBest(ctx context.Context, rr prowlarr.ReleaseResource, mediaType string, mediaRef int64, upgrade bool) (*job.Job, error) {
	// Media-level dedup: if this item already has an in-flight job (e.g. a prior
	// cycle grabbed a different tracker's release, or an upgrade already running),
	// don't download it twice.
	if mediaRef > 0 {
		if existing, _ := s.store.ActiveJobForMedia(ctx, mediaType, mediaRef); existing != nil {
			return existing, nil
		}
	}
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
			IsUpgrade: upgrade,
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
		Protocol: "usenet", MediaType: mediaType, MediaRef: mediaRef, IsUpgrade: upgrade,
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
