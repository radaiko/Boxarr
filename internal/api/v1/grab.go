package v1

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/media"
)

// grabHTTP fetches release artifacts (.nzb / .torrent) from Prowlarr's proxy URL.
var grabHTTP = &http.Client{Timeout: 60 * time.Second}

// browserUA is sent on artifact downloads: a Prowlarr instance behind Cloudflare
// may "Browser Integrity Check" non-API paths (like /{id}/download) and 403 the
// default Go user-agent. A browser UA usually passes that check.
const browserUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

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
	// Present as a browser. Prowlarr 301-redirects usenet downloads to the real
	// indexer (e.g. NZBFinder), which sits behind Cloudflare; the default Go
	// user-agent trips Cloudflare's bot check and gets a 403 challenge page. A
	// browser UA + Accept headers pass it — this is what FlareSolverr does for the
	// *arr stack. Go preserves these headers across the cross-host redirect.
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", "application/x-nzb,application/x-bittorrent,application/octet-stream,*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	// When the download URL points at Prowlarr, authenticate: Prowlarr's /download
	// proxy requires the API key, and the URL doesn't always embed it.
	if base := strings.TrimRight(h.deps.Settings.ProwlarrURL(), "/"); base != "" && strings.HasPrefix(rawURL, base) {
		req.Header.Set("X-Api-Key", h.deps.Settings.ProwlarrAPIKey())
	}
	resp, err := grabHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	// resp.Request is the LAST request after redirects — the host that actually
	// served the response (Prowlarr redirects out to the indexer for NZBs).
	finalHost := ""
	if resp.Request != nil && resp.Request.URL != nil {
		finalHost = resp.Request.URL.Host
	}
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		slog.Default().Warn("artifact download failed", "finalHost", finalHost,
			"status", resp.StatusCode, "contentType", ct, "cloudflare", looksCloudflare(body, resp.Header), "body", msg)
		if looksCloudflare(body, resp.Header) {
			return nil, fmt.Errorf("status %d: Cloudflare blocked the download at %s — that indexer needs FlareSolverr or a Cloudflare allow-rule", resp.StatusCode, finalHost)
		}
		if msg != "" {
			return nil, fmt.Errorf("status %d (%s): %s", resp.StatusCode, finalHost, msg)
		}
		return nil, fmt.Errorf("status %d from %s", resp.StatusCode, finalHost)
	}
	// A 200 that's really an HTML error/challenge page, not a file.
	if strings.Contains(ct, "text/html") {
		slog.Default().Warn("artifact download returned HTML, not a file", "finalHost", finalHost,
			"contentType", ct, "cloudflare", looksCloudflare(body, resp.Header))
		if looksCloudflare(body, resp.Header) {
			return nil, fmt.Errorf("blocked by Cloudflare at %s (challenge page) — that indexer needs FlareSolverr or a Cloudflare allow-rule", finalHost)
		}
		return nil, fmt.Errorf("got an HTML page from %s (not an NZB/torrent — check the indexer download/auth)", finalHost)
	}
	return body, nil
}

// looksCloudflare reports whether a response is a Cloudflare block/challenge page
// (mirrors Prowlarr's CloudFlareDetectionService markers).
func looksCloudflare(body []byte, hdr http.Header) bool {
	if strings.Contains(strings.ToLower(hdr.Get("Server")), "cloudflare") {
		return true
	}
	b := strings.ToLower(string(body))
	return strings.Contains(b, "just a moment") ||
		strings.Contains(b, "attention required") ||
		strings.Contains(b, "error code: 1020") ||
		strings.Contains(b, "/cdn-cgi/") ||
		strings.Contains(b, "no-js ie6 oldie")
}
