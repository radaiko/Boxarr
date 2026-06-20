package torbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
)

// TorrentCreateRequest describes a torrent to submit. Exactly one of Magnet or
// TorrentContent should be set; precedence is hash > magnet > file.
type TorrentCreateRequest struct {
	Magnet         string
	TorrentContent []byte // raw .torrent bytes -> multipart "file"
	TorrentName    string
	Seed           int // 1=auto (default), 2=always, 3=never; omitted when 0
	AllowZip       bool
	AsQueued       bool
}

// TorrentCreateResult is the data payload of a create-torrent response.
type TorrentCreateResult struct {
	TorrentID FlexInt `json:"torrent_id"`
	QueuedID  FlexInt `json:"queued_id"`
	Hash      string  `json:"hash"`
	AuthID    string  `json:"auth_id"`
}

// TorrentDownload is one record from the TorBox torrents/mylist endpoint.
type TorrentDownload struct {
	ID               FlexInt       `json:"id"`
	Hash             string        `json:"hash"`
	Name             string        `json:"name"`
	Magnet           string        `json:"magnet"`
	Size             int64         `json:"size"`
	DownloadState    string        `json:"download_state"`
	DownloadFinished bool          `json:"download_finished"`
	DownloadPresent  bool          `json:"download_present"`
	Progress         float64       `json:"progress"`
	DownloadSpeed    float64       `json:"download_speed"`
	UploadSpeed      float64       `json:"upload_speed"`
	Seeds            int           `json:"seeds"`
	Peers            int           `json:"peers"`
	Ratio            float64       `json:"ratio"`
	ETA              float64       `json:"eta"`
	Active           bool          `json:"active"`
	ExpiresAt        string        `json:"expires_at"`
	Files            []TorrentFile `json:"files"`
}

// TorrentFile is one file inside a TorrentDownload.
type TorrentFile struct {
	ID                FlexInt `json:"id"`
	Name              string  `json:"name"`
	ShortName         string  `json:"short_name"`
	Size              int64   `json:"size"`
	MimeType          string  `json:"mimetype"`
	OpenSubtitlesHash string  `json:"opensubtitles_hash"`
}

// CachedCheck is one entry of a checkcached (format=list) response.
type CachedCheck struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Hash string `json:"hash"`
}

// User is the trimmed /user/me account payload. Plan is a FlexInt because the
// API/SDKs disagree on whether the tier serializes as an int, string, or float.
type User struct {
	ID               FlexInt `json:"id"`
	AuthID           string  `json:"auth_id"`
	Email            string  `json:"email"`
	Plan             FlexInt `json:"plan"`
	IsSubscribed     bool    `json:"is_subscribed"`
	PremiumExpiresAt string  `json:"premium_expires_at"`
	CooldownUntil    string  `json:"cooldown_until"`
	TotalDownloaded  int64   `json:"total_downloaded"`
}

// ProgressPct converts the 0.0-1.0 progress float to a 0-100 integer.
func (d TorrentDownload) ProgressPct() int {
	p := d.Progress
	if p > 1 {
		return int(p + 0.5)
	}
	return int(p*100 + 0.5)
}

// Failed reports whether the torrent is in a TorBox error/failed/stalled state.
// Matches by prefix/keyword (e.g. "stalled (no seeds)").
func (d TorrentDownload) Failed() bool {
	s := strings.ToLower(strings.TrimSpace(d.DownloadState))
	return strings.HasPrefix(s, "failed") ||
		strings.HasPrefix(s, "error") ||
		strings.Contains(s, "stalled")
}

// ETASeconds returns TorBox's estimated seconds remaining, clamped to >= 0.
func (d TorrentDownload) ETASeconds() int64 {
	if d.ETA <= 0 {
		return 0
	}
	return int64(d.ETA)
}

// DownloadedBytes estimates transferred bytes from progress and total size.
func (d TorrentDownload) DownloadedBytes() int64 {
	if d.Size <= 0 {
		return 0
	}
	p := d.Progress
	if p > 1 {
		p /= 100
	}
	return int64(float64(d.Size) * p)
}

// CreateTorrent submits a magnet or .torrent and returns the created download.
func (c *Client) CreateTorrent(ctx context.Context, req TorrentCreateRequest) (*TorrentCreateResult, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if req.Magnet != "" {
		_ = mw.WriteField("magnet", req.Magnet)
	}
	if len(req.TorrentContent) > 0 {
		fw, err := mw.CreateFormFile("file", "upload.torrent")
		if err != nil {
			return nil, fmt.Errorf("creating multipart file: %w", err)
		}
		if _, err := fw.Write(req.TorrentContent); err != nil {
			return nil, fmt.Errorf("writing torrent content: %w", err)
		}
	}
	if req.TorrentName != "" {
		_ = mw.WriteField("name", req.TorrentName)
	}
	if req.Seed > 0 {
		_ = mw.WriteField("seed", strconv.Itoa(req.Seed))
	}
	if req.AllowZip {
		_ = mw.WriteField("allow_zip", "true")
	}
	if req.AsQueued {
		_ = mw.WriteField("as_queued", "true")
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("closing multipart writer: %w", err)
	}
	env, err := c.do(ctx, http.MethodPost, "/torrents/createtorrent", mw.FormDataContentType(), &body)
	if err != nil {
		return nil, err
	}
	var res TorrentCreateResult
	if len(env.Data) > 0 && string(env.Data) != "null" {
		if err := json.Unmarshal(env.Data, &res); err != nil {
			return nil, fmt.Errorf("decoding create torrent result: %w", err)
		}
	}
	return &res, nil
}

// ListTorrents returns every torrent download on the account.
func (c *Client) ListTorrents(ctx context.Context) ([]TorrentDownload, error) {
	env, err := c.do(ctx, http.MethodGet, "/torrents/mylist?bypass_cache=true", "", nil)
	if err != nil {
		return nil, err
	}
	var list []TorrentDownload
	if len(env.Data) > 0 && string(env.Data) != "null" {
		if err := json.Unmarshal(env.Data, &list); err != nil {
			return nil, fmt.Errorf("decoding torrent list: %w", err)
		}
	}
	return list, nil
}

// ControlTorrent performs an operation (delete, pause, resume, reannounce) on a
// torrent download. The dangerous "all" flag is never set.
func (c *Client) ControlTorrent(ctx context.Context, id int64, op string) error {
	payload, err := json.Marshal(map[string]any{"torrent_id": id, "operation": op})
	if err != nil {
		return fmt.Errorf("encoding control request: %w", err)
	}
	_, err = c.do(ctx, http.MethodPost, "/torrents/controltorrent",
		"application/json", bytes.NewReader(payload))
	return err
}

// CheckCached reports which of the given info-hashes are cached on TorBox
// ("instant"). Absent hashes are not cached. It requests format=list and
// tolerates a hash-keyed object response as a fallback.
func (c *Client) CheckCached(ctx context.Context, hashes []string) ([]CachedCheck, error) {
	if len(hashes) == 0 {
		return nil, nil
	}
	path := "/torrents/checkcached?hash=" + strings.Join(hashes, ",") + "&format=list&list_files=false"
	env, err := c.do(ctx, http.MethodGet, path, "", nil)
	if err != nil {
		return nil, err
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return nil, nil
	}
	var list []CachedCheck
	if err := json.Unmarshal(env.Data, &list); err == nil {
		return list, nil
	}
	// Fallback: object keyed by lowercase hash.
	var m map[string]CachedCheck
	if err := json.Unmarshal(env.Data, &m); err != nil {
		return nil, fmt.Errorf("decoding checkcached: %w", err)
	}
	for h, cc := range m {
		if cc.Hash == "" {
			cc.Hash = h
		}
		list = append(list, cc)
	}
	return list, nil
}

// UserMe returns the account/plan/usage payload.
func (c *Client) UserMe(ctx context.Context) (*User, error) {
	env, err := c.do(ctx, http.MethodGet, "/user/me?settings=false", "", nil)
	if err != nil {
		return nil, err
	}
	var u User
	if err := json.Unmarshal(env.Data, &u); err != nil {
		return nil, fmt.Errorf("decoding user: %w", err)
	}
	return &u, nil
}
