# Boxarr — External Contracts (Spec 03)

**Date:** 2026-06-20
**Status:** Approved for planning

This spec **pins the request/response contracts and the Go client surfaces** for every external system Boxarr *calls outbound*: **TorBox**, **Prowlarr**, **TMDB**, **TVDB**, and **Plex**. The inbound Sonarr/Radarr v3 emulation that Seerr calls is **not** here — it is owned by `05-seerr-emulation.md`.

Everything below is grounded in primary-source contracts and the real `sab2torbox` TorBox client. Where a fact could not be confirmed from source it is encoded as a **chosen default + fallback** and cross-referenced to the runtime-verify register in `00-decisions-and-assumptions.md` §9. **Decode defensively everywhere:** unknown fields ignored, every numeric ID through `FlexInt`, optional fields tolerant of `null`/absent/empty.

Conventions inherited from `00` (locked): module `github.com/radaiko/boxarr`; new clients live under `internal/<service>` (`internal/torbox` extended, plus new `internal/prowlarr`, `internal/tmdb`, `internal/tvdb`, `internal/plex` per `01-architecture-and-packages.md`); wrapped errors `fmt.Errorf("doing X: %w", err)`; structured `slog` carrying `job_id`/`torbox_id`/`tmdb_id`; `context.Context` propagated to every call.

**One shared HTTP invariant across all five clients:** each client holds a `baseURL`, credential, and a `*http.Client` (30 s timeout, matching the existing TorBox client `client.go:35`). All non-TorBox clients copy the TorBox `do()` shape — read-with-`LimitReader`, parse `Retry-After` on every response, surface HTTP≥400 as a typed error carrying `Status`/`Detail`/`RetryAfter` — so the existing `RateLimit`/`Retryable`/`parseRetryAfter` helpers (TorBox `client.go:176-211`) work unchanged for retry/back-off against **all** services. TorBox additionally carries the `{success,error,detail,data}` envelope; the others use service-native error shapes wrapped into the same `*APIError` Go type.

---

## TorBox

**Base URL:** `https://api.torbox.app/v1/api` (no trailing slash; `v1` is part of the path, not a query param) (verified `05-ext-torbox-api.md`; existing `DefaultBaseURL` `client.go:18`).
**Auth:** single header `Authorization: Bearer <token>` on every request, set in `do()` (verified `client.go:142`). All endpoints below require it.
**Envelope (locked):** `{success bool, error json.RawMessage, detail string, data json.RawMessage}`. **An error is raised when `resp.StatusCode >= 400` OR `env.Success == false`** — a `success:false` HTTP 200 still errors (verified `client.go:163`). Existing `Envelope`, `APIError`, `FlexInt`, `parseRetryAfter`, `RateLimit`, `Retryable`, `truncate` are **reused verbatim** for the new methods — no infrastructure changes (locked, Assumption C).

### Endpoints

| Concern | Method | Path | Notes |
|---|---|---|---|
| Submit usenet | POST | `/usenet/createusenetdownload` | multipart `file`/`link`/`name`. **60/hour.** *(implemented `client.go:61`)* |
| List usenet | GET | `/usenet/mylist?bypass_cache=true` | *(implemented `client.go:100`)* |
| Control usenet | POST | `/usenet/controlusenetdownload` | JSON `{usenet_id,operation}`, ops `delete\|pause\|resume\|reannounce` *(implemented `client.go:116`)* |
| Token probe | GET | `/usenet/mylist?limit=1` | cheap validity ping, reused by `/healthz` *(implemented `client.go:127`)* |
| **Submit torrent** | POST | `/torrents/createtorrent` | multipart `magnet`/`file`/`name`/`seed`/`allow_zip`/`as_queued`. Precedence **hash > magnet > file**. **60/hr uncached, 300/min cached (shared).** *(NEW)* |
| **List torrents** | GET | `/torrents/mylist?bypass_cache=true` | same envelope/shape family as usenet, different fields *(NEW)* |
| **Control torrent** | POST | `/torrents/controltorrent` | JSON `{torrent_id,operation,all}`, ops `reannounce\|delete\|resume\|pause` *(NEW)* |
| **Cached check** | GET | `/torrents/checkcached?hash=…&format=list&list_files=false` | "instant vs will-download" *(NEW)* |
| **Account / usage** | GET | `/user/me?settings=false` | plan tier, cooldown, monthly usage *(NEW)* |

`/torrents/mylist` accepts `bypass_cache`, `id`, `offset` (default `0`), `limit` (default `1000`) (verified `05-ext-torbox-api.md`). Boxarr sends `?bypass_cache=true` for explicit/post-mutation reads and paginates with `offset`+`limit` only for large libraries; the 600 s server cache is acceptable for the 15-min reconcile sweep (`06-pipelines.md`, FR-NC-1). **Whether torrent endpoints honour `bypass_cache`/`limit` like usenet is unverified — see `00` §9 TorBox register.**

### FlexInt + completion + failure (locked)

- **Every torrent ID field uses `FlexInt`** — TorBox serializes IDs as number *or* quoted string, and tolerates `null`→0 / `""`→0 / `7.0`→7 (verified `types.go:18-56`). The `mylist` numeric key is **`id`** (not `torrent_id`); the create-response key is `torrent_id` (verified `05-ext-torbox-api.md`).
- **Completion is the AND of two booleans:** `dl.DownloadFinished && dl.DownloadPresent` — identical to usenet; **no `IsComplete()` method**, match the field-test convention at call sites (locked `types.go:80-81`). These can differ briefly when a download finishes but storage indexing lags — keep polling until both are true.
- **Failure detection reuses `Failed()`** (lowercase+trim `download_state`, then `HasPrefix "failed"` / `HasPrefix "error"` / `Contains "stalled"`) (verified `types.go:111-116`). TorBox emits `download_state` strings `downloading`, `uploading`, **`stalled (no seeds)`** (literal parentheses + space), `paused`, `completed`, `cached`, `metaDL`, `checkingResumeData` (verified `05-ext-torbox-api.md`). `Contains "stalled"` matches `stalled (no seeds)` correctly; treat `metaDL`/`checkingResumeData` as intermediate-waiting (keep polling), `cached`/`completed` as success-leaning, anything `Failed()` as terminal-fail. **Exact failure strings (e.g. `failed (...)` reason suffixes) are unverified — `Failed()` matches by prefix/keyword so reason suffixes do not break it (`00` §9).**
- **Rate limits reuse the existing helpers verbatim:** on every TorBox error, callers check `RateLimit(err)` first — if `(d, true)`, sleep `d` (default to a sane minimum if `d==0`); else check `Retryable(err)` (Status `0`/`429`/`≥500` and transport errors) and back off (verified `client.go:196-211`). The `Retry-After` header is parsed on **every** response, not just 429 (verified `client.go:155`). For `createtorrent`, maintain two conceptual buckets in the grab limiter (`06-pipelines.md`, FR-LIM-3): a 300/min shared ceiling and a 60/hr uncached ceiling; on `ACTIVE_LIMIT` do **not** retry (no free slots — surface it); on `COOLDOWN_LIMIT`/`MONTHLY_LIMIT` read `cooldown_until` from `/user/me` and surface the exact resume time.

### POST /torrents/createtorrent — request + response

Request is `multipart/form-data` mirroring `CreateUsenetDownload` exactly (verified `05-ext-torbox-api.md`; mirror `client.go:62-88`):

| Field | Type | Notes |
|---|---|---|
| `magnet` | string | magnet URI |
| `file` | bytes | raw `.torrent`; default multipart filename `upload.torrent` |
| `name` | string | optional custom display name |
| `seed` | string | `"1"`=auto (default), `"2"`=always, `"3"`=never |
| `allow_zip` | string | `"true"`/`"false"` — ZIP for >100-file torrents |
| `as_queued` | bool | queue instead of start; **silently ignored on free plan** |

```json
// success data
{
  "torrent_id": 12345,
  "hash": "abc123...",
  "auth_id": "user_auth_id",
  "active_limit": 5,
  "current_active_downloads": 2,
  "queued_id": null
}
```

`queued_id` is non-null when `as_queued=true` or the active-slot limit is hit. **The create success-data ID field name (`torrent_id` vs `id` vs `queued_id`) is verified only for usenet (`usenetdownload_id`); decode `torrent_id` defensively and fall back to `id`/`queued_id` if absent (`00` §9 TorBox register).**

### GET /torrents/mylist — response item (trimmed to fields Boxarr consumes)

```json
{
  "id": 12345, "hash": "abc123...", "name": "My.Torrent.Name",
  "magnet": "magnet:?xt=urn:btih:...", "size": 1073741824,
  "active": true, "download_state": "downloading",
  "seeds": 42, "peers": 10, "ratio": 0.5,
  "progress": 0.73, "download_speed": 5242880, "upload_speed": 1048576,
  "eta": 120, "torrent_file": true, "availability": 1.0,
  "download_present": false, "download_finished": false,
  "expires_at": "2026-07-01T00:00:00Z", "owner": "user_auth_id", "private": false,
  "files": [
    { "id": 1, "name": "path/to/file.mkv", "short_name": "file.mkv",
      "size": 1073741824, "mimetype": "video/x-matroska",
      "md5": "abc123...", "opensubtitles_hash": "def456..." }
  ]
}
```

Torrent items carry `magnet`/`seeds`/`peers`/`ratio`/`private`/`owner`; usenet items carry `original_url`/`download_id`/`cached`/`cached_at` instead. Both share `id`/`hash`/`name`/`size`/`active`/`download_state`/`progress`/`download_speed`/`upload_speed`/`eta`/`torrent_file`/`download_present`/`download_finished`/`expires_at`/`files` (verified `05-ext-torbox-api.md`). **`seeds`/`peers`/`ratio`/`upload_speed`/`owner`/`private`/`files[].opensubtitles_hash` are inferred by analogy + third-party SDKs (stremthru) and not in the official Go SDK model; tolerate their absence (`00` §9). v8.4.1 may add `tags`/`alternative_hashes` — leave optional slice fields so they store/display when present without breaking parsing.**

### POST /torrents/controltorrent — request

```json
{ "torrent_id": 12345, "operation": "delete", "all": false }
```

Ops `reannounce`/`delete`/`resume`/`pause`; `data` is `null` on success (verified `05-ext-torbox-api.md`). **The `all` boolean is confirmed only in the stremthru Go impl, not the official SDK — Boxarr defaults `all:false` and never sets `true` without an explicit, confirmed UI action (`00` §9; FR-DEL-1).**

### GET /torrents/checkcached — response

Boxarr calls with `format=list` (faster; skips hash-keyed map conversion). With `format=list`, `data` is an **array**; with `format=object` (default), `data` is a **map keyed by lowercase hash** (only cached hashes appear). Hashes are comma-separated in a single `?hash=` param per the API, but the existing client builds repeated `hash` params via `url.Values` (both observed in the wild) — **use comma-joined per the verified spec, and guard `env.Data` with the `len>0 && !="null"` pattern from `ListUsenet` (`client.go:106`); treat empty/absent as "none cached".**

```json
// format=list, list_files=false
{ "success": true, "data": [ { "name": "Torrent Name", "size": 1073741824, "hash": "abc123..." } ] }
```

**`checkcached` shape (`list` array vs `object` map; key case; `files[]` field set when `list_files=true`; empty `{}` vs `null`) is only partly confirmed (`00` §9). Boxarr requests `format=list&list_files=false` and decodes into a slice; on a map response it falls back to map-then-flatten.** `files[]` in checkcached, when requested, carry only `{name,size}` (simpler than mylist files).

### GET /user/me — response (trimmed)

```json
{
  "id": 42, "auth_id": "abc123", "email": "user@example.com",
  "plan": 2, "is_subscribed": true,
  "premium_expires_at": "2026-12-01T00:00:00Z",
  "cooldown_until": "2026-06-21T00:00:00Z",
  "total_downloaded": 10737418240,
  "created_at": "2025-01-01T00:00:00Z", "updated_at": "2026-06-20T00:00:00Z"
}
```

`plan` is an **integer** tier; the response does **not** carry active-slot counts — derive them in Boxarr from a static lookup `{0:1, 1:3, 2:10, 3:5}` (Free/Essential/Pro/Standard), `0` also = 10-download monthly limit (verified `05-ext-torbox-api.md`). `cooldown_until` is the ISO-8601 time the cooldown ends after `MONTHLY_LIMIT`/`COOLDOWN_LIMIT`; `total_downloaded` is bytes over a rolling 30 days (v8.4.5). **The plan-integer ordering, the slot-count mapping, the cooldown/usage field names, and the `settings` shape are inferred (third-party iota; official SDK uses float for `plan`) — verify against a live `/user/me` and re-derive slot counts if `ACTIVE_LIMIT` errors appear unexpectedly (`00` §9 TorBox register).**

### Known error codes (the `error` field)

Surface these to the notification center (`06-pipelines.md`, FR-NC-3): `ACTIVE_LIMIT` (no free slots — don't retry), `MONTHLY_LIMIT` / `COOLDOWN_LIMIT` (read `cooldown_until`), `DOWNLOAD_NOT_CACHED`, `DUPLICATE_ITEM`, `PLAN_RESTRICTED_FEATURE`, `BAD_TOKEN`/`AUTH_ERROR`/`NO_AUTH`, `DOWNLOAD_TOO_LARGE`, `BOZO_TORRENT`/`BOZO_NZB` (verified `05-ext-torbox-api.md`).

### Go client surface (extend `internal/torbox` in place)

New method signatures — all route through `do()`, returning `*APIError` so `RateLimit`/`Retryable` keep working (verified `00-sab-torbox-client.md` proposal):

```go
// POST /torrents/createtorrent (multipart). Exactly one of TorrentContent or Magnet set.
func (c *Client) CreateTorrent(ctx context.Context, req TorrentCreateRequest) (*TorrentCreateResult, error)

// GET /torrents/mylist?bypass_cache=true
func (c *Client) ListTorrents(ctx context.Context) ([]TorrentDownload, error)

// POST /torrents/controltorrent  {"torrent_id":id,"operation":op}  op in {reannounce,delete,resume,pause}
func (c *Client) ControlTorrent(ctx context.Context, id int64, op string) error

// GET /torrents/checkcached?hash=h1,h2&format=list&list_files=false
func (c *Client) CheckCached(ctx context.Context, hashes []string) ([]CachedCheck, error)

// GET /user/me?settings=false
func (c *Client) UserMe(ctx context.Context) (*User, error)
```

New structs (json tags; reuse `FlexInt` for IDs):

```go
// Multipart, not JSON-tagged — mirrors CreateRequest (client.go:41-45).
type TorrentCreateRequest struct {
    TorrentContent []byte // .torrent bytes -> multipart "file" (default filename "upload.torrent")
    TorrentName    string // -> "name"
    Magnet         string // -> "magnet"
    Seed           int    // 1=auto,2=always,3=never -> "seed" (omit if 0)
    AllowZip       bool   // -> "allow_zip"
    AsQueued       bool   // -> "as_queued"
}
type TorrentCreateResult struct {
    TorrentID FlexInt `json:"torrent_id"`
    QueuedID  FlexInt `json:"queued_id"`
    Hash      string  `json:"hash"`
    AuthID    string  `json:"auth_id"`
}
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
    Owner            string        `json:"owner"`
    Private          bool          `json:"private"`
    Files            []TorrentFile `json:"files"`
}
type TorrentFile struct {
    ID                FlexInt `json:"id"`
    Name              string  `json:"name"`
    ShortName         string  `json:"short_name"`
    Size              int64   `json:"size"`
    MimeType          string  `json:"mimetype"`
    OpenSubtitlesHash string  `json:"opensubtitles_hash"`
}
type CachedCheck struct {
    Name string `json:"name"`
    Size int64  `json:"size"`
    Hash string `json:"hash"`
}
type User struct {
    ID               FlexInt `json:"id"`
    AuthID           string  `json:"auth_id"`
    Email            string  `json:"email"`
    Plan             int     `json:"plan"`
    IsSubscribed     bool    `json:"is_subscribed"`
    PremiumExpiresAt string  `json:"premium_expires_at"`
    CooldownUntil    string  `json:"cooldown_until"`
    TotalDownloaded  int64   `json:"total_downloaded"`
}
```

Helper methods copy the usenet pure-functions onto `TorrentDownload` (identical fields): `ProgressPct()`, `Failed()`, `ETASeconds()`, `DownloadedBytes()` (mirror `types.go:99-136`). The poller treats both download types uniformly via a small interface `{ DownloadFinished/DownloadPresent/Failed/ProgressPct/ETASeconds }` (`06-pipelines.md`). Build `checkcached`/`mylist` query strings with `net/url`; `CreateTorrent` uses `multipart.NewWriter` + `mw.FormDataContentType()` as the content-type, exactly as `CreateUsenetDownload`.

### Quirks to bake in

1. `mylist` numeric ID key is **`id`**; create-response is **`torrent_id`** (+`queued_id`). Decode both, prefer `torrent_id` then `id` then `queued_id`.
2. `download_state` **`stalled (no seeds)`** has literal parentheses + a space — never treat it as a clean enum; `Failed()`'s `Contains "stalled"` is the right test.
3. `download_finished && download_present` is completion; they can lag each other — keep polling until both.
4. Create precedence **hash > magnet > file**; Boxarr sends magnet when available (no HTTP round-trip), else the `.torrent` bytes (FR-GP-2).
5. `as_queued` is silently ignored on free plan; max combined queue across all types is 1000 (v8.2).
6. `/user/me` returns **no** slot counts — derive from `plan` via the static map; recheck `plan` on unexpected `ACTIVE_LIMIT`.
7. `ACTIVE_LIMIT` ⇒ no retry; `COOLDOWN_LIMIT`/`MONTHLY_LIMIT` ⇒ read `cooldown_until`. Reuse `RateLimit`/`Retryable` for 429/5xx/transport.
8. `checkcached` returns only cached hashes (absent ⇒ uncached); request `format=list&list_files=false`; batch ≤100 hashes; server caches ~1 h.
9. `controltorrent.all=true` is dangerous (esp. `delete`) and SDK-unconfirmed — never set without explicit confirmed action.

**maps to:** FR-GP-2/3/6/7 (grab/poll torrents), FR-DEL-1 (delete propagates to TorBox), FR-HEAL-1 (resubmit stored magnet/torrent), FR-LIM-1/2/3/4 + FR-ST-1/2 (`/user/me` plan/cooldown/usage), FR-SR-2 (`checkcached` "instant" badge). Consumed by `02-data-model.md` (torrent job fields) and `06-pipelines.md` (grab/poll/delete/heal/limits).

---

## Prowlarr

**Base URL:** `http(s)://host:9696` (default `http://localhost:9696`) (verified `06-ext-prowlarr-api.md`).
**Auth:** `X-Api-Key: <key>` HTTP header (also accepts `?apikey=<key>`; **Boxarr always uses the header** to keep the key out of access logs). Configured via `BOXARR_PROWLARR_URL`/`BOXARR_PROWLARR_API_KEY` (`08-config-deploy-ci.md`).

### Endpoints

| Concern | Method | Path | Notes |
|---|---|---|---|
| Search | GET | `/api/v1/search?query=…&type=…&categories=…&indexerIds=…&limit=…&offset=…` | aggregated multi-indexer; returns `ReleaseResource[]`; usenet+torrent |
| Indexers | GET | `/api/v1/indexer` | configured indexers + capabilities, for UI filters/diagnostics |

### GET /api/v1/search — params

| Param | Type | Default | Notes |
|---|---|---|---|
| `query` | string | (required) | URL-encoded term |
| `type` | string | `search` | `search` \| `tvsearch` \| `movie` \| `music` \| `book` |
| `categories` | repeated int | (all) | **repeated keys** `&categories=2000&categories=5000` — comma-separated returns **HTTP 400** |
| `indexerIds` | repeated int | (all) | repeated keys; pass **`indexerIds=-1`** explicitly for "all" |
| `limit` | int | 100 | defaults to 100 if absent or `<1` |
| `offset` | int | 0 | pagination |

Boxarr builds queries: movies → `type=movie&categories=2000`; TV → `type=tvsearch&categories=5000` (parent IDs catch all subcategories); generic free-text → `type=search` (default). Add UHD-specific parents (`2045`/`5045`) as extra repeated `categories` keys when desired. **Always pass `indexerIds=-1` explicitly** (behaviour when omitted is undocumented). Category space: Movies `2000–2999`, TV `5000–5999` (parent `2000`/`5000` matches all children; a child like `2040` filters to that subcategory) (verified `06-ext-prowlarr-api.md`).

Example:
```
GET /api/v1/search?query=Oppenheimer&type=movie&categories=2000&indexerIds=-1
X-Api-Key: <key>
```

### GET /api/v1/search — `ReleaseResource` (trimmed to fields Boxarr consumes; all nullable, camelCase)

```json
[
  {
    "title": "Oppenheimer.2023.2160p.UHD.BluRay...",
    "indexer": "SomeIndexer", "indexerId": 3,
    "size": 1073741824, "files": 1, "grabs": 421,
    "protocol": "torrent",                       // "torrent" | "usenet" | "unknown"
    "downloadUrl": "http://prowlarr/.../download", // proxied; .torrent or .nzb file
    "magnetUrl": "magnet:?xt=urn:btih:...",        // direct magnet (torrent only; may be null)
    "infoUrl": "https://indexer/details/...",
    "infoHash": "abc123def456...",                 // torrent only
    "seeders": 120, "leechers": 8,                 // torrent only
    "publishDate": "2025-01-01T00:00:00Z",
    "indexerFlags": ["freeleech", "scene"],
    "categories": [ { "id": 2045, "name": "Movies/UHD", "subCategories": [] } ],
    "fileName": "Oppenheimer.2023.2160p.torrent",  // read-only, computed from protocol
    "imdbId": 0, "tmdbId": 0, "tvdbId": 0, "tvMazeId": 0,
    "guid": "Indexer-Guid"
  }
]
```

`protocol` is `torrent`/`usenet`/`unknown`. `fileName` is computed: `.torrent` for torrent, `.nzb` for usenet. `downloadUrl` is **always** a Prowlarr-internal proxy URL (never the raw indexer URL); cache TTL ~30 min. **Do not parse or rewrite `downloadUrl`.**

**Grab-URL choice (locked decision tree, mirror `06-ext-prowlarr-api.md`):**
```
if protocol == "torrent":
    if magnetUrl != "" -> use magnetUrl   (hand straight to TorBox CreateTorrent.Magnet; no HTTP fetch)
    else               -> fetch downloadUrl -> .torrent bytes (or redirect to a magnet) -> CreateTorrent.TorrentContent
else if protocol == "usenet":
    fetch downloadUrl -> .nzb bytes -> CreateUsenetDownload
```
**Always fall back to `downloadUrl`** — `magnetUrl` may be `null` even when a magnet exists (depends on the per-indexer `PreferMagnetUrl` setting, unconfirmed effect on JSON; `00` §9 Prowlarr register). Store the resolved magnet/`.torrent`/`.nzb` locally on the job at grab time so heals don't depend on a live indexer link (FR-GP-1).

**Field caveats (`00` §9 Prowlarr register):**
- **`leechers` vs `peers`:** internal model uses `Peers`; the JSON key is documented as `leechers`. Decode `leechers`, fall back to `peers`.
- **`downloadVolumeFactor`/`uploadVolumeFactor`:** **absent** from the JSON `ReleaseResource` per official SDKs (present only in Torznab XML / indexer JSON). Use the `indexerFlags` array instead — `"freeleech"` (≈ doesn't count against ratio), `"halfleech"`, `"neutralLeech"`, `"doubleUpload"`, `"scene"`. Surface `freeleech` as a UI badge (FR-SR-2). Decode `downloadvolumefactor`/`uploadvolumefactor` (lowercase) only as an optional best-effort fallback.
- **`type` for movie search** may be `movie` (Torznab) or `moviesearch` in some wikis — Boxarr sends `movie`; verify on the instance Swagger.

### GET /api/v1/indexer — `IndexerResource` (trimmed)

```json
[
  {
    "id": 1, "name": "SomeIndexer", "enable": true,
    "protocol": "torrent", "privacy": "public", "priority": 25,
    "capabilities": {
      "limitsMax": 100, "limitsDefault": 50,
      "categories": [ { "id": 2000, "name": "Movies",
        "subCategories": [ { "id": 2040, "name": "Movies/HD", "subCategories": [] } ] } ],
      "searchParams": ["q"],
      "tvSearchParams": ["q","tvdbId","season","ep"],
      "movieSearchParams": ["q","imdbId","tmdbId"]
    }
  }
]
```

Boxarr calls this once on startup / on demand to populate the search filter list (store `id`, `name`, `protocol`, `enable`, `capabilities.categories`) and to know which ID-based params each indexer supports (`07-frontend.md` filters; `04-internal-api.md` exposes a `/api/v1/indexers` passthrough). `categories` is recursive (`subCategories`).

### Go client surface (new `internal/prowlarr`)

```go
type Client struct { baseURL, apiKey string; http *http.Client } // 30s timeout

func New(baseURL, apiKey string) *Client

// GET /api/v1/search — categories/indexerIds via repeated keys; indexerIds defaults to []int{-1}.
func (c *Client) Search(ctx context.Context, p SearchParams) ([]ReleaseResource, error)

// GET /api/v1/indexer
func (c *Client) Indexers(ctx context.Context) ([]IndexerResource, error)

type SearchParams struct {
    Query      string
    Type       string  // "search" | "tvsearch" | "movie"
    Categories []int   // repeated keys
    IndexerIDs []int   // repeated keys; default {-1}
    Limit      int     // 0 -> omit (Prowlarr defaults 100)
    Offset     int
}
type ReleaseResource struct {
    Title        string     `json:"title"`
    Indexer      string     `json:"indexer"`
    IndexerID    int        `json:"indexerId"`
    Size         int64      `json:"size"`
    Files        int        `json:"files"`
    Grabs        int        `json:"grabs"`
    Protocol     string     `json:"protocol"`
    DownloadURL  string     `json:"downloadUrl"`
    MagnetURL    string     `json:"magnetUrl"`
    InfoURL      string     `json:"infoUrl"`
    InfoHash     string     `json:"infoHash"`
    Seeders      int        `json:"seeders"`
    Leechers     int        `json:"leechers"`
    PublishDate  string     `json:"publishDate"`
    IndexerFlags []string   `json:"indexerFlags"`
    Categories   []Category `json:"categories"`
    FileName     string     `json:"fileName"`
    GUID         string     `json:"guid"`
}
type Category struct {
    ID            int        `json:"id"`
    Name          string     `json:"name"`
    SubCategories []Category `json:"subCategories"`
}
```

Build repeated params with `url.Values.Add` in a loop. `do()` mirrors TorBox: set `X-Api-Key`, `LimitReader` the body, parse `Retry-After`, surface HTTP≥400 as `*APIError`. Prowlarr returns an **empty list (not an error)** when individual indexer searches fail (errors are logged server-side) — Boxarr treats `[]` as "no results", not a failure. De-duplicate results by `guid` defensively (some indexers ignore `limit`/`offset`). **No documented cache-bust for the ~30-min result cache (`00` §9).**

### Quirks to bake in

1. **Repeated** `&categories=`/`&indexerIds=` keys only — comma-separated ⇒ HTTP 400.
2. Always pass **`indexerIds=-1`** explicitly for "all indexers".
3. Grab choice: torrent → `magnetUrl` if non-empty else `downloadUrl`; usenet → `downloadUrl`. Always have the `downloadUrl` fallback.
4. `downloadUrl` is a Prowlarr proxy URL — never rewrite it; fetch it to get the artifact bytes.
5. `leechers` is the JSON key (internal `Peers`); decode `leechers`, fall back `peers`.
6. No `downloadVolumeFactor` in JSON — read `indexerFlags` for freeleech etc.
7. Parent category ID matches all subcategories; child ID filters to one.
8. Empty results list ≠ error; de-dup by `guid`; expect 30-min stale cache with no bust.
9. Send `X-Api-Key` as a header, not query param (keeps key out of logs).

**maps to:** FR-SR-1/2/3 (manual search → ranked release list → grab), FR-GP-1 (store artifact), FR-HEAL-2 (fresh Prowlarr re-search heal fallback, `00` §3 decision 19.4). Consumed by `04-internal-api.md` (search/grab endpoints), `06-pipelines.md` (selection scoring + grab + heal), `07-frontend.md` (search UI + indexer filters).

---

## TMDB

**Base URL:** `https://api.themoviedb.org/3/` (all paths relative) (verified `07-ext-tmdb-api.md`).
**Auth:** `Authorization: Bearer <API Read Access Token>` (preferred; covers v3+v4). Legacy `?api_key=` also works. Configured via `BOXARR_TMDB_API_KEY`.
**Images:** never hardcode — call `/configuration` at startup, cache `images.secure_base_url` (`https://image.tmdb.org/t/p/`) + size arrays; construct `secure_base_url + size + file_path` at render time; store only `file_path` in the DB. `w500` posters, `w1280` backdrops, `w300` stills (locked; `00` §9 TMDB: "always call `/configuration`").
**Rate limit:** ~40 req/s per **IP** (not per key — shared across workers); 429 carries `Retry-After` (read the actual header). Treat 40 req/s as the safe ceiling; parallel season fetches must respect it.

### Endpoints

| Concern | Method | Path | Notes |
|---|---|---|---|
| Image config | GET | `/configuration` | image base + size arrays; cache at startup |
| TVDB→TMDB | GET | `/find/{tvdbId}?external_source=tvdb_id` | series-level resolution via `tv_results[0].id` |
| TV details | GET | `/tv/{id}?append_to_response=external_ids` | header + season summaries + `tvdb_id`/`imdb_id` |
| TV season | GET | `/tv/{id}/season/{n}` | per-episode air dates/titles/stills |
| TV external IDs | GET | `/tv/{id}/external_ids` | `tvdb_id` (int), `imdb_id` (fallback to the append) |
| Movie details | GET | `/movie/{id}?append_to_response=release_dates` | runtime/status/`imdb_id`/dates |
| Movie external IDs | GET | `/movie/{id}/external_ids` | no `tvdb_id` for movies |
| Search TV | GET | `/search/tv?query=…&first_air_date_year=…` | resolve id from name+year |
| Search movie | GET | `/search/movie?query=…&primary_release_year=…` | resolve id from name+year |

`append_to_response` works on detail endpoints (movie/tv/season/episode) but **not** on search/list (verified `07-ext-tmdb-api.md`).

### Call sequence — build a series

1. **Resolve id** (if from search, not Seerr): `GET /search/tv?query={name}&first_air_date_year={year}` → `results[0].id`.
2. **Header + external IDs in one call:** `GET /tv/{id}?append_to_response=external_ids`. Store `id`, `name`, `status`, `in_production`, `first_air_date`, `last_air_date`, `number_of_seasons`, `number_of_episodes`, `poster_path`, `backdrop_path`, `overview`, `external_ids.tvdb_id`, `external_ids.imdb_id`. **`imdb_id` is NOT in the bare `/tv/{id}` body — the append (or `/tv/{id}/external_ids`) is required.**
3. **Season list** from the step-2 `seasons[]` summary (`season_number`, `name`, `episode_count`, `air_date`, `poster_path`). Filter `season_number == 0` (Specials) unless wanted.
4. **Episode details per season** (one `GET /tv/{id}/season/{n}` per season, parallelisable within the 40 req/s ceiling): store per episode `episode_number`, `season_number`, `name`, `air_date`, `overview`, `still_path`, `runtime`. **Air-date-aware:** episodes with a future/empty `air_date` are not "wanted" (FR-CAT-4).
5. **Refresh** (FR-CAT-5): repeat step 2; compare `number_of_episodes`/`last_air_date` to detect new content; only re-fetch changed seasons.

TV `status` is a **string** (`"Returning Series"`, `"Planned"`, `"Pilot"`, `"In Production"`, `"Ended"`, `"Canceled"`) — read the string; **do not** rely on integer codes (those are only for the Discover `with_status` filter, and 1–3 orderings differ between sources; `00` §9).

### Call sequence — build a movie

1. **Resolve id** (if manual add): `GET /search/movie?query={name}&primary_release_year={year}` → `results[0].id`.
2. **Details + release dates:** `GET /movie/{id}?append_to_response=release_dates`. From the body store `id`, `title`, `imdb_id` (**present directly for movies**, unlike TV), `status`, `release_date`, `runtime`, `overview`, `poster_path`, `backdrop_path`. From `release_dates.results` filter `iso_3166_1=="US"`: `type==3`→theatrical (`inCinemas`), `type==4`→digital, `type==5`→physical (release type enum: 1 Premiere, 2 Theatrical-limited, 3 Theatrical, 4 Digital, 5 Physical, 6 TV). Movie `status` strings: `"Rumored"`, `"Planned"`, `"In Production"`, `"Post Production"`, `"Released"`, `"Canceled"`.

These date/status values feed the Radarr-emulation `minimumAvailability` mapping owned by `05-seerr-emulation.md` (announced/inCinemas/released/preDB).

### Call sequence — resolve tvdb→tmdb (Seerr adds)

Overseerr's Sonarr-emulation add carries a `tvdbId` integer; Boxarr must convert before catalog work:
```
GET https://api.themoviedb.org/3/find/{tvdbId}?external_source=tvdb_id
Authorization: Bearer <token>
```
Use `tv_results[0].id`; **check `tv_results.length > 0` first** and log the raw `tvdbId` alongside the resolved TMDB id. `tvdbId` must be the **series-level** TVDB id (integer). **Whether `/find` resolves season-specific TVDB ids to the parent series is unconfirmed — fall back to TVDB `/search/remoteid` (see TVDB §) only if `/find` returns empty (`00` §9 TMDB + TVDB registers).**

### Key response shapes (trimmed)

```json
// GET /find/{tvdbId}?external_source=tvdb_id
{ "movie_results": [], "tv_results": [ { "id": 1399, "name": "Game of Thrones",
  "first_air_date": "2011-04-17", "poster_path": "/..." } ] }

// GET /tv/{id}/external_ids   (tvdb_id is an INTEGER, any field may be null)
{ "id": 1399, "imdb_id": "tt0944947", "tvdb_id": 121361 }

// GET /tv/{id}/season/{n} -> episodes[]
{ "season_number": 1, "episodes": [ { "episode_number": 1, "season_number": 1,
  "name": "Winter Is Coming", "air_date": "2011-04-17", "still_path": "/...",
  "runtime": 62 } ] }

// GET /movie/{id}  (imdb_id present directly)
{ "id": 11, "title": "Star Wars", "imdb_id": "tt0076759", "status": "Released",
  "release_date": "1977-05-25", "runtime": 121, "poster_path": "/..." }
```

429 body: `{"status_code":25,"status_message":"Too many requests...","retry_after":10}` — read the actual `Retry-After` header (value varies).

### Go client surface (new `internal/tmdb`)

```go
type Client struct { baseURL, token string; http *http.Client; cfg atomic.Pointer[Configuration] }

func New(token string) *Client

func (c *Client) Configuration(ctx context.Context) (*Configuration, error)            // call at startup; cache
func (c *Client) FindByTVDB(ctx context.Context, tvdbID int) (*FindResult, error)       // /find?external_source=tvdb_id
func (c *Client) TVDetails(ctx context.Context, id int) (*TVDetails, error)             // append_to_response=external_ids
func (c *Client) TVSeason(ctx context.Context, id, season int) (*SeasonDetails, error)
func (c *Client) TVExternalIDs(ctx context.Context, id int) (*ExternalIDs, error)
func (c *Client) MovieDetails(ctx context.Context, id int) (*MovieDetails, error)       // append_to_response=release_dates
func (c *Client) MovieExternalIDs(ctx context.Context, id int) (*ExternalIDs, error)
func (c *Client) SearchTV(ctx context.Context, query string, year int) ([]TVResult, error)
func (c *Client) SearchMovie(ctx context.Context, query string, year int) ([]MovieResult, error)

// ImageURL reconstructs a full image URL from a cached secure_base_url + size + file_path.
func (c *Client) ImageURL(size, filePath string) string
```

`do()` mirrors TorBox: `Authorization: Bearer`, `LimitReader`, parse `Retry-After`, HTTP≥400 → `*APIError` (so `RateLimit`/`Retryable` cover TMDB 429/5xx). A package-level limiter caps outbound at ~40 req/s to respect the IP budget. Struct json tags follow the field names above; `tvdb_id` is `int`, all paths stored as `file_path` strings.

### Quirks to bake in

1. **Always** `GET /configuration` at startup; never hardcode image base/sizes; store only `file_path`, reconstruct at render.
2. `imdb_id` is present on `/movie/{id}` but **NOT** on `/tv/{id}` — TV needs `external_ids` (use the append).
3. `status` is a **string** — read it; don't map integer codes.
4. `seasons[]` in `/tv/{id}` is a summary; per-episode data needs `/tv/{id}/season/{n}`.
5. Season 0 = Specials; filter unless wanted. Air-date-aware "wanted" set (skip future/empty `air_date`).
6. `/find` needs the **series-level** integer `tvdbId`; check `tv_results.length>0`.
7. Rate limit is per **IP** (~40 req/s, shared across workers) — throttle parallel season fetches; read the real `Retry-After`.
8. `append_to_response` works on detail endpoints only, never on search/list.

**maps to:** FR-CAT-1/2/4/5 (build/refresh catalog, wanted-but-missing), FR-UI-1/2/3 (posters, season/episode views), FR-IMP-2 (Plex-friendly folder/file names from titles+years), and the tvdb→tmdb bridge for FR-SEERR-4. Consumed by `02-data-model.md` (series/season/episode/movie metadata cache columns), `05-seerr-emulation.md` (lookups + add ingest), `06-pipelines.md` (namer + season-pack→episode mapping).

---

## TVDB (v4)

**Base URL:** `https://api4.thetvdb.com/v4` (verified `08-ext-tvdb-api.md`; OAS3 `https://thetvdb.github.io/v4-api/`).
**Auth:** `POST /login` with `{apikey, pin?}` → `{data:{token}}`; carry `Authorization: Bearer <token>` thereafter. **Token lifetime ≈ 30 days; there is NO refresh endpoint — re-`POST /login`.** Configured via `BOXARR_TVDB_API_KEY` (+ optional PIN). **A project/licensed key needs no PIN; a user-supported key needs the subscriber PIN.**

**Token lifecycle (locked):** parse the JWT `exp` claim to get the exact expiry; cache the token; **pre-emptively re-login ~2 days before `exp`** (no refresh endpoint exists). **The exact lifetime in seconds is undocumented — rely on `exp`, not a hardcoded 30 days (`00` §9 TVDB register).**

**When Boxarr consults TVDB vs TMDB (locked, `00` §3 decision 19.3):** TMDB is **primary** for discovery, posters, overviews, movie+TV catalog. TVDB is consulted **only** for (1) obtaining the `tvdbId` the Sonarr-emulation surface keys on — but normally that comes from TMDB `/tv/{id}/external_ids.tvdb_id`, so TVDB is the **fallback** when that is null; (2) **episode ordering that differs from TMDB's** — anime absolute ordering and DVD/alternate ordering. Standard western TV never needs a TVDB call.

### Endpoints

| Concern | Method | Path | Notes |
|---|---|---|---|
| Login | POST | `/login` | `{apikey,pin?}` → bearer; parse JWT `exp` |
| Series extended | GET | `/series/{id}/extended` | `remoteIds[]`, `seasons[]`, `seasonTypes[]`, `defaultSeasonType` |
| Episodes by ordering | GET | `/series/{id}/episodes/{season-type}` | `season-type` ∈ `default\|official\|dvd\|absolute\|alternate\|regional`; paginated (≤500) |
| Season types | GET | `/seasons/types` | the numeric-id ↔ type-string table; cache at startup |
| Remote-id lookup | GET | `/search/remoteid/{remoteId}` | reverse lookup (TMDB/IMDB) — TVDB-only fallback path |

### Aired vs DVD vs absolute ordering

`season-type` is a literal path segment selecting the ordering (verified `08-ext-tvdb-api.md`):

| `season-type` | Meaning | When Boxarr uses it |
|---|---|---|
| `default` | alias for the series' `defaultSeasonType` (usually `official`) | safe universal fallback |
| `official` | broadcast/aired order, grouped into calendar seasons | **standard western TV** (default; matches TMDB) |
| `dvd` | DVD/Blu-ray release order (may differ from air order, e.g. Firefly) | only when catalog/user explicitly flags DVD ordering |
| `absolute` | all episodes in one virtual season, numbered 1…N continuously | **anime** (scene groups + Sonarr use `absoluteNumber`) |
| `alternate` | community/platform recut ordering | platform-specific cuts (edge case) |
| `regional` | region-specific broadcast order | edge case until tested |

`EpisodeBaseRecord` carries both the per-ordering coordinates (`seasonNumber`, `number`) **and** a series-wide `absoluteNumber` (**nullable** — only populated when TVDB editors entered it; null for most non-anime). **Whether `absoluteNumber` is populated in non-`absolute` orderings is unconfirmed — Boxarr fetches `season-type=absolute` explicitly for anime rather than relying on `absoluteNumber` appearing in the `official` list, and stores both `absoluteNumber` and `(seasonNumber, number)` per episode so either mapping strategy works without a second call (`00` §9 TVDB).**

`SeasonType` numeric ids: `1`=official (Aired Order), `2`=dvd, `3`=absolute, `4`=alternate; `regional`=unknown. **These integers are inferred from community code — call `GET /seasons/types` at startup and cache the full list rather than hardcoding (`00` §9 TVDB). Match the TMDB cross-id by `sourceName` string (`"TheMovieDB.com"`), not the `remoteIds[].type` integer (reported as `12` but safer by name).**

### Request/response shapes (trimmed)

```json
// POST /login  (request / response)
{ "apikey": "string", "pin": "string" }
{ "status": "success", "data": { "token": "<JWT>" } }

// GET /series/{id}/extended  -> data.remoteIds[] + seasons[] + defaultSeasonType
{ "status": "success", "data": {
  "id": 1234567, "name": "Breaking Bad", "defaultSeasonType": 1,
  "remoteIds": [
    { "id": "tt0903747", "type": 2,  "sourceName": "IMDB" },
    { "id": "1396",      "type": 12, "sourceName": "TheMovieDB.com" }
  ],
  "seasons": [ { "id": 0, "number": 1, "type": { "id": 1, "type": "official" } } ]
} }

// GET /series/{id}/episodes/{season-type}  -> data.episodes[] + links (pagination)
{ "status": "success", "data": { "episodes": [
  { "id": 123456, "seriesId": 1234567, "name": "Pilot",
    "number": 1, "seasonNumber": 1, "absoluteNumber": 1,
    "aired": "2008-01-20", "runtime": 58 } ] },
  "links": { "next": "https://api4.thetvdb.com/v4/series/.../episodes/default?page=1",
             "total_items": 62, "page_size": 500 } }

// GET /seasons/types  -> data[]
{ "status": "success", "data": [ { "id": 1, "name": "Aired Order", "type": "official", "alternateName": null } ] }
```

Episodes paginate at ≤500/page — follow `links.next` until null. **`/series/{id}/extended?meta=episodes` may inline episodes only for the default ordering — for non-default orderings, call `/episodes/{season-type}` explicitly (`00` §9 TVDB).** Prefer TMDB `/find` for tvdb→tmdb; use TVDB `/search/remoteid/{tmdbId}` only as a fallback, and its reliability for TMDB ids is uncertain (`00` §9).

### Go client surface (new `internal/tvdb`)

```go
type Client struct {
    baseURL, apiKey, pin string
    http *http.Client
    mu   sync.Mutex
    token string
    exp   time.Time   // parsed from JWT exp; re-login ~2 days early
}

func New(baseURL, apiKey, pin string) *Client

func (c *Client) ensureToken(ctx context.Context) error                       // POST /login if token nil/near-exp; parse exp
func (c *Client) SeriesExtended(ctx context.Context, id int) (*SeriesExtended, error)
func (c *Client) Episodes(ctx context.Context, id int, seasonType string) ([]Episode, error) // follows links.next
func (c *Client) SeasonTypes(ctx context.Context) ([]SeasonType, error)       // cache at startup
func (c *Client) SearchRemoteID(ctx context.Context, remoteID string) ([]RemoteIDMatch, error)

type RemoteID struct { ID string `json:"id"`; Type int `json:"type"`; SourceName string `json:"sourceName"` }
type Episode struct {
    ID             int    `json:"id"`
    SeriesID       int    `json:"seriesId"`
    Name           string `json:"name"`
    Number         int    `json:"number"`
    SeasonNumber   int    `json:"seasonNumber"`
    AbsoluteNumber *int   `json:"absoluteNumber"` // nullable
    Aired          string `json:"aired"`
    Runtime        int    `json:"runtime"`
}
type SeasonType struct { ID int `json:"id"`; Name, Type, AlternateName string }
```

`ensureToken` guards every call (under `mu`): if `token=="" || time.Now().After(exp.Add(-48*time.Hour))`, POST `/login`, decode the JWT payload's `exp` (base64 the middle segment, parse `exp` unix seconds), store both. `do()` mirrors TorBox (`Authorization: Bearer <token>`, `LimitReader`, parse `Retry-After`, HTTP≥400 → `*APIError`); a 401 forces one re-login + retry. **v4 rate limits are undocumented — rely on `Retryable` back-off (`00` §9).**

### Quirks to bake in

1. No refresh endpoint — parse JWT `exp`, pre-emptively re-`POST /login` ~2 days early; serialize re-auth under a mutex.
2. PIN required only for user-supported keys; project keys omit it.
3. `season-type` is a path segment; default → `official`. Use `official` for standard TV, `absolute` for anime.
4. `absoluteNumber` is **nullable** — store it and `(seasonNumber, number)`; fetch `absolute` ordering explicitly for anime.
5. Cross-id match by `sourceName == "TheMovieDB.com"`, not the integer `type`; call `/seasons/types` at startup, don't hardcode ids.
6. Episodes paginate ≤500/page — follow `links.next`.
7. Prefer TMDB `/find` for tvdb→tmdb; TVDB `/search/remoteid` is fallback-only and TMDB-id reliability is uncertain.
8. Send `seriesType: "anime"` to the Sonarr emulation when ordering is absolute (so XEM/absolute matching applies) — owned by `05-seerr-emulation.md`.

**maps to:** FR-CAT-1 (resolve `tvdb_id` when TMDB external_ids lacks it), FR-IMP-4 (season-pack→episode mapping for anime via absolute ordering), and supplying the `tvdbId` the Sonarr-emulation surface keys on. Consumed by `02-data-model.md` (`series.tvdb_id` + per-episode `absolute_number`), `05-seerr-emulation.md` (Sonarr lookups), `06-pipelines.md` (episode mapping for anime/DVD).

---

## Plex

**Base URL:** `http(s)://plex-host:32400` (verified `09-ext-plex-api.md`).
**Auth:** `X-Plex-Token` — HTTP header **or** `?X-Plex-Token=` query param (fully equivalent; Boxarr uses the header). Configured via `BOXARR_PLEX_URL`/`BOXARR_PLEX_TOKEN`. **Optional integration** — `Enabled()` predicate gates it (mirror `WebDAVRefreshEnabled()`).

### Endpoints

| Concern | Method | Path | Notes |
|---|---|---|---|
| List sections | GET | `/library/sections` | discover movie/show section ids + Location paths |
| Partial scan | GET | `/library/sections/{id}/refresh?path={enc}` | scan one folder; async; 200 empty body |
| Full section scan | GET | `/library/sections/{id}/refresh` | fallback when partial silently fails |
| Version probe | GET | `/identity` | check PMS version ≥ partial-scan minimum |

Boxarr requests `Accept: application/json` so `/library/sections` returns JSON (XML is the default).

### GET /library/sections — response (JSON)

```json
{ "MediaContainer": { "size": 2, "Directory": [
  { "key": "1", "type": "movie", "title": "Movies",
    "Location": [ { "id": 1, "path": "/data/media/movies" } ] },
  { "key": "2", "type": "show", "title": "TV Shows",
    "Location": [ { "id": 2, "path": "/data/media/tv" } ] }
] } }
```

On startup Boxarr walks `Directory[]`: `type=="movie"` → store `key` as the movie section id; `type=="show"` → store as the TV section id (if several, surface in the config UI for the operator to choose; `04-internal-api.md`/`07-frontend.md`). Log each `Location[].path` so the operator can confirm the path namespace matches what Boxarr will send. A 401 at startup is surfaced clearly, not swallowed.

### GET /library/sections/{id}/refresh — partial scan

```
GET http://plex-host:32400/library/sections/2/refresh?path=%2Fdata%2Fmedia%2Ftv%2FBreaking+Bad+%282008%29%2FSeason+01
X-Plex-Token: <token>
```

**Path encoding (chosen default + fallback):** **default `quote_plus` semantics** (spaces→`+`, `/`→`%2F`, `(`→`%28`, `)`→`%29`) — what python-plexapi and every community tool use. **If a live scan with a space-containing path fails, fall back to standard percent-encoding (spaces→`%20`) (`00` §9 Plex register).** Path must be the path **as Plex sees it** (container-internal in Docker; per `00` Assumption D the WebDAV/library mount is at the **same absolute path** in every container, so Boxarr's library path equals Plex's — no path-mapping table needed in the same-path deployment; a configurable `host→plex` prefix map is the escape hatch if they ever diverge). Path must be **at or below** a section `Location` root.

- For movies: scan the movie's own folder (`<MOVIE_LIBRARY_ROOT>/<Title> (<Year>)`).
- For TV: scan the **season-level** folder (faster, sufficient).
- Response: **HTTP 200, empty body, async** — caller does not wait (verified `09-ext-plex-api.md`). **Success code is assumed 200; treat any 2xx as success (`00` §9).** 401 ⇒ bad/missing token.
- **Fallback:** if the partial scan returns 200 but the item doesn't appear within a configurable timeout (default 60 s), fire the **full-section scan** (`/refresh` without `path`) and log a warning — handles the known large-library partial-scan reliability gap.

**Min-version caveat:** the `?path=` partial scan is honoured only on **PMS ≥ ~1.20.0.3125** (community-sourced; `00` §9 Plex register); older servers ignore `path` and do a full scan. Boxarr checks `/identity` at startup and warns (advising the operator to disable partial scan) if below that. **The newer `POST /library/sections/{id}/refresh` (PMS ≥ 1.43.2) is NOT used — its partial-scan `path` support is unconfirmed; Boxarr defaults to the legacy `GET ...?path=` for maximum compatibility (`00` §9).**

### Go client surface (new `internal/plex`)

```go
type Client struct { baseURL, token string; http *http.Client } // optional integration

func New(baseURL, token string) *Client

// GET /library/sections (Accept: application/json)
func (c *Client) Sections(ctx context.Context) ([]Section, error)

// GET /library/sections/{id}/refresh?path={quote_plus(path)}  — partial scan
func (c *Client) ScanPath(ctx context.Context, sectionID, path string) error

// GET /library/sections/{id}/refresh  — full-section fallback
func (c *Client) ScanSection(ctx context.Context, sectionID string) error

type Section struct {
    Key       string     `json:"key"`   // numeric string
    Type      string     `json:"type"`  // "movie" | "show"
    Title     string     `json:"title"`
    Locations []Location `json:"Location"`
}
type Location struct { ID int `json:"id"`; Path string `json:"path"` }
```

`ScanPath` encodes the path with `quote_plus`-equivalent semantics (Go: `url.QueryEscape` produces spaces→`+` — matches `quote_plus`; note Go's `url.PathEscape` does **not**, so use `QueryEscape` then place it in the query string). `do()` mirrors TorBox: set `X-Plex-Token` header + `Accept: application/json`, `LimitReader`, parse `Retry-After`, surface non-2xx as `*APIError`. No throttling is documented for rapid scans; back off via `Retryable` if a 429/5xx ever appears.

### Quirks to bake in

1. `X-Plex-Token` as header or query — Boxarr uses the header.
2. `/library/sections` Directory `type` is `movie`/`show`; `Location[].path` is what Plex actually watches.
3. Partial scan path uses `quote_plus` semantics (spaces→`+`); fallback to `%20` percent-encoding if a space-path scan fails.
4. Path must be the **Plex-internal** path and **at/below** a section Location; same-path mount (Assumption D) makes Boxarr's path == Plex's.
5. Scan returns 200 + empty body **async** — never wait on it; treat any 2xx as success.
6. Partial scan needs PMS ≥ ~1.20.0.3125 — check `/identity`, warn if older.
7. Stick to legacy `GET ...?refresh?path=`; don't use the POST v1.2.2 form (partial-path support unconfirmed).
8. On partial-scan miss within timeout, fall back to full-section scan.

**maps to:** FR-IMP-5 (post-import partial scan), FR-CFG-1 (Plex connection + section auto-detect with "Test connection"). Consumed by `06-pipelines.md` (import hook fires the scan), `04-internal-api.md` (settings/test endpoints), `07-frontend.md` (Plex settings UI).

---

## Definition of done

Boxarr ships five outbound clients whose request/response contracts match this spec verbatim: the **TorBox** client is extended in place with `CreateTorrent`/`ListTorrents`/`ControlTorrent`/`CheckCached`/`UserMe` reusing `do()`/`Envelope`/`FlexInt`/`APIError`/`parseRetryAfter`/`RateLimit`/`Retryable` unchanged, with `FlexInt` IDs, `download_finished && download_present` completion, `Failed()` failure-string matching (incl. `stalled (no seeds)`), and 429/`Retry-After`-driven back-off; new **Prowlarr**, **TMDB**, **TVDB**, and **Plex** clients each copy the `do()` shape so the shared rate-limit helpers apply, send the correct auth (`X-Api-Key` / `Bearer` / `Bearer`-from-JWT-login / `X-Plex-Token`), build the documented query shapes (repeated `categories`/`indexerIds` keys with `indexerIds=-1`, `magnetUrl`-then-`downloadUrl` grab choice, TMDB `append_to_response` sequences, TVDB ordering paths with JWT-`exp` pre-emptive re-login, Plex `quote_plus` partial-scan with full-section fallback), and decode defensively (unknown fields ignored, optional/nullable tolerated). Every field flagged uncertain here resolves to its chosen default with the cited fallback and a one-to-one entry in the `00-decisions-and-assumptions.md` §9 runtime-verify register, so implementation can proceed against these pinned contracts and confirm only the registered items against live instances.
