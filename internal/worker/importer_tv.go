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
		root = w.cfg.TVLibraryRoot
	}
	seriesFolder := movieFolderName(sr.Title, sr.Year) // "Title (Year)"

	imported := 0
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
		linkPath := w.tvLinkPath(root, seriesFolder, sr.Title, targets[0], filepath.Ext(video))
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
	w.maybePlexScan(ctx, filepath.Join(root, seriesFolder), "tv")
	return nil
}

// tvLinkPath builds the Plex-standard episode path:
// <root>/<Series (Year)>/Season NN/<Series> - S01E02[-E03] - <Title>.ext
func (w *Workers) tvLinkPath(root, seriesFolder, seriesTitle string, ep *media.Episode, ext string) string {
	seasonDir := fmt.Sprintf("Season %02d", ep.SeasonNumber)
	tag := fmt.Sprintf("S%02dE%02d", ep.SeasonNumber, ep.EpisodeNumber)
	name := fmt.Sprintf("%s - %s", sanitizePathComponent(seriesTitle), tag)
	if ep.Title != "" {
		name += " - " + sanitizePathComponent(ep.Title)
	}
	return filepath.Join(root, seriesFolder, seasonDir, name+strings.ToLower(ext))
}

// matchEpisodes maps a parsed file to catalog episodes: a single episode, a
// range/adjacent multi-episode (EpisodeStart..EpisodeEnd), or (for a season
// pack with no per-file episode) nothing here — pack files always carry SxxEyy.
func matchEpisodes(p *release.ParsedRelease, episodes []*media.Episode) []*media.Episode {
	if p.EpisodeStart == 0 {
		return nil
	}
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
	return out
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
