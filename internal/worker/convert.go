package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/radaiko/boxarr/internal/job"
)

// ConvertSeriesType switches a series between "standard" and "anime", relocating
// its already-imported library symlinks from the old library root to the new one
// (the symlink targets are unchanged — only the link location moves), updating the
// catalog + symlink rows, and rescanning Plex. Future grabs use the new root via
// the series' RootFolderPath.
func (w *Workers) ConvertSeriesType(ctx context.Context, seriesID int64, newType string) error {
	if newType != "anime" {
		newType = "standard"
	}
	sr, err := w.store.GetSeries(ctx, seriesID)
	if err != nil {
		return fmt.Errorf("loading series %d: %w", seriesID, err)
	}
	if sr.SeriesType == newType {
		return nil // already the requested type
	}
	newRoot := w.set.TVLibraryRoot()
	if newType == "anime" {
		newRoot = w.set.AnimeLibraryRoot()
	}
	oldRoot := sr.RootFolderPath
	if oldRoot == "" {
		oldRoot = w.set.TVLibraryRoot()
		if sr.SeriesType == "anime" {
			oldRoot = w.set.AnimeLibraryRoot()
		}
	}

	if newRoot != "" && newRoot != oldRoot {
		syms, _ := w.store.ListImportedSymlinks(ctx)
		byPath := make(map[string]*job.ImportedSymlink, len(syms))
		for _, s := range syms {
			byPath[s.SymlinkPath] = s
		}
		eps, _ := w.store.ListEpisodes(ctx, seriesID)
		for _, ep := range eps {
			if ep.LibraryPath == "" || !strings.HasPrefix(ep.LibraryPath, oldRoot) {
				continue
			}
			oldPath := ep.LibraryPath
			newPath := newRoot + strings.TrimPrefix(oldPath, oldRoot)
			target, terr := os.Readlink(oldPath)
			if terr != nil { // link gone — fall back to the recorded target
				if s := byPath[oldPath]; s != nil {
					target = s.TargetPath
				}
			}
			if target != "" {
				if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
					w.logger.Warn("convert: creating dir", "path", newPath, "error", err)
					continue
				}
				if err := atomicReplaceSymlink(newPath, target); err != nil {
					w.logger.Warn("convert: relinking", "path", newPath, "error", err)
					continue
				}
				_ = removeLibrarySymlink(oldPath)
			}
			ep.LibraryPath = newPath
			if err := w.store.UpdateEpisode(ctx, ep); err != nil {
				w.logger.Warn("convert: updating episode", "id", ep.ID, "error", err)
			}
			if s := byPath[oldPath]; s != nil {
				_ = w.store.DeleteImportedSymlink(ctx, s.ID)
				_ = w.store.UpsertImportedSymlink(ctx, &job.ImportedSymlink{
					JobID: s.JobID, SymlinkPath: newPath, TargetPath: s.TargetPath,
				})
			}
		}
		// Refresh both Plex libraries: the new one to pick the show up, the old to
		// drop it.
		w.maybePlexScan(ctx, newRoot, plexKind(newType))
		w.maybePlexScan(ctx, oldRoot, plexKind(sr.SeriesType))
	}

	sr.SeriesType = newType
	sr.RootFolderPath = newRoot
	if err := w.store.UpdateSeries(ctx, sr); err != nil {
		return fmt.Errorf("updating series: %w", err)
	}
	w.logger.Info("converted series type", "series_id", seriesID, "to", newType, "root", newRoot)
	return nil
}

func plexKind(seriesType string) string {
	if seriesType == "anime" {
		return "anime"
	}
	return "tv"
}
