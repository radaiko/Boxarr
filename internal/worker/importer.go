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
)

// videoExts are the extensions the importer treats as the playable file.
var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".avi": true, ".m4v": true,
	".ts": true, ".mov": true, ".wmv": true, ".mpg": true, ".mpeg": true,
}

// importMedia imports a completed media job into the library by writing a symlink
// directly at its final Plex path (00 §5.1: no _incoming farm, no byte copy),
// updates the catalog item, records the link for the healer, and transitions the
// job to imported. Phase 1 handles movies; episodes/packs land in Phase 2.
func (w *Workers) importMedia(ctx context.Context, j *job.Job, sourceDir string) error {
	switch j.MediaType {
	case "movie":
		return w.importMovie(ctx, j, sourceDir)
	case "episode", "season", "series":
		return w.importEpisodes(ctx, j, sourceDir)
	default:
		return fmt.Errorf("import: unsupported media_type %q (job %d)", j.MediaType, j.ID)
	}
}

func (w *Workers) importMovie(ctx context.Context, j *job.Job, sourceDir string) error {
	log := w.logger.With("job_id", j.ID, "movie_id", j.MediaRef)
	m, err := w.store.GetMovie(ctx, j.MediaRef)
	if err != nil {
		return fmt.Errorf("loading movie %d: %w", j.MediaRef, err)
	}
	video, err := largestVideo(sourceDir)
	if err != nil {
		return err
	}
	root := m.RootFolderPath
	if root == "" {
		root = w.set.MovieLibraryRoot()
	}
	folder := movieFolderName(m.Title, m.Year)
	dir := filepath.Join(root, folder)
	linkPath := filepath.Join(dir, folder+strings.ToLower(filepath.Ext(video)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating library dir %q: %w", dir, err)
	}
	if err := atomicReplaceSymlink(linkPath, video); err != nil {
		return fmt.Errorf("linking %q -> %q: %w", linkPath, video, err)
	}
	if err := w.store.UpsertImportedSymlink(ctx, &job.ImportedSymlink{
		JobID: j.ID, SymlinkPath: linkPath, TargetPath: video,
	}); err != nil {
		log.Error("recording imported symlink", "error", err)
	}

	now := time.Now()
	j.State = job.StateImported
	j.StoragePath = sourceDir
	j.ProgressPct = 100
	j.CompletedAt = &now
	if err := w.store.UpdateJob(ctx, j); err != nil {
		return fmt.Errorf("marking job imported: %w", err)
	}

	m.LibraryPath = linkPath
	m.HasFile = true
	m.Status = media.MediaAvailable
	if err := w.store.UpdateMovie(ctx, m); err != nil {
		log.Error("updating movie after import", "error", err)
	}
	log.Info("movie imported", "library_path", linkPath)

	w.notifyEvent(ctx, "download_completed", j, map[string]any{"title": m.Title, "libraryPath": linkPath})
	w.maybePlexScan(ctx, dir, "movie")
	return nil
}

// maybePlexScan triggers a best-effort Plex partial scan of dir if Plex is wired
// and the relevant section id is configured.
func (w *Workers) maybePlexScan(ctx context.Context, dir, kind string) {
	if w.plex == nil || !w.set.PlexEnabled() {
		return
	}
	section := w.set.PlexMovieSection()
	switch kind {
	case "tv":
		section = w.set.PlexTVSection()
	case "anime":
		if section = w.set.PlexAnimeSection(); section == "" {
			section = w.set.PlexTVSection() // fall back to the TV section
		}
	}
	if section == "" {
		return
	}
	if err := w.plex.ScanPath(ctx, section, dir); err != nil {
		w.logger.Warn("plex scan failed", "dir", dir, "error", err)
	}
}

// largestVideo returns the largest video file under dir (recursively).
func largestVideo(dir string) (string, error) {
	var best string
	var bestSize int64 = -1
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !videoExts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		if info.Size() > bestSize {
			best, bestSize = path, info.Size()
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("scanning %q: %w", dir, err)
	}
	if best == "" {
		return "", fmt.Errorf("no video file in %q", dir)
	}
	return best, nil
}

// movieFolderName builds the Plex-standard "Title (Year)" with illegal path
// characters sanitized.
func movieFolderName(title string, year int) string {
	name := sanitizePathComponent(title)
	if year > 0 {
		name = fmt.Sprintf("%s (%d)", name, year)
	}
	return name
}

var illegalPathChars = strings.NewReplacer(
	"/", " ", `\`, " ", ":", " ", "*", " ", "?", " ", `"`, " ", "<", " ", ">", " ", "|", " ")

func sanitizePathComponent(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f { // NUL/control chars → space (else Symlink/MkdirAll fail)
			return ' '
		}
		return r
	}, s)
	s = illegalPathChars.Replace(s)
	s = strings.Join(strings.Fields(s), " ") // collapse whitespace runs
	s = strings.TrimRight(strings.TrimSpace(s), ". ")
	if s == "" { // empty/dot-only title would make a blank/hidden path component
		return "untitled"
	}
	return s
}
