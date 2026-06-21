package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/prowlarr"
	"github.com/radaiko/boxarr/internal/selection"
	"github.com/radaiko/boxarr/internal/torbox"
)

// resubmitStored resubmits a job's stored artifact (the FR-HEAL-1 path) and
// returns the new TorBox id + hash. It errors when no artifact is stored or the
// TorBox create call fails, so the caller can decide on a re-search fallback.
func (w *Workers) resubmitStored(ctx context.Context, j *job.Job) (int64, string, error) {
	if j.Protocol == "torrent" {
		if j.TorrentMagnet == "" && len(j.TorrentFile) == 0 {
			return 0, "", fmt.Errorf("no stored torrent artifact")
		}
		res, err := w.tb.CreateTorrent(ctx, torbox.TorrentCreateRequest{
			Magnet: j.TorrentMagnet, TorrentContent: j.TorrentFile, TorrentName: j.NZBName,
		})
		if err != nil {
			return 0, "", err
		}
		id := int64(res.TorrentID)
		if id == 0 {
			id = int64(res.QueuedID)
		}
		return id, res.Hash, nil
	}
	if len(j.NZBContent) == 0 && j.NZBURL == "" {
		return 0, "", fmt.Errorf("no stored NZB artifact")
	}
	res, err := w.tb.CreateUsenetDownload(ctx, torbox.CreateRequest{
		NZBContent: j.NZBContent, NZBName: j.NZBName + ".nzb", Link: j.NZBURL,
	})
	if err != nil {
		return 0, "", err
	}
	return int64(res.UsenetDownloadID), res.Hash, nil
}

// researchAndResubmit re-searches Prowlarr for the job's title, grabs the best
// release, resubmits it to TorBox, and rewrites the job's artifact fields so a
// subsequent finishHeal repoints the broken symlinks at the new release.
func (w *Workers) researchAndResubmit(ctx context.Context, j *job.Job) (int64, string, error) {
	query, cats := w.healQuery(ctx, j)
	typ := "search"
	if len(cats) == 1 && cats[0] == 2000 {
		typ = "movie"
	} else if len(cats) == 1 && cats[0] == 5000 {
		typ = "tvsearch"
	}
	results, err := w.set.Prowlarr().Search(ctx, prowlarr.SearchParams{Query: query, Type: typ, Categories: cats})
	if err != nil {
		return 0, "", fmt.Errorf("prowlarr search: %w", err)
	}
	best, ok := pickBestRelease(w.set.SelectionConfig(), results)
	if !ok {
		return 0, "", fmt.Errorf("no acceptable release for %q", query)
	}

	if best.Protocol == "torrent" {
		j.Protocol = "torrent"
		j.TorrentHash = strings.ToLower(best.InfoHash)
		j.NZBContent, j.NZBURL, j.NZBSHA256 = nil, "", ""
		if best.MagnetURL != "" {
			j.TorrentMagnet, j.TorrentFile = best.MagnetURL, nil
		} else if best.DownloadURL != "" {
			b, ferr := w.fetchArtifact(ctx, best.DownloadURL)
			if ferr != nil {
				return 0, "", ferr
			}
			j.TorrentFile, j.TorrentMagnet = b, ""
		} else {
			return 0, "", fmt.Errorf("torrent release has no magnet/url")
		}
		res, cerr := w.tb.CreateTorrent(ctx, torbox.TorrentCreateRequest{
			Magnet: j.TorrentMagnet, TorrentContent: j.TorrentFile, TorrentName: best.Title,
		})
		if cerr != nil {
			return 0, "", cerr
		}
		id := int64(res.TorrentID)
		if id == 0 {
			id = int64(res.QueuedID)
		}
		return id, res.Hash, nil
	}

	if best.DownloadURL == "" {
		return 0, "", fmt.Errorf("usenet release has no url")
	}
	b, ferr := w.fetchArtifact(ctx, best.DownloadURL)
	if ferr != nil {
		return 0, "", ferr
	}
	sum := sha256.Sum256(b)
	j.Protocol = "usenet"
	j.NZBContent, j.NZBURL, j.NZBSHA256 = b, best.DownloadURL, hex.EncodeToString(sum[:])
	j.TorrentMagnet, j.TorrentFile, j.TorrentHash = "", nil, ""
	res, cerr := w.tb.CreateUsenetDownload(ctx, torbox.CreateRequest{
		NZBContent: b, NZBName: best.Title + ".nzb", Link: best.DownloadURL,
	})
	if cerr != nil {
		return 0, "", cerr
	}
	return int64(res.UsenetDownloadID), res.Hash, nil
}

// healQuery derives a search query + Prowlarr categories from the job's media
// link (precise), falling back to the stored release name.
func (w *Workers) healQuery(ctx context.Context, j *job.Job) (string, []int) {
	switch j.MediaType {
	case "movie":
		if m, err := w.store.GetMovie(ctx, j.MediaRef); err == nil {
			if m.Year > 0 {
				return fmt.Sprintf("%s %d", m.Title, m.Year), []int{2000}
			}
			return m.Title, []int{2000}
		}
	case "episode":
		if ep, err := w.store.GetEpisode(ctx, j.MediaRef); err == nil {
			if sr, serr := w.store.GetSeries(ctx, ep.SeriesID); serr == nil {
				return fmt.Sprintf("%s S%02dE%02d", sr.Title, ep.SeasonNumber, ep.EpisodeNumber), []int{5000}
			}
		}
	case "season", "series":
		if sr, err := w.store.GetSeries(ctx, j.MediaRef); err == nil {
			return sr.Title, []int{5000}
		}
	}
	return j.NZBName, nil
}

// pickBestRelease scores releases and returns the best non-rejected one.
func pickBestRelease(cfg selection.Config, results []prowlarr.ReleaseResource) (prowlarr.ReleaseResource, bool) {
	bestIdx, bestScore := -1, 0
	for i, rr := range results {
		sc := cfg.Score(selection.Release{
			Title: rr.Title, Protocol: rr.Protocol, Size: rr.Size,
			Seeders: rr.Seeders, Grabs: rr.Grabs, IndexerFlags: rr.IndexerFlags,
		})
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

func (w *Workers) fetchArtifact(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := w.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}
