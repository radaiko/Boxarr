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
			// Fetch the .torrent; if that's forbidden/unavailable (some indexers 403
			// the direct download), fall back to an infohash magnet — TorBox resolves
			// a torrent by its hash (cached or via DHT) without the .torrent file.
			if b, err := h.fetchArtifact(ctx, ref.DownloadURL); err == nil {
				j.TorrentFile = b
			} else if hash != "" {
				j.TorrentMagnet = magnetFromHash(hash)
			} else {
				return nil, false, fmt.Errorf("fetching .torrent: %w", err)
			}
		case hash != "":
			j.TorrentMagnet = magnetFromHash(hash) // hash only — let TorBox resolve it
		default:
			return nil, false, fmt.Errorf("release has no magnet, download URL, or infohash")
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
	b, err := h.fetchArtifact(ctx, ref.DownloadURL)
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

// magnetFromHash builds a minimal magnet URI from a torrent infohash. TorBox
// resolves it via its cache/DHT, so we don't need the .torrent file.
func magnetFromHash(hash string) string { return "magnet:?xt=urn:btih:" + hash }

func (h *Handler) fetchArtifact(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	// When the download URL points at Prowlarr, authenticate: Prowlarr's /download
	// proxy requires the API key, and the URL doesn't always embed it — a plain GET
	// then 403s. Harmless on non-Prowlarr URLs.
	if base := strings.TrimRight(h.deps.Settings.ProwlarrURL(), "/"); base != "" && strings.HasPrefix(rawURL, base) {
		req.Header.Set("X-Api-Key", h.deps.Settings.ProwlarrAPIKey())
	}
	resp, err := grabHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if resp.StatusCode >= 400 {
		// Surface the upstream message (Prowlarr/the indexer says why — bad key, VIP
		// required, link expired) instead of just the status code.
		msg := strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		if msg != "" {
			return nil, fmt.Errorf("status %d: %s", resp.StatusCode, msg)
		}
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return body, nil
}
