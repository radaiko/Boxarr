package v1

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/media"
)

// grabHTTP fetches release artifacts (.nzb / .torrent) from Prowlarr's proxy URL.
var grabHTTP = &http.Client{Timeout: 60 * time.Second}

func (h *Handler) grabMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	var body struct {
		ReleaseID string `json:"releaseId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ReleaseID == "" {
		h.writeError(w, http.StatusBadRequest, "bad_request", "releaseId is required")
		return
	}
	ref, err := decodeReleaseID(body.ReleaseID)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "not_found", "release token expired; re-search")
		return
	}
	ctx := r.Context()
	m, err := h.deps.Store.GetMovie(ctx, id)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "not_found", "movie not found")
		return
	}

	j, deduped, err := h.grabRelease(ctx, ref, "movie", m.ID)
	if err != nil {
		h.deps.Logger.Warn("grab failed", "movieId", id, "release", ref.Title, "protocol", ref.Protocol, "error", err.Error())
		h.writeError(w, http.StatusUnprocessableEntity, "unprocessable", err.Error())
		return
	}
	// Link the movie to the (new or existing) job and flag it queued.
	m.JobID = j.ID
	if m.Status != media.MediaAvailable {
		m.Status = media.MediaQueued // a job is queued on TorBox now
	}
	if uerr := h.deps.Store.UpdateMovie(ctx, m); uerr != nil {
		h.deps.Logger.Error("grab: linking movie to job", "error", uerr)
	}
	h.writeJSON(w, http.StatusAccepted, map[string]any{
		"jobId": j.ID, "state": string(j.State), "deduped": deduped,
	})
}

// grabRelease stores the artifact locally, dedups, and creates a pending job
// linked to the catalog item via (mediaType, mediaRef). Returns the existing
// job (deduped=true) when the same artifact was already grabbed.
func (h *Handler) grabRelease(ctx context.Context, ref grabRef, mediaType string, mediaRef int64) (*job.Job, bool, error) {
	category := mediaType
	st := h.deps.Store

	if ref.Protocol == "torrent" {
		hash := strings.ToLower(ref.InfoHash)
		if hash != "" {
			if existing, _ := st.FindByTorrentHash(ctx, hash, category); existing != nil {
				return existing, true, nil
			}
		}
		j := &job.Job{
			State: job.StatePending, Category: category, NZBName: ref.Title,
			Protocol: "torrent", MediaType: mediaType, MediaRef: mediaRef, TorrentHash: hash,
		}
		switch {
		case ref.MagnetURL != "":
			j.TorrentMagnet = ref.MagnetURL // no HTTP fetch needed
		case ref.DownloadURL != "":
			b, err := fetchArtifact(ctx, ref.DownloadURL)
			if err != nil {
				return nil, false, fmt.Errorf("fetching .torrent: %w", err)
			}
			j.TorrentFile = b
		default:
			return nil, false, fmt.Errorf("release has no magnet or download URL")
		}
		id, err := st.CreateJob(ctx, j)
		if err != nil {
			return nil, false, err
		}
		j.ID = id
		return j, false, nil
	}

	// usenet
	if ref.DownloadURL == "" {
		return nil, false, fmt.Errorf("usenet release has no download URL")
	}
	b, err := fetchArtifact(ctx, ref.DownloadURL)
	if err != nil {
		return nil, false, fmt.Errorf("fetching .nzb: %w", err)
	}
	sum := sha256.Sum256(b)
	sha := hex.EncodeToString(sum[:])
	if existing, _ := st.FindBySHA256(ctx, sha, category); existing != nil {
		return existing, true, nil
	}
	j := &job.Job{
		State: job.StatePending, Category: category, NZBName: ref.Title,
		NZBContent: b, NZBSHA256: sha, NZBURL: ref.DownloadURL,
		Protocol: "usenet", MediaType: mediaType, MediaRef: mediaRef,
	}
	id, err := st.CreateJob(ctx, j)
	if err != nil {
		return nil, false, err
	}
	j.ID = id
	return j, false, nil
}

func fetchArtifact(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := grabHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}
