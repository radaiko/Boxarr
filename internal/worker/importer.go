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

	oldJobID := m.JobID
	m.LibraryPath = linkPath
	m.HasFile = true
	m.Status = media.MediaAvailable
	m.JobID = j.ID
	if err := w.store.UpdateMovie(ctx, m); err != nil {
		log.Error("updating movie after import", "error", err)
	}
	log.Info("movie imported", "library_path", linkPath)
	if j.IsUpgrade {
		w.supersedeOldDownload(ctx, j.ID, oldJobID)
	}

	w.notifyEvent(ctx, "download_completed", j, map[string]any{"title": m.Title, "libraryPath": linkPath})
	w.maybePlexScan(ctx, dir, "movie")
	return nil
}

// supersedeOldDownload deletes the TorBox download + job that an upgrade replaced
// (the new import already overwrote the symlink and re-owns the imported_symlink
// row, so only the orphaned old TorBox content is removed).
func (w *Workers) supersedeOldDownload(ctx context.Context, newJobID, oldJobID int64) {
	if oldJobID == 0 || oldJobID == newJobID {
		return
	}
	w.logger.Info("upgrade: removing superseded download", "old_job", oldJobID, "new_job", newJobID)
	w.DeleteDownloads(ctx, []int64{oldJobID}, func(int, int, string) {})
}

// ScanPlexLibraries triggers a full Plex scan of every configured library section
// (movies, tv, anime), deduped. The per-import partial scan only fires when an
// import happens; this lets a manual "Refresh from TorBox + Plex" make Plex pick up
// library changes on demand. Best-effort: per-section failures are logged, not fatal.
func (w *Workers) ScanPlexLibraries(ctx context.Context) error {
	if w.plex == nil || !w.set.PlexEnabled() {
		return nil
	}
	seen := map[string]bool{}
	for _, section := range []string{w.set.PlexMovieSection(), w.set.PlexTVSection(), w.set.PlexAnimeSection()} {
		if section == "" || seen[section] {
			continue
		}
		seen[section] = true
		if err := w.plex.ScanSection(ctx, section); err != nil {
			w.logger.Warn("plex library scan failed", "section", section, "error", err)
			continue
		}
		w.logger.Info("plex library scan triggered", "section", section)
	}
	return nil
}

// maybePlexScan triggers a best-effort Plex scan of the just-imported dir if Plex
// is wired and the section id is configured. Boxarr and Plex often mount the same
// storage at different paths (e.g. Boxarr "/mnt/library/tv", Plex "/mnt/smedia/tv"),
// and Plex's partial-scan endpoint silently no-ops on a path it doesn't recognize.
// So the Boxarr path is remapped onto Plex's reported library location for an
// efficient partial scan; when it can't be mapped unambiguously, a full section
// scan is used instead (Plex finds the new file via its own mount).
func (w *Workers) maybePlexScan(ctx context.Context, dir, kind string) {
	if w.plex == nil || !w.set.PlexEnabled() {
		return
	}
	section, root := w.set.PlexMovieSection(), w.set.MovieLibraryRoot()
	switch kind {
	case "tv":
		section, root = w.set.PlexTVSection(), w.set.TVLibraryRoot()
	case "anime":
		root = w.set.AnimeLibraryRoot()
		if section = w.set.PlexAnimeSection(); section == "" {
			section = w.set.PlexTVSection() // fall back to the TV section
		}
	}
	if section == "" {
		return
	}
	if target, ok := plexScanTarget(dir, root, w.plexLocations(ctx, section)); ok {
		if err := w.plex.ScanPath(ctx, section, target); err != nil {
			w.logger.Warn("plex scan failed", "dir", target, "error", err)
			return
		}
		w.logger.Info("plex partial scan triggered", "section", section, "path", target)
		return
	}
	// Path couldn't be mapped to Plex's mount — a full section scan still surfaces
	// the new file (Plex scans its own configured location).
	if err := w.plex.ScanSection(ctx, section); err != nil {
		w.logger.Warn("plex section scan failed", "section", section, "error", err)
		return
	}
	w.logger.Info("plex section scan triggered (path not mappable to Plex mount)", "section", section, "dir", dir)
}

// plexLocations returns a section's Plex-side library paths, memoized (they're
// effectively static for a server). Failures aren't cached, so a transient Plex
// error is retried on the next import.
func (w *Workers) plexLocations(ctx context.Context, section string) []string {
	if v, ok := w.plexLocCache.Load(section); ok {
		return v.([]string)
	}
	locs, err := w.plex.SectionLocations(ctx, section)
	if err != nil {
		return nil
	}
	w.plexLocCache.Store(section, locs)
	return locs
}

// plexScanTarget remaps a Boxarr library dir onto the path Plex sees, using the
// section's Plex-side locations. Returns (path, true) when the dir falls under the
// Boxarr root and maps unambiguously (exact match, matching basename, or a single
// Plex location); otherwise ("", false) so the caller does a full section scan.
func plexScanTarget(dir, boxarrRoot string, plexLocs []string) (string, bool) {
	boxarrRoot = strings.TrimRight(boxarrRoot, "/")
	if boxarrRoot == "" || !strings.HasPrefix(dir, boxarrRoot) || len(plexLocs) == 0 {
		return "", false
	}
	rel := dir[len(boxarrRoot):] // leading "/" retained
	for _, l := range plexLocs { // already the path Plex uses → no remap
		if strings.TrimRight(l, "/") == boxarrRoot {
			return dir, true
		}
	}
	base := filepath.Base(boxarrRoot)
	for _, l := range plexLocs { // same library folder name under a different mount
		if filepath.Base(strings.TrimRight(l, "/")) == base {
			return strings.TrimRight(l, "/") + rel, true
		}
	}
	if len(plexLocs) == 1 { // unambiguous single location
		return strings.TrimRight(plexLocs[0], "/") + rel, true
	}
	return "", false
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
