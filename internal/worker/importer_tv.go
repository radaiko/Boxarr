package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/release"
)

// importEpisodes imports a completed TV release: it enumerates the release
// folder's video files, parses each, maps it to a catalog episode, and writes a
// library symlink per covered episode (00 §5.1 / FR-IMP-4). A single-episode job
// links to one episode; a season/series pack expands across many.
func (w *Workers) importEpisodes(ctx context.Context, j *job.Job, sourceDir string) error {
	log := w.logger.With("job_id", j.ID, "series_ref", j.MediaRef)

	// Resolve the series from the linked episode (single) or directly (pack).
	var seriesID int64
	switch j.MediaType {
	case "episode":
		ep, err := w.store.GetEpisode(ctx, j.MediaRef)
		if err != nil {
			return fmt.Errorf("loading episode %d: %w", j.MediaRef, err)
		}
		seriesID = ep.SeriesID
	default: // season | series
		seriesID = j.MediaRef
	}
	sr, err := w.store.GetSeries(ctx, seriesID)
	if err != nil {
		return fmt.Errorf("loading series %d: %w", seriesID, err)
	}
	episodes, err := w.store.ListEpisodes(ctx, seriesID)
	if err != nil {
		return fmt.Errorf("listing episodes: %w", err)
	}

	videos, err := allVideos(sourceDir)
	if err != nil {
		return err
	}
	if len(videos) == 0 {
		return fmt.Errorf("no video files in %q", sourceDir)
	}

	root := sr.RootFolderPath
	if root == "" {
		root = w.set.TVLibraryRoot()
	}
	seriesFolder := movieFolderName(sr.Title, sr.Year) // "Title (Year)"

	imported := 0
	oldJobs := map[int64]bool{} // previous jobs an upgrade replaces (deleted after)
	for _, video := range videos {
		parsed, perr := release.ParseRelease(filepath.Base(video))
		if perr != nil {
			log.Warn("parse failed; skipping file", "file", video, "error", perr)
			continue
		}
		targets := matchEpisodes(parsed, episodes)
		if len(targets) == 0 {
			log.Warn("no episode match for file; left unimported", "file", filepath.Base(video))
			continue
		}
		linkPath := w.tvLinkPath(root, seriesFolder, sr.Title, targets, filepath.Ext(video))
		if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
			return fmt.Errorf("creating season dir: %w", err)
		}
		if err := atomicReplaceSymlink(linkPath, video); err != nil {
			return fmt.Errorf("linking %q: %w", linkPath, err)
		}
		if err := w.store.UpsertImportedSymlink(ctx, &job.ImportedSymlink{
			JobID: j.ID, SymlinkPath: linkPath, TargetPath: video,
		}); err != nil {
			log.Error("recording imported symlink", "error", err)
		}
		// Every covered episode points at the (possibly multi-ep) file.
		for _, ep := range targets {
			if ep.JobID != 0 && ep.JobID != j.ID {
				oldJobs[ep.JobID] = true
			}
			ep.LibraryPath = linkPath
			ep.HasFile = true
			ep.Status = media.MediaAvailable
			ep.JobID = j.ID
			if err := w.store.UpdateEpisode(ctx, ep); err != nil {
				log.Error("updating episode after import", "episode_id", ep.ID, "error", err)
			}
		}
		imported++
	}
	if imported == 0 {
		return fmt.Errorf("no files could be mapped to episodes in %q", sourceDir)
	}

	now := time.Now()
	j.State = job.StateImported
	j.StoragePath = sourceDir
	j.ProgressPct = 100
	j.CompletedAt = &now
	if err := w.store.UpdateJob(ctx, j); err != nil {
		return fmt.Errorf("marking job imported: %w", err)
	}
	log.Info("series release imported", "files", imported)
	if j.IsUpgrade {
		for old := range oldJobs {
			w.supersedeOldDownload(ctx, j.ID, old)
		}
	}
	w.notifyEvent(ctx, "download_completed", j, map[string]any{"title": sr.Title, "files": imported})
	kind := "tv"
	if sr.SeriesType == "anime" {
		kind = "anime"
	}
	w.maybePlexScan(ctx, filepath.Join(root, seriesFolder), kind)
	return nil
}

// tvLinkPath builds the Plex-standard episode path:
// <root>/<Series (Year)>/Season NN/<Series> - S01E02[-E03] - <Title>.ext
// eps is the (ordered) set of catalog episodes the file covers; a multi-episode
// file gets an SxxEyy-Ezz range tag so Plex recognizes every episode.
func (w *Workers) tvLinkPath(root, seriesFolder, seriesTitle string, eps []*media.Episode, ext string) string {
	first := eps[0]
	last := eps[len(eps)-1]
	seasonDir := fmt.Sprintf("Season %02d", first.SeasonNumber)
	tag := fmt.Sprintf("S%02dE%02d", first.SeasonNumber, first.EpisodeNumber)
	if last.SeasonNumber == first.SeasonNumber && last.EpisodeNumber > first.EpisodeNumber {
		tag += fmt.Sprintf("-E%02d", last.EpisodeNumber)
	}
	name := fmt.Sprintf("%s - %s", sanitizePathComponent(seriesTitle), tag)
	if first.Title != "" {
		name += " - " + sanitizePathComponent(first.Title)
	}
	return filepath.Join(root, seriesFolder, seasonDir, name+strings.ToLower(ext))
}

// matchEpisodes maps a parsed file to catalog episodes. It tries, in order:
// standard S/E numbering (incl. multi-episode ranges), anime absolute numbering,
// then daily/date-based air-date matching — so anime and daily shows aren't
// silently dropped (a season-pack file with none of these carries no episode).
func matchEpisodes(p *release.ParsedRelease, episodes []*media.Episode) []*media.Episode {
	// 1. Standard SxxEyy (and adjacent multi-episode ranges).
	if p.EpisodeStart > 0 {
		end := p.EpisodeEnd
		if end < p.EpisodeStart {
			end = p.EpisodeStart
		}
		var out []*media.Episode
		for n := p.EpisodeStart; n <= end; n++ {
			for _, ep := range episodes {
				if ep.SeasonNumber == p.SeasonNumber && ep.EpisodeNumber == n {
					out = append(out, ep)
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	// 2. Anime absolute numbering.
	if len(p.AbsoluteEpisodes) > 0 {
		var out []*media.Episode
		for _, abs := range p.AbsoluteEpisodes {
			for _, ep := range episodes {
				if ep.AbsoluteNumber > 0 && ep.AbsoluteNumber == abs {
					out = append(out, ep)
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	// 3. Daily / date-based shows.
	if p.AirDate != "" {
		for _, ep := range episodes {
			if ep.AirDate != "" && ep.AirDate == p.AirDate {
				return []*media.Episode{ep}
			}
		}
	}
	return nil
}

// allVideos returns every video file under dir (recursively), sorted by path.
func allVideos(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && videoExts[strings.ToLower(filepath.Ext(path))] {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}
