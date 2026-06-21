package worker

import (
	"context"
	"fmt"

	"github.com/radaiko/boxarr/internal/job"
)

// AdoptResolver resolves an unknown release folder name to a catalog link,
// creating the catalog row if needed (satisfied by *catalog.Service).
type AdoptResolver interface {
	ResolveAdopt(ctx context.Context, name string) (mediaType string, mediaRef int64, err error)
}

// SetAdoptResolver wires the catalog resolver used by AdoptUnknown.
func (w *Workers) SetAdoptResolver(a AdoptResolver) { w.adoptResolver = a }

// AdoptUnknown imports an already-present (unknown) WebDAV release into the
// library (FR-NC-2 adopt): it resolves/creates the catalog entry, links the
// existing TorBox download (so a later delete propagates), synthesizes a job, and
// runs the normal importer over the existing folder — then marks the item known.
func (w *Workers) AdoptUnknown(ctx context.Context, remotePath, name string) error {
	if w.adoptResolver == nil {
		return fmt.Errorf("adopt: no resolver configured")
	}
	mediaType, mediaRef, err := w.adoptResolver.ResolveAdopt(ctx, name)
	if err != nil {
		return fmt.Errorf("adopt: resolving %q: %w", name, err)
	}

	j := &job.Job{
		State: job.StateQueued, Category: mediaType, NZBName: name,
		MediaType: mediaType, MediaRef: mediaRef, StoragePath: remotePath,
	}
	// Link the existing TorBox download by folder name so deletion can propagate.
	w.linkExistingTorBoxDownload(ctx, j, name)

	id, err := w.store.CreateJob(ctx, j)
	if err != nil {
		return fmt.Errorf("adopt: creating job: %w", err)
	}
	j.ID = id

	if err := w.importMedia(ctx, j, remotePath); err != nil {
		// Roll back any partial import so a retry starts clean: remove on-disk
		// symlinks recorded for this job, revert the catalog rows it touched, then
		// drop the placeholder job (do disk/catalog revert BEFORE DeleteJob so the
		// imported_symlinks rows, ON DELETE CASCADE, are still queryable).
		w.rollbackImport(ctx, id)
		return fmt.Errorf("adopt: importing %q: %w", name, err)
	}
	if err := w.store.SetWebDAVItemKnown(ctx, remotePath, true); err != nil {
		w.logger.Warn("adopt: marking item known", "remote_path", remotePath, "error", err)
	}
	w.logger.Info("adopted unknown content", "name", name, "media_type", mediaType, "media_ref", mediaRef)
	return nil
}

// linkExistingTorBoxDownload sets j.TorBoxID + j.Protocol when a TorBox download
// with this folder name exists (torrents checked first, then usenet). Best-effort;
// it warns if neither list could be queried so a lost delete-propagation linkage
// is surfaced rather than silently swallowed.
func (w *Workers) linkExistingTorBoxDownload(ctx context.Context, j *job.Job, name string) {
	torrents, terr := w.tb.ListTorrents(ctx)
	if terr == nil {
		for _, d := range torrents {
			if d.Name == name {
				j.TorBoxID, j.Protocol, j.TorBoxHash = int64(d.ID), "torrent", d.Hash
				return
			}
		}
	}
	usenet, uerr := w.tb.ListUsenet(ctx)
	if uerr == nil {
		for _, d := range usenet {
			if d.Name == name {
				j.TorBoxID, j.Protocol, j.TorBoxHash = int64(d.ID), "usenet", d.Hash
				return
			}
		}
	}
	if terr != nil && uerr != nil {
		w.logger.Warn("adopt: could not query TorBox to link download; deletes will not propagate",
			"name", name, "torrents_error", terr, "usenet_error", uerr)
	}
}

// rollbackImport undoes a partially-completed import for a job: it unlinks each
// recorded library symlink from disk, reverts the catalog rows the import
// touched, and then drops the job row.
func (w *Workers) rollbackImport(ctx context.Context, jobID int64) {
	if syms, err := w.store.ListImportedSymlinks(ctx); err == nil {
		for _, s := range syms {
			if s.JobID == jobID {
				if rerr := removeLibrarySymlink(s.SymlinkPath); rerr != nil {
					w.logger.Warn("rollback: removing symlink", "path", s.SymlinkPath, "error", rerr)
				}
			}
		}
	}
	if err := w.store.ResetImportLinks(ctx, jobID); err != nil {
		w.logger.Error("rollback: resetting catalog links", "job_id", jobID, "error", err)
	}
	if err := w.store.DeleteJob(ctx, jobID); err != nil {
		w.logger.Error("rollback: deleting job", "job_id", jobID, "error", err)
	}
}
