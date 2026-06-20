# Boxarr — Pipelines: Workers, Selection, Import, Parsing, Lifecycle (Spec 06)

**Date:** 2026-06-20
**Status:** Approved for planning

This spec owns the **worker state machines and algorithms**: the grab pipeline (usenet + the parallel torrent twin), the selection score (FR-SR-5), import + library naming + season-pack expansion, release-name parsing (§11.4), delete/heal/reaper lifecycle (§12), the 15-minute reconciler (§15), and TorBox-limit gating (§13). It is the "how it runs" layer; `02-data-model.md` owns the rows it mutates (job `state`, media `status`), `03-external-contracts.md` owns the wire calls it makes (TorBox / Prowlarr / Plex / TMDB / TVDB), and `00-decisions-and-assumptions.md` fixes every cross-cutting decision. This document does **not** re-decide anything `00` decides — it instantiates the worker behavior.

**Foundational invariant (locked, Assumption C):** Boxarr **reuses the sab2torbox worker package verbatim where it works** — `worker.go`'s loop/ticker harness, `submitter.go`, `poller.go` (incl. `resolveStoragePath`'s 30 s stat-poll), `webdav.go`'s `maybeRefreshWebDAV`, `symlink.go` (`buildSymlinkFarm`/`removeSymlinkDir`/`isWithin`/`classifyReleaseDir`/`atomicReplaceSymlink`/`findBestMatch`/`lcp`), `healer.go`, `deleter.go`, `reaper.go`, and the `clock.go` `timeNow` seam (verified `02-sab-worker-pipeline.md`, `03-sab-worker-lifecycle.md`). The torrent path is added as **parallel loops, never branches inside the usenet loops**, so a usenet 429 can never pause torrents and vice-versa.

---

## 1. Worker topology (what runs, how often)

`Workers.Run` launches one goroutine per loop; each calls its `*Once` function immediately, then on a `time.NewTicker(interval)` (verified `worker.go:78-127`). Shutdown is a `select` on `ctx.Done()` vs. the ticker; errors are logged only when `ctx.Err()==nil` (locked). Boxarr keeps every existing loop and **adds disjoint torrent twins + the reconciler**:

| Loop | Interval (default) | Reuse / New | Touches rows (filter) |
|---|---|---|---|
| `submitter` (usenet) | `PollInterval` 1m | reused verbatim | `state=pending AND protocol='usenet'` |
| `poller` (usenet) | `PollInterval` 1m | reused verbatim | `state IN (queued,downloading) AND protocol='usenet'` |
| `torrentSubmitter` | `PollInterval` 1m | **new (mirror)** | `state=pending AND protocol='torrent'` |
| `torrentPoller` | `PollInterval` 1m | **new (mirror)** | `state IN (queued,downloading,seeding) AND protocol='torrent'` |
| `deleter` | `PollInterval` 1m | extended (protocol branch) | `state=deleted` (both protocols) |
| `reaper` | 5m | reused verbatim | `state=completed`/`imported` empty-dir detection |
| `healer` + `healReconciler` | `HealInterval` 1h / `PollInterval` | extended (protocol + Prowlarr fallback) | `state IN (imported,healing,heal_failed)` |
| `reconciler` | `BOXARR_RECONCILE_INTERVAL` 15m | **new** | WebDAV mount + both mylists vs. all known jobs |
| `metadataRefresh` | `BOXARR_METADATA_REFRESH_INTERVAL` 24h | **new** (catalog) | `series`/`movie` (FR-CAT-5; sequence in `03` TMDB §) |

**Per-loop state needs no locks** because each loop is a single goroutine (verified `02-sab-worker-pipeline.md`). The torrent loops get their **own** debounce/back-off maps so they are fully independent of usenet:

```go
// internal/worker/worker.go — Workers gains torrent-twin fields (mirror existing maps)
torrentMissingPolls      map[int64]int  // torrentPoller only (mirrors missingPolls)
torrentSubmitBackoffUntil time.Time     // torrentSubmitter only (mirrors submitBackoffUntil)
torrentCreateWindow      *rateWindow    // 300/min + 60/hr buckets (§7)
usenetCreateWindow       *rateWindow    // 60/hr bucket (§7)
deleteAttempts           map[int64]int  // shared (deleter is one loop; keyed by job id) — unchanged
```

**Extended `TorBoxAPI` interface** (mirror `worker.go:18-23`, add the three torrent methods + `/user/me`):

```go
type TorBoxAPI interface {
    // existing
    CreateUsenetDownload(ctx, torbox.CreateRequest) (*torbox.CreateResult, error)
    ListUsenet(ctx) ([]torbox.UsenetDownload, error)
    ControlUsenet(ctx, id int64, op string) error
    Ping(ctx) error
    // NEW (03 TorBox §)
    CreateTorrent(ctx, torbox.TorrentCreateRequest) (*torbox.TorrentCreateResult, error)
    ListTorrents(ctx) ([]torbox.TorrentDownload, error)
    ControlTorrent(ctx, id int64, op string) error
    CheckCached(ctx, hashes []string) ([]torbox.CachedCheck, error)
    UserMe(ctx) (*torbox.User, error)
}
```

**The poller treats both download kinds uniformly** via a tiny interface (locked `03` TorBox §) so `reconcile` is written once:

```go
// internal/worker/download.go — unify usenet+torrent records for the poller/healer
type Download interface {
    DownloadFinished() bool
    DownloadPresent() bool
    Failed() bool
    ProgressPct() int
    ETASeconds() int64
    DownloadedBytes() int64
    StateString() string  // raw download_state, for fail messages + StateSeeding mapping
    SizeBytes() int64
}
```

`torbox.UsenetDownload` and `torbox.TorrentDownload` both already expose these as pure methods (verified `types.go:99-136`, mirrored in `03`). **Clock seam:** every new `time.Now()` read in the torrent twins, reconciler, and limiter routes through `worker.timeNow` (clock.go) so tests can drive time (verified `02-sab-worker-pipeline.md` — the existing pollers call `time.Now` directly, so the torrent port must use `timeNow`).

---

## 2. Grab pipeline (usenet flow + parallel torrent twin)

The grab pipeline turns a chosen Prowlarr release (or a heal re-grab) into an imported library symlink. **Steps map one-to-one onto `jobs.state` transitions** (`02` §2.1, twelve-state Go enum). The usenet column is the proven sab2torbox flow; the torrent column is the new twin. They share dedup, the WebDAV resolve, and the importer.

### 2.1 Step → state map

| # | Step | Usenet | Torrent | `jobs.state` after |
|---|---|---|---|---|
| 1 | **Grab** (HTTP handler / search-on-add) creates the job row | row written with `nzb_url`, `media_*` | row written with `torrent_magnet` or pending fetch | `pending` |
| 2 | **Fetch + store artifact locally** | fetch `downloadUrl` → `.nzb` bytes → `nzb_content`, compute `nzb_sha256` | magnet present → store `torrent_magnet` + `torrent_hash` (no fetch); else fetch `downloadUrl` → `.torrent` bytes → `torrent_file`, derive `torrent_hash` | `pending` (artifact persisted before submit, FR-GP-1) |
| 3 | **Dedup** (app-level, before insert/submit) | `FindBySHA256(sha,cat)` then `FindByURL(nzb_url,cat)` | `FindByTorrentHash(hash,cat)` then `FindByURL(magnet,cat)` | existing job returned → no new row (FR-GP-5) |
| 4 | **Submit** to TorBox | `CreateUsenetDownload` (multipart `file`/`link`/`name`) | `CreateTorrent` (multipart `magnet` or `file`+`name`; precedence hash>magnet>file) | `submitting` → `queued` (sets `torbox_id`,`torbox_hash`,`submitted_at`) |
| 5 | **Poll** the matching mylist until `download_finished && download_present` | `ListUsenet` each tick | `ListTorrents` each tick | `queued`→`downloading`(→`seeding`)→`completed` |
| 6 | **Resolve WebDAV release folder** by `name` (≤30 s stat-poll + optional `/refresh`) | `resolveStoragePath(ctx, rec.Name)` against `UsenetPath()` | same, against `TorrentPath()` | (within `completed`) |
| 7 | **Import** — write library symlink(s) directly at final Plex path; optional Plex partial scan | shared importer (§4) | shared importer (§4) | `completed` → `imported` |

**Step-1 artifact source (grab handler, lives outside the worker package — `04-internal-api.md`):** the grab endpoint resolves the artifact per the locked Prowlarr decision tree (`03` Prowlarr §): torrent with non-empty `magnetUrl` → store magnet (no HTTP fetch); else fetch `downloadUrl` for the `.torrent`/`.nzb` bytes. The artifact is **always persisted on the job row at grab time** so heals never depend on a live indexer link (FR-GP-1, FR-HEAL-1). The handler also sets `protocol`, `media_type`, `media_ref`, and flips the linked media `status` to `searching` then `downloading` (`02` §2.2 mapping).

### 2.2 Submitter (reuse + mirror)

Usenet `submitOnce`/`submitJob` are reused **byte-for-byte** (verified `submitter.go:18-117`): set `StateSubmitting`; build `torbox.CreateRequest{NZBContent, NZBName+".nzb", Link: NZBURL}` (bytes already on the row, no fetch); `backoff.Retry` with `newRetryPolicy` (InitialInterval 2 s, MaxInterval 30 s, MaxElapsedTime 2 m) where `torbox.RateLimit(err)` → `backoff.Permanent`, `torbox.Retryable(err)` retries, else permanent. On 429 the job **reverts to `pending`** (not `failed`) and the whole submitter pauses via `submitBackoffUntil = now + retryAfter` (or 5 m, capped 1 h); `submitOnce` stops draining on the first 429. On success: `StateQueued`, `TorBoxID=result.UsenetDownloadID`, `TorBoxHash=result.Hash`, `SubmittedAt=now`.

`submitTorrentsOnce`/`submitTorrentJob` mirror this exactly (decision: parallel loops, `02-sab-worker-pipeline.md` rec 4), differing only in:
1. `store.JobsByState(StatePending)` filtered to `protocol='torrent'`.
2. The TorBox call: `CreateTorrent(torbox.TorrentCreateRequest{Magnet: j.TorrentMagnet, TorrentContent: j.TorrentFile, TorrentName: j.NZBName})` — **send magnet when present** (no round-trip), else the `.torrent` bytes (quirk: precedence hash>magnet>file, `03`).
3. `TorBoxID=result.TorrentID` (fall back `result.QueuedID` then `result.id` — defensive decode, `00` §9 TorBox).
4. Its own `torrentSubmitBackoffUntil` and `torrentCreateWindow` (§7) — a usenet 429 never touches it.
5. **`as_queued`**: Boxarr sets `AsQueued=true` only when the limiter (§7) sees the active-slot allowance is full but the hourly/minute caps are not (queue rather than fail); a non-null `QueuedID` in the result transitions to `queued` exactly like a started download. `as_queued` is silently ignored on Free plan (quirk, `03`).

**Both create calls are 60/hour** (FR-GP-6); torrents add a 300/min shared ceiling and the 60/hr uncached cap. The grab limiter (§7) gates *before* the create call; on `ACTIVE_LIMIT` the submitter does **not** retry — it requeues the job to `pending` and surfaces a `limit_reached` notification (FR-NC-3).

### 2.3 Poller (reuse + mirror)

Usenet `pollOnce`/`reconcile`/`handleMissing` are reused verbatim (verified `poller.go:34-218`). Key locked behaviors:
1. A **failed `ListUsenet` returns early and is NOT a miss** — only an active job absent from a *successful* list increments `missingPolls`; at the `missingPollThreshold=6` (~6 m) the job goes `StateFailed` "download no longer present on TorBox".
2. Every tick updates `TotalBytes`, `DownloadedBytes`, `ProgressPct`, `ETASeconds` — even while awaiting WebDAV.
3. `rec.Failed()` → `StateFailed` with "TorBox reported state: " + `rec.DownloadState`.
4. On `download_finished && download_present`: `resolveStoragePath(ctx, rec.Name)` (≤30 s stat-poll at 1 s interval, ctx-cancellable) → importer (§4). **`files==0` means the folder was listed before its contents** → remove and stay awaiting (locked quirk). Otherwise `StateCompleted`, `StoragePath=<resolved>`, `ProgressPct=100`, `CompletedAt=now`.
5. `maybeRefreshWebDAV` fires **only when awaiting and not ongoing**, debounced by `WebDAVRefreshCooldown` (2 m) + a 15 m back-off on a 429 (verified `webdav.go:22-59`); opt-in via `BOXARR_TORBOX_WEBDAV_USER`/`_PASS`.

`pollTorrentsOnce`/`reconcileTorrent` mirror this with three differences:
- `tb.ListTorrents` keyed by `TorBoxID`; its own `torrentMissingPolls`.
- `resolveStoragePath` stats **`TorrentPath()`** = `filepath.Join(WebDAVMountRoot, WebDAVTorrentSubpath)` joined with `rec.Name` (`BOXARR_WEBDAV_TORRENT_SUBPATH` default empty — torrents surface under the same flat root as usenet unless the live mount differs; `00` §3 19.5 + register).
- **`StateSeeding` mapping** (`02` §2.1): map `download_state` → state as below, but **completion is always the AND of the two booleans regardless of `download_state`** (a torrent may read `download_state="uploading"` while files are already present):

```
download_state          -> intent
"downloading"           -> StateDownloading
"metaDL" / "checkingResumeData" -> keep StateQueued/StateDownloading (intermediate, keep polling)
"uploading"             -> StateSeeding   (files may already be present; do NOT treat as terminal)
"completed" / "cached"  -> success-leaning (rely on the booleans)
"paused"                -> keep current state, keep polling
Failed() (prefix failed/error OR contains "stalled")  -> StateFailed
// THEN, independent of the above: if rec.DownloadFinished && rec.DownloadPresent -> resolve + import (StateCompleted)
```

The literal `"stalled (no seeds)"` (parentheses + space) is matched by `Failed()`'s `Contains "stalled"` (locked, `03` quirk 2). `maybeRefreshWebDAV` is reused **as-is** — there is one WebDAV mount, so no torrent-specific refresh path is needed (`02-sab-worker-pipeline.md` rec 5).

### 2.4 Failure & re-search (FR-GP-7)

A grab that lands `StateFailed` (TorBox failure state, 6-miss debounce, or submit rejection) returns the linked media item to `wanted` (reconciler, §6) and emits a `grab_failed` notification (FR-NC-3). If automatic selection is enabled (FR-SR-4), the next candidate from the cached Prowlarr result set is picked by the selection score (§3) and a fresh job is created — manual search remains the MVP path. Known TorBox error codes (`ACTIVE_LIMIT`, `MONTHLY_LIMIT`, `COOLDOWN_LIMIT`, `DOWNLOAD_NOT_CACHED`, `DUPLICATE_ITEM`, `BOZO_TORRENT`/`BOZO_NZB`, `BAD_TOKEN`…) are surfaced verbatim to the notification center (`03` TorBox §).

### Quirks to bake in (grab)

1. Artifact is **stored before submit** (FR-GP-1) — bytes/magnet live on the job row; submitter never fetches.
2. Torrent submit prefers **magnet over `.torrent` bytes** (no HTTP round-trip); precedence hash>magnet>file.
3. **Torrent and usenet have separate create budgets and separate back-off timers** — never share a limiter or a cooldown.
4. Completion is `download_finished && download_present` for both; they can lag — keep polling.
5. `StateSeeding` is non-terminal and torrent-only; usenet jobs never enter it.
6. A failed mylist fetch is not a "miss"; only absence from a *successful* list counts toward the 6-miss fail.
7. `as_queued=true` only when active-slot-full but caps are not — queue, don't fail; ignored on Free plan.
8. `files==0` on resolve = WebDAV listed the folder before its contents → not ready; remove and keep awaiting.

---

## 3. Selection score (FR-SR-5)

A **deliberately simple, configurable weighted sum** — explicitly *not* Sonarr's custom-format engine (`00` §8, requirements §3). Selection runs when a release must be chosen automatically (search-on-add, re-search after fail/heal); the manual search UI shows the same per-release score so the operator's hand-pick and the auto-pick agree. **Hard reject conditions are evaluated first**; surviving releases are scored; ties are broken deterministically.

### 3.1 Inputs (per release)

From the Prowlarr `ReleaseResource` (`03` Prowlarr §) plus the parsed name (§5):
`protocol`, `size`, `seeders`/`leechers` (torrent), `grabs` (usenet), `indexerFlags` (`freeleech`…), parsed `Resolution`/`Quality`/`Group`/`Proper`/`Repack`, and the TorBox `checkcached` result for torrents (cached vs. will-download, `03` TorBox §).

### 3.2 Reject conditions (return score = −∞, never grabbed)

```
reject if resolution NOT in BOXARR_SELECT_ALLOWED_RESOLUTIONS (when non-empty)
reject if size < perQualityMin(quality)  OR  size > perQualityMax(quality)
reject if protocol == "torrent" AND seeders < BOXARR_SELECT_MIN_SEEDERS
reject if protocol == "usenet"  AND grabs   < BOXARR_SELECT_MIN_GRABS
reject if Group in BOXARR_SELECT_BLOCKED_GROUPS
reject if title matches any term in BOXARR_SELECT_BLOCKED_KEYWORDS
reject if protocol == "torrent" AND requireCached AND not cached  (BOXARR_SELECT_REQUIRE_CACHED, default false)
```

Per-quality min/max come from `BOXARR_SELECT_SIZE_LIMITS` (a JSON map in settings, see knobs); a missing quality key uses the global `BOXARR_SELECT_MIN_SIZE`/`_MAX_SIZE` fallback.

### 3.3 Scoring formula (concrete pseudocode)

```go
// internal/selection/score.go — deterministic, pure given config + release + cachedSet.
func Score(r Release, cfg SelectConfig, cached bool) int {
    if rejected(r, cfg, cached) { return math.MinInt }

    s := 0

    // 1) Resolution preference: index into the ordered preference list (higher = better).
    //    e.g. ["2160p","1080p","720p"] -> 2160p scores highest.
    if idx := indexOf(cfg.PreferredResolutions, r.Resolution); idx >= 0 {
        s += cfg.WeightResolution * (len(cfg.PreferredResolutions) - idx)
    }

    // 2) Quality/source preference (WEB-DL > BluRay-remux > BluRay > HDTV ... operator-ordered).
    if idx := indexOf(cfg.PreferredQualities, r.Quality); idx >= 0 {
        s += cfg.WeightQuality * (len(cfg.PreferredQualities) - idx)
    }

    // 3) Protocol preference (locked default: cached-torrent > usenet > uncached-torrent).
    switch {
    case r.Protocol == "torrent" && cached: s += cfg.WeightProtocolCachedTorrent  // default 300
    case r.Protocol == "usenet":            s += cfg.WeightProtocolUsenet         // default 200
    case r.Protocol == "torrent":           s += cfg.WeightProtocolUncachedTorrent// default 100
    }

    // 4) Health: seeders (torrent) or grabs (usenet), saturating so a 9000-seed release
    //    doesn't dwarf quality. cap at SeedSaturation (default 100).
    health := r.Seeders; if r.Protocol == "usenet" { health = r.Grabs }
    s += cfg.WeightHealth * min(health, cfg.SeedSaturation) / cfg.SeedSaturation  // 0..WeightHealth

    // 5) Preferred group / keyword bonuses (additive).
    if contains(cfg.PreferredGroups, r.Group)        { s += cfg.WeightPreferredGroup }    // default 150
    for _, kw := range cfg.PreferredKeywords {
        if strings.Contains(strings.ToLower(r.Title), kw) { s += cfg.WeightPreferredKeyword } // default 50
    }

    // 6) Freeleech bonus (torrent ratio-friendly).
    if hasFlag(r.IndexerFlags, "freeleech") { s += cfg.WeightFreeleech }  // default 40

    // 7) Proper/Repack edge bonus (small; tie-breaker-ish but counted in score).
    if r.Proper || r.Repack { s += cfg.WeightProper }  // default 25

    return s
}
```

### 3.4 Tie-breakers (applied in order when scores are equal)

1. **Cached torrent** beats anything not cached.
2. **Higher seeders** (torrent) / **higher grabs** (usenet).
3. **Smaller size** within the accepted band (cheaper on the plan, faster to import).
4. **Lower `indexerId` then lexicographic `guid`** — fully deterministic so re-runs pick the same release.

### 3.5 BOXARR_* knobs (cross-ref `08-config-deploy-ci.md`)

All follow the `int`/`default:` envconfig pattern (`04-sab-api-config-ci.md` rec); slices are comma-separated; `SIZE_LIMITS` is a JSON string (settings UI). Runtime overrides live in the `settings` KV table (`02` §3.5).

| Env var | Type | Default | Meaning |
|---|---|---|---|
| `BOXARR_SELECT_PREFERRED_RESOLUTIONS` | csv | `2160p,1080p,720p` | ordered best→worst |
| `BOXARR_SELECT_ALLOWED_RESOLUTIONS` | csv | (empty=all) | hard allow-list |
| `BOXARR_SELECT_PREFERRED_QUALITIES` | csv | `WEB-DL,BluRay,WEBRip,HDTV` | ordered best→worst |
| `BOXARR_SELECT_MIN_SIZE` / `_MAX_SIZE` | int (bytes) | `0` / `0`(=∞) | global size band fallback |
| `BOXARR_SELECT_SIZE_LIMITS` | json | `{}` | per-quality `{ "2160p": {"min":..,"max":..} }` |
| `BOXARR_SELECT_MIN_SEEDERS` | int | `1` | torrent reject threshold |
| `BOXARR_SELECT_MIN_GRABS` | int | `0` | usenet reject threshold |
| `BOXARR_SELECT_REQUIRE_CACHED` | bool | `false` | reject uncached torrents |
| `BOXARR_SELECT_PREFERRED_GROUPS` / `_BLOCKED_GROUPS` | csv | (empty) | release-group lists |
| `BOXARR_SELECT_PREFERRED_KEYWORDS` / `_BLOCKED_KEYWORDS` | csv | (empty) | title-substring lists |
| `BOXARR_SELECT_WEIGHT_RESOLUTION` | int | `400` | weight (3.3 step 1) |
| `BOXARR_SELECT_WEIGHT_QUALITY` | int | `200` | weight (step 2) |
| `BOXARR_SELECT_WEIGHT_PROTOCOL_CACHED_TORRENT` | int | `300` | step 3 |
| `BOXARR_SELECT_WEIGHT_PROTOCOL_USENET` | int | `200` | step 3 |
| `BOXARR_SELECT_WEIGHT_PROTOCOL_UNCACHED_TORRENT` | int | `100` | step 3 |
| `BOXARR_SELECT_WEIGHT_HEALTH` | int | `100` | step 4 |
| `BOXARR_SELECT_SEED_SATURATION` | int | `100` | step 4 saturation cap |
| `BOXARR_SELECT_WEIGHT_PREFERRED_GROUP` | int | `150` | step 5 |
| `BOXARR_SELECT_WEIGHT_PREFERRED_KEYWORD` | int | `50` | step 5 |
| `BOXARR_SELECT_WEIGHT_FREELEECH` | int | `40` | step 6 |
| `BOXARR_SELECT_WEIGHT_PROPER` | int | `25` | step 7 |
| `BOXARR_SELECT_MIN_SCORE` | int | `0` | reject if final score `< MIN_SCORE` |

**Cached status** is resolved by batching all torrent `infoHash`es through `CheckCached` (≤100 per call, `format=list`, `03` TorBox §) before scoring; usenet releases skip this step. With the locked defaults, a cached 1080p WEB-DL torrent (300+400×2+200×3=1700-ish) beats an uncached one and a comparable usenet release, matching the protocol preference "cached-torrent > usenet > uncached-torrent".

---

## 4. Import + library naming (DIRECT-to-library symlink model)

**Decision (00 §5.1, reframing requirements §11.1/§11.6):** Boxarr **is its own importer** — there is no external Sonarr/Radarr move-import step and **no `_incoming` symlink-farm intermediate**. Boxarr writes the **library symlink directly at its final Plex path**, pointing (absolute path) at the file inside the flat WebDAV release folder. Two invariants replace the historical "rename within one filesystem" constraint: (a) the library symlink targets the WebDAV release file by absolute path; (b) the WebDAV mount is at the same absolute path in every container (Assumption D). The existing `atomicReplaceSymlink` (the only `rename(2)` in the codebase) is reused for **heal repoint**, and `removeSymlinkDir`/`classifyReleaseDir`/the `reaper` are reused on the library symlinks.

### 4.1 Importer flow (StateCompleted → StateImported)

The importer runs when the poller sets `StateCompleted` with `StoragePath=<resolved WebDAV release dir>`:

1. Load the job's `(media_type, media_ref)` mapping (recorded at submit, FR-IMP-1) — this is the source of truth, so TorBox's flat namespace is a non-issue for Boxarr's own downloads.
2. **Enumerate the release-folder files** (`os.ReadDir`/walk of `StoragePath`); ignore samples, `.nfo`, subtitles handled per-video.
3. **Movie** (`media_type='movie'`): pick the largest video file; compute the target path via the namer (§4.2); `os.MkdirAll` the `<Title> (<Year>)` dir; `atomicReplaceSymlink(libPath, absVideoPath)` (idempotent — `Lstat` skip if already correct). Set `movie.library_path`, `movie.has_file=1`, `movie.status='available'`, `movie.job_id`.
4. **Single episode** (`media_type='episode'`): same, but the season dir + episode filename from the namer.
5. **Season pack / series** (`media_type IN ('season','series')`): **expand** (§4.3) — parse each file, map to an episode, write one symlink per episode, set each episode's `library_path`/`has_file`/`status='available'`/`job_id`.
6. Record each created link in `imported_symlinks` (`UpsertImportedSymlink`, the only UNIQUE-driven upsert, `02` §1.4) so the healer can discover and repoint it.
7. Transition the job `StateCompleted → StateImported`; emit `download_completed` (FR-NC-3).
8. **Optional Plex partial scan** (§4.4).

**No byte copy, ever** (requirements §11.6) — links are absolute symlinks into the WebDAV mount, exactly as `buildSymlinkFarm` did, but written **at the final library path** rather than a drop-zone (the one structural change from sab2torbox). An **explicit same-filesystem / same-path guard** is added (which sab2torbox lacked): at startup, `os.Stat` the library roots and the WebDAV mount and warn/refuse if the mount is absent (Assumption D; `00` §9 foundation note).

### 4.2 Library namer (Plex-standard, 00 §6)

```
Movies: <MOVIE_LIBRARY_ROOT>/<Title> (<Year>)/<Title> (<Year>).<ext>
TV:     <TV_LIBRARY_ROOT>/<Series Title> (<Year>)/Season <NN>/<Series Title> - S<NN>E<MM> - <Episode Title>.<ext>
```

Rules (locked, `00` §6):
- **Zero-padded** season/episode numbers (`Season 01`, `S01E05`); `<NN>`/`<MM>` are 2-digit minimum, widening for >99.
- **Sanitize illegal path chars** (`/ \ : * ? " < > |` → space; collapse runs; trim trailing dots/spaces — Windows/SMB-safe for Plex on any backing FS).
- `<Year>` from `series.year`/`movie.year` (TMDB first-air/release year); omit the ` (<Year>)` suffix only if year is unknown.
- `<Episode Title>` from `episode.title`; if empty, omit the ` - <Episode Title>` segment.
- **Multi-episode file** (range/adjacent, §5): filename uses `S01E01-E03` (range) or `S01E01E02E03` (adjacent), and **one symlink is created at the multi-ep filename, plus the file is recorded against every covered episode** (each episode row gets the same `library_path`; Plex reads the multi-ep file once). `<Episode Title>` is the first episode's title.
- **Daily show** (`series_type='daily'`, §5): `<Series Title> - <YYYY-MM-DD> - <Episode Title>.<ext>` (Plex daily convention) instead of `S<NN>E<MM>`.
- **Anime** (`series_type='anime'`): mapped to `S<NN>E<MM>` via the absolute→(season,episode) lookup (§4.3); the absolute number is preserved in `episode.absolute_number` but the **library layout stays Plex aired-order** so Plex's agent matches.
- `<ext>` is the source video extension (lower-cased).

### 4.3 Season-pack expansion (enumerate → map each to an episode)

When the import is a multi-file pack (`media_type IN ('season','series')`, or a movie folder that is actually a pack — rare), each video file is run through `ParseRelease` (§5) and mapped to a catalog episode:

1. **Parse the file name** → `ParsedRelease{SeasonNumber, EpisodeStart, EpisodeEnd, AbsoluteEpisodes, AirDate}`.
2. **Resolve target episode(s)** against the stored catalog (`store.ListEpisodes(seriesID)`):
   - **Standard / DVD:** match `(season_number, episode_number)`; a range/adjacent file covers `EpisodeStart..EpisodeEnd`.
   - **Daily:** match `episode.air_date == AirDate` (lexicographic on the stored `YYYY-MM-DD` string).
   - **Anime (`series_type='anime'`):** map each `AbsoluteEpisodes[i]` via `episode.absolute_number` (stored from TVDB `absolute` ordering, `03` TVDB §) → `(season_number, episode_number)`. If `absolute_number` is unpopulated, fall back to the TVDB `/episodes/absolute` list fetched at catalog time (locked: Boxarr stores **both** `absolute_number` and `(season,number)` so either strategy works without a live call, `03` TVDB quirk 4).
3. **Bare season pack** (`IsSeasonPack && EpisodeStart==0`): the pack is the *whole season folder*, but TorBox delivered individual files — so expansion is per-file (step 1–2). If a single archive/file represents the season with no per-episode names, look up the **full episode list for `Season` from the catalog** (TVDB/TMDB-sourced, `11` rec 3) and map files positionally by sort order as a last resort, logging a warning.
4. **Unmapped file** (no confident episode match): leave it un-imported, record it for the reconciler (§6) as a partial-pack anomaly, and emit a notification rather than guessing — same caution as `findBestMatch` (never relink to a different edition, `03-sab-worker-lifecycle.md` `symlink.go:142-173`).

### 4.4 Optional Plex partial scan (FR-IMP-5)

Gated by `plex.Enabled()` (both URL + token set; mirror `WebDAVRefreshEnabled()`). After the symlink(s) are written:
- Determine the section: movie → `section_id_movie`, TV → `section_id_tv` (auto-detected from `/library/sections`, `03` Plex §).
- Compute the scan path **as Plex sees it** — identical to Boxarr's library path under Assumption D (same-path mount; a configurable `host→plex` prefix map is the escape hatch).
- Movies: scan the movie's own folder. TV: scan the **season-level** folder (faster, sufficient).
- `GET /library/sections/{id}/refresh?path=<quote_plus(path)>` (Go `url.QueryEscape` → spaces `+`; fallback `%20` if a space-path scan fails — `00` §9 Plex). Returns 200 + empty body **async**; never wait.
- **Fallback:** if the item doesn't appear within `BOXARR_PLEX_SCAN_TIMEOUT` (default 60 s), fire the full-section scan and log a warning (large-library partial-scan reliability gap, `03` Plex §).
- **PMS version guard:** at startup `GET /identity`; if `< ~1.20.0.3125`, warn that partial scan is unsupported and advise the operator to rely on the full scan (`00` §9 Plex).

### Quirks to bake in (import)

1. Library symlink is written **at the final Plex path**, not a drop-zone — no `_incoming`, no second move.
2. Symlink targets are **absolute** WebDAV paths; same-path mount (Assumption D) makes them resolvable from Plex's container.
3. `atomicReplaceSymlink` (tmp + `rename(2)`) is the only rename; reused for heal repoint, never for media bytes.
4. Multi-episode files map to **every** covered episode row (same `library_path`); one physical symlink.
5. Anime keeps **Plex aired-order layout** even though parsing is absolute-number; `absolute_number` stored for mapping only.
6. Unmapped pack files are flagged to the reconciler, never guessed.
7. Plex scan is **best-effort and async**; failure never blocks the import.

---

## 5. Release-name parsing (§11.4) — pure-Go stack

**Decision (00 §5.2):** pure-Go, no Python/JS sidecar (a `guessit`/`parsett` route breaks the distroless single-binary deploy — `11` "Why Shelling Out…"). The stack is **`github.com/chill-institute/torrentname@v1.4.0`** (primary; MIT, zero-dep, has `EpisodeEnd`/`Complete`) + **`github.com/nssteinbrenner/anitogo@v1.0.0`** (anime absolute; MPL-2.0, file-level copyleft — usable unmodified) + **three in-house regexes Boxarr owns**: adjacent multi-episode `S01E01E02`, daily-show `YYYY-MM-DD`, and bare season packs (`S01` with no `COMPLETE` keyword). Lives in a new `internal/release` (a.k.a. `mediamatch`) package (`01-architecture-and-packages.md`).

### 5.1 Unified `ParsedRelease` struct

```go
// internal/release/parse.go
type ParsedRelease struct {
    Title            string
    Year             int
    SeasonNumber     int
    EpisodeStart     int    // 0 = unknown / season-pack
    EpisodeEnd       int    // 0 = single; >0 = range/adjacent end
    AbsoluteEpisodes []int  // anime absolute numbering
    AirDate          string // "YYYY-MM-DD" for daily shows, else ""
    IsSeasonPack     bool
    IsAnime          bool   // [Group] prefix or episode>99 heuristic
    Quality          string
    Resolution       string
    Codec            string
    HDR              string
    Audio            string
    Group            string
    Source           string
    Proper           bool
    Repack           bool
}

func ParseRelease(filename string) (*ParsedRelease, error)
```

### 5.2 The three in-house regexes (Boxarr owns)

```go
// daily show: The.Daily.Show.2024.01.15 or ...2024-01-15
var reDailyDate = regexp.MustCompile(
    `(?i)(?P<year>(?:19|20)\d{2})[-_. ]+(?P<month>0\d|1[0-2])[-_. ]+(?P<day>[0-2]\d|3[01])(?:[^0-9]|$)`)

// S01E01E02 adjacent multi-episode (no dash; chill-institute gap, 11 uncertainty)
var reAdjacentMultiEp = regexp.MustCompile(`(?i)S(?P<season>\d{1,2})(?:E(?P<ep>\d{2,3}))+`)

// bare season pack with NO "COMPLETE" keyword: "Show.S01.1080p.BluRay" (chill-institute Complete gap, 11 rec 4.3)
var reBareSeasonPack = regexp.MustCompile(`(?i)\bS(?P<season>\d{1,2})\b(?!\s*E\d)`)
```

### 5.3 Algorithm (combine + supplement)

1. `torrentname.Parse(filename)` → map `Title/Year/Season→SeasonNumber/Episode→EpisodeStart/EpisodeEnd/Complete→IsSeasonPack/Quality/Resolution/Codec/HDR/Audio/Group/Source/Proper/Repack`.
2. `reAdjacentMultiEp`: if ≥2 `E` groups, set `EpisodeStart=first`, `EpisodeEnd=last` (covers the `S01E01E02` gap).
3. `reDailyDate`: on a valid `time.Parse("2006-01-02", …)`, set `AirDate`; mark the series `daily` if season/episode are both 0.
4. `reBareSeasonPack`: if matched and no episode marker follows, set `IsSeasonPack=true`, `SeasonNumber` from the capture (covers the `Complete`-keyword gap).
5. **Anime supplement:** if `[Group]`-prefixed (`^\s*\[`) or (`SeasonNumber==0 && EpisodeStart>99`), set `IsAnime=true`, call `anitogo.Parse(filename, anitogo.DefaultOptions)`, fill `Title` from `AnimeTitle` if empty, append each numeric `EpisodeNumber` to `AbsoluteEpisodes`, and fill `SeasonNumber` from `AnimeSeason[0]` if still 0.

### 5.4 Golden-file test table (gates regressions — exact `11` rec 5 strings)

| Input | Expected |
|---|---|
| `Mr.Robot.S01E05.HDTV.x264-KILLERS` | S1E5 (single) |
| `Show.Name.S01E01-E03.1080p.WEB-DL.x264-GRP` | S1, EpisodeStart=1, EpisodeEnd=3 (torrentname range) |
| `Show.Name.S01E01E02.1080p.BluRay.x264-GRP` | S1, EpisodeStart=1, EpisodeEnd=2 (in-house adjacent regex) |
| `Sample Series S01 COMPLETE 720p WEBRip x264-GRP` | S1, IsSeasonPack=true (torrentname `Complete`) |
| `Show.S01.1080p.BluRay` | S1, IsSeasonPack=true (in-house bare-pack regex) |
| `The.Daily.Show.2024.01.15.720p.WEB-DL` | AirDate=`2024-01-15` (in-house daily regex) |
| `[HorribleSubs] Detective Conan - 862 [1080p].mkv` | AbsoluteEpisodes=[862], IsAnime=true |
| `[SubGroup] Attack on Titan S04E01 [1080p]` | S4E1, IsAnime=true |
| `Bleach 225.mkv` | AbsoluteEpisodes=[225] (anitogo path) |

**Runtime-verify (`00` §9 / `11` uncertainties):** chill-institute's handling of adjacent `S01E01E02` is unconfirmed — the in-house regex is the fallback and is exercised by the golden table; the bare-pack frequency in real feeds drives whether `reBareSeasonPack` needs tuning; anitogo MPL-2.0 stays unmodified (file-level copyleft, no source release needed).

### 5.5 Season-pack → episode-list mapping (TVDB/TMDB)

Already detailed in §4.3. The mapping key is the parsed `Season`/`AirDate`/`AbsoluteEpisodes` against the **stored** catalog (built from TMDB seasons/episodes + TVDB ordering, `03`). `series_type` (`standard`/`daily`/`anime`, `02` §3.2) selects the strategy.

---

## 6. Reconciler (15-min sweep, §15)

A new `reconcileOnce` loop on `BOXARR_RECONCILE_INTERVAL` (default 15 m, matching TorBox's WebDAV refresh cadence; FR-NC-1). It is the slow, full-sweep reconciliation path; **the forced `/refresh` stays the fast path for in-flight imports** (FR-NC-5) and is unchanged.

### 6.1 Sweep algorithm

1. **List WebDAV mount** (one `os.ReadDir` of the flat root(s) — `UsenetPath()` and `TorrentPath()` if distinct) → set of release-folder names + sizes (sum of folder files).
2. **List both mylists** (`ListUsenet` + `ListTorrents`, `bypass_cache=true`) → known TorBox downloads keyed by `name`/`hash`.
3. **Load all known jobs** (`store` — every job with a `storage_path` or `torbox_hash`).
4. For each mount item, `UpsertWebDAVItem` (`02` §5.4 — UNIQUE on `remote_path`, refreshes `size`/`last_seen`, clears `is_broken`):
   - **Match to a job** by folder name == `filepath.Base(job.StoragePath)` or by `torbox_hash` → `known=1`, `job_id` set, `category` from the job's `media_type` (`movie`/`series`).
   - **No match** → `known=0`, `category` best-effort from `ParseRelease` of the folder name (`movie` if it parses as a movie title+year with no S/E, else `series`, else `unknown`).
5. **Mark vanished:** `MarkWebDAVItemsBrokenNotSeenSince(sweepStart)` flips `is_broken=1` on rows whose `last_seen` predates this sweep (gone from the mount) — feeds the healer's broken-detection.
6. **Status reconcile** (the projection sync, `02` §2.2): for each catalog item, if its job reached `StateImported` and the symlink is intact → `status='available'`; if the job hit `StateFailed` → return to `wanted`; if the symlink is broken / mount item vanished → `status='expired_broken'` (heal candidate). This is the loop that keeps `movie.status`/`episode.status` (denormalized, fast-to-query) honest against `jobs.state` (authority).
7. **Storage overview** (FR-ST-1/2): `WebDAVUsageBytes()` = `SUM(size) WHERE is_broken=0`; surface `/user/me` plan tier, active slots in use, monthly usage/cooldown (FR-LIM-4).

### 6.2 Unknown-content notifications (FR-NC-2)

Each `webdav_item` with `known=0` (and not previously notified) raises an `unknown_content` notification (`02` §3.3 — `job_id` nullable) carrying `{name, size, category}`, with UI actions:
- **Adopt/categorize** — create a catalog row + a synthetic `imported` job linked to the existing TorBox download, then re-import (write the library symlink) so Boxarr owns it going forward.
- **Ignore** — mark the item ignored (a `settings`/flag) so subsequent sweeps don't re-notify.
- **Delete from TorBox** — enqueue a delete (controlusenet/controltorrent), §5.

Event notifications (FR-NC-3) — `download_completed`, `grab_failed`, `heal_triggered`/`heal_succeeded`/`heal_failed`, `deletion_completed`, `limit_reached` — are enqueued by the respective workers via `EnqueueNotification`; the badge is `UnreadCount()`, feed is newest-first (`02` §5.3).

### Quirks to bake in (reconcile)

1. 15-min sweep is reconciliation + unknown-detection only; **`/refresh` remains the in-flight fast path** (FR-NC-5).
2. Mount listing is the expensive op — confined to the sweep + targeted post-completion `resolveStoragePath` (NFR-7).
3. `webdav_item.remote_path` UNIQUE upsert mirrors `imported_symlinks` exactly — re-seen each sweep.
4. The reconciler is the single place `media status` is reconciled against `jobs.state`; library views never scan WebDAV (FR-UI-9).
5. Adopt creates a synthetic `imported` job so the standard heal/delete machinery applies to adopted content.

---

## 7. TorBox limits (§13)

**Reuse the existing 429/`Retry-After` handling verbatim** (`RateLimit`, `Retryable`, `parseRetryAfter` — `client.go:155-211`) for every TorBox call including the new torrent endpoints (FR-LIM-1). On top of that, a **grab limiter** gates submissions:

### 7.1 Concurrency gate vs. plan active-slot allowance (FR-LIM-2)

- `/user/me.plan` (int) → static slot map `{0:1, 1:3, 2:10, 3:5}` (Free/Essential/Pro/Standard — `03` TorBox §; **runtime-verify**, re-derive if `ACTIVE_LIMIT` appears unexpectedly).
- Active downloads = count of jobs in `state IN (submitting,queued,downloading,seeding)` across **both** protocols.
- Before any `Create*` call, if `active >= slots`, **queue the job** (leave `pending`; for torrents set `as_queued=true` if the caps below allow) rather than submit. On a TorBox `ACTIVE_LIMIT` error, do **not** retry — requeue + `limit_reached` notification.

### 7.2 Create-cap windows (FR-LIM-3)

Two sliding-window counters (in `rateWindow`, `timeNow`-driven, per-process; the worker is single-instance):

| Cap | Scope | Window | Action when approached |
|---|---|---|---|
| **60/hour** create | usenet `createusenetdownload` | 1 h | `usenetCreateWindow`; pause `submitter` (existing `submitBackoffUntil` style) |
| **60/hour** uncached create | torrent `createtorrent` (uncached) | 1 h | `torrentCreateWindow` hourly bucket; queue (`as_queued`) the rest |
| **300/min** shared | torrent `createtorrent` (cached counts here) | 1 m | `torrentCreateWindow` minute bucket; token-bucket pace (~1 every 200 ms) |

**Cached vs. uncached** is known from the `CheckCached` step at selection time — a cached torrent counts against the 300/min bucket, an uncached one against the 60/hr bucket. The limiter **backs off and queues, never fails the grab** (FR-LIM-3): when a window is full, the submitter stops draining (mirroring the existing 429 `submitBackoffUntil` pattern) and resumes on the next tick once the window has room.

### 7.3 Cooldown / monthly (FR-LIM-4)

On `MONTHLY_LIMIT`/`COOLDOWN_LIMIT`, read `cooldown_until` from `/user/me` and surface the exact resume time in a `limit_reached` notification + the storage overview; pause new submits until `cooldown_until`. Free-plan 10-download monthly limit (`plan==0`) is tracked the same way. `/user/me` is polled by the reconciler (15 m cadence is fine for plan/usage; cached 5 m by the health check pattern).

### Quirks to bake in (limits)

1. **Separate** usenet (60/hr) and torrent (60/hr uncached + 300/min shared) budgets — never share a window.
2. `ACTIVE_LIMIT` ⇒ no retry, requeue + notify; `COOLDOWN_LIMIT`/`MONTHLY_LIMIT` ⇒ read `cooldown_until`, surface exact time.
3. Slot count is **derived from `plan`** (not returned by `/user/me`); recheck on unexpected `ACTIVE_LIMIT`.
4. Cached torrents count against 300/min, uncached against 60/hr — classify via `CheckCached` before submit.
5. The limiter **queues**, it does not fail — back-off + resume, mirroring the existing submitter cooldown.

---

## 8. Delete & lifecycle (§12)

### 8.1 Deleter (FR-DEL-1/2)

Reuse `deleteOnce`/`deleteJob` (verified `deleter.go`) with **one branch on `protocol`** and the rest verbatim:
1. UI/API flips the target media (and its job) to `state='deleted'` (the `imported/completed → deleted` transition already exists, `02` §2.1).
2. `deleteJob`: if `TorBoxID != 0`, propagate to TorBox — usenet → `ControlUsenet(id,"delete")` (`{usenet_id,operation:"delete"}`); torrent → `ControlTorrent(id,"delete")` (`{torrent_id,operation:"delete"}`). **Never set `all=true`** (dangerous + SDK-unconfirmed, `03` quirk 9 / FR-DEL-1).
3. **Transient-retry then drop:** on error, `deleteAttempts[id]++`; if `< deleteGiveUpAttempts (60, ~1 h at 1 m poll)` log Warn "will retry next cycle" and keep the row; else log **Error** "dropping job, download may be orphaned" and proceed.
4. Remove the library symlink(s) via the guarded `removeSymlinkDir`/per-link unlink (within library root) and `store.DeleteJob` (cascades `imported_symlinks`, SET-NULLs the catalog `job_id`, `02` §3.1/§3.2). Emit `deletion_completed`.

**Cascade for series/season (FR-DEL-2):** a layer **above** `deleteOnce` (`02-sab-worker-pipeline.md` rec c). API endpoint (e.g. `POST /api/v1/series/{id}/delete?season=N`, `04`) selects the job set via new store queries `JobsBySeries(seriesID)`/`JobsBySeason(seriesID,season)` keyed off `(media_type,media_ref)`, **transactionally flips them all to `deleted`**, and returns; the existing `deleteOnce` loop drains them **one `Control*` call at a time** (preserving per-item transient retry + orphan logging). Never bulk-`RemoveAll` a series dir — keep the guarded per-release removal; then reap now-empty season/series parent dirs with the `isWithin` guard (mirrors `sweepSymlinkFarm`'s "never remove a category dir or the root").

### 8.2 Healer (FR-HEAL-1/2)

Reuse the entire heal machinery (`healer.go`) and **extend it for torrents + the Prowlarr re-search fallback** (`00` §3 19.4):

**Detect → resubmit stored artifact (reused):**
1. `discoverSymlinks` (hourly re-walk = source of truth) records library symlinks pointing into the WebDAV mount; `detectBrokenSymlinks` flags broken ones — **`Lstat` ok but `Stat` fails = dangling target** (target gone). If `Lstat` itself fails, the link was moved → delete the row for rediscovery.
2. `triggerHeals`: skip if `state ∉ {imported,heal_failed}`, if `HealCount >= HealMaxAttempts` (default 3 — manual needed), or if within `healBackoff(initial, count)` (exponential: `initial*2^count`, default 5m→10m→20m…). Else `startHeal`.
3. `startHeal` **resubmits the stored artifact**, branching on `protocol` (`03-sab-worker-lifecycle.md` rec a):
   - usenet → `CreateUsenetDownload{NZBContent, NZBName+".nzb", Link: NZBURL}`.
   - torrent → `CreateTorrent{Magnet: TorrentMagnet, TorrentContent: TorrentFile, TorrentName}`.
   - Set `TorBoxID`/`TorBoxHash`, transition `StateHealing`; emit `heal_triggered`.
4. `healReconcileOnce` watches `StateHealing` jobs against the matching mylist (`ListUsenet`/`ListTorrents`, keyed by `TorBoxID`); on `Failed()` → `markHealFailed`; on `download_finished && download_present` → `finishHeal`.
5. `finishHeal` (protocol-agnostic, reused verbatim) repoints each broken link: `newTarget = Join(newReleaseDir, base)`; if `Stat` fails, `findBestMatch(newReleaseDir, base)` (exact case-insensitive **or** same-extension longest-common-prefix with score > len/2, else skip — never guess by "lone video file"); `atomicReplaceSymlink`; `UpdateSymlinkTarget`. **Critical guard preserved:** `healed==0 && broken>0` → `markHealFailed`, never a misleading success. On success: `StateImported`, `HealCount=0`, `LastHealedAt`, clear error; emit `heal_succeeded`.

**On dead artifact → fresh Prowlarr re-search (FR-HEAL-2, the genuinely new piece, `00` §3 19.4):**
- Triggered when there is **no stored artifact** (both `nzb_content`/`nzb_url` and `torrent_*` empty) **or** the resubmit's download lands `Failed()`.
- Gated by `BOXARR_HEAL_PROWLARR_FALLBACK` (default `true`). Query `prowlarr.Search` with the original release title + the appropriate category (`2000`/`5000`), score the results with the **same selection score (§3)**, and best-match against the original release name (prefer same quality/edition to avoid relinking different content — same caution as `findBestMatch`).
- Submit the chosen release's magnet/`.torrent`/`.nzb` via the protocol-appropriate create call, **persist the new artifact onto the job** (so subsequent heals reuse it), transition `StateHealing`. The repoint half handles a possibly-different filename via the existing best-match logic.
- Wrap the whole resubmit-then-research as **one** heal attempt (one `HealCount` increment); record `last_heal_method` so `/health/heal_failed` distinguishes "dead artifact" from "re-search failed". Emit a distinct event (e.g. `method:"prowlarr_research"`).

**Escape hatch (reused):** `POST /api/v1/.../heal/{jobID}/give_up` → `StateManuallyResolved` (terminal; healer ignores it) + delete its symlink rows; `.../retry` → reset `HealCount`/`LastHealedAt`/`LastHealError` (both require current `heal_failed`). Status side: a healing item shows `expired_broken`; on success it returns to `available`.

### 8.3 Reaper (reused verbatim, FR-IMP via reconcile)

`reapOnce` (5 m): `detectImports` (StateCompleted job whose release dir is **empty or gone** → still used as a safety net even though Boxarr now imports directly; an empty dir means the operator/another tool moved links), `ReapImported` (TTL 24 h — `DELETE FROM jobs WHERE state='imported' AND updated_at < cutoff`), and `sweepSymlinkFarm` adapted to the library roots: per release dir, `classifyReleaseDir` → empty → `os.Remove`; all-broken & not in `ActiveStoragePaths` → `os.RemoveAll`. **Never removes a library root or a `<Title>`/`Season NN` parent that still owns active links** (the `isWithin` + active-set guards are preserved). `ActiveStoragePaths` = `storage_path` of jobs `state NOT IN (deleted,failed)`.

### Quirks to bake in (lifecycle)

1. Delete branches on `protocol` only — the transient-retry (60 attempts ~1 h) + guarded removal + `DeleteJob` tail are verbatim.
2. `all=true` is **never** sent to `controltorrent`/`controlusenetdownload` (dangerous, SDK-unconfirmed).
3. Series/season delete is a **transactional state flip** above the per-item deleter — resumable across restarts, idempotent.
4. Heal resubmits the **stored** artifact first; Prowlarr re-search is the dead-artifact fallback, counted as one attempt.
5. `healed==0 && broken>0` is a **failure**, never a success — preserved across both protocol + re-search extensions.
6. `findBestMatch` never guesses by "lone video file" — exact or strong-prefix match only, else leave the broken link.
7. `manually_resolved` is terminal and invisible to the healer; backoff is exponential with a max-attempts cap.

---

## Definition of done

Boxarr's pipelines are done when: the torrent submit/poll loops run **parallel** to the proven usenet loops with their own back-off/missing-poll maps and their own create-cap windows (a usenet 429 never pauses torrents), each step mapping onto the twelve-state `jobs.state` machine (`pending→submitting→queued→downloading[→seeding]→completed→imported`) with `download_finished && download_present` as the universal completion gate and `Failed()` (incl. `stalled (no seeds)`) as the failure gate; the artifact is always stored on the job before submit (magnet preferred over `.torrent` bytes) and dedup runs `FindByTorrentHash`/`FindBySHA256`+`FindByURL` before insert (FR-GP-5); the selection score is the documented configurable weighted sum with hard rejects, deterministic tie-breakers, and the full `BOXARR_SELECT_*` knob set, defaulting to cached-torrent > usenet > uncached-torrent; the importer writes library symlinks **directly at the final Plex path** (no `_incoming`, no byte copy, absolute targets, `atomicReplaceSymlink` for heal repoint), expands season packs by parsing+mapping each file to an episode via the catalog (standard/daily/anime strategies), and fires an optional async Plex partial scan with full-section fallback; release parsing is `chill-institute/torrentname` + `anitogo` + the three in-house regexes producing the unified `ParsedRelease`, passing the golden-file table verbatim; the deleter propagates per-protocol `delete` with the 60-attempt transient retry and series/season cascade, the healer resubmits the stored artifact then falls back to a fresh Prowlarr re-search + best-match repoint (backoff/max-attempts/`manually_resolved` preserved, extended to torrents), and the reaper sweeps empty/orphaned dirs under the library roots; the 15-min reconciler lists the WebDAV mount + both mylists vs. known jobs, upserts `webdav_item`, raises adopt/ignore/delete notifications for unknown content, and reconciles media `status` against `jobs.state` while the forced `/refresh` stays the in-flight fast path; the limiter gates on the plan active-slot allowance and the 60/hr + 300/min create caps by queueing rather than failing; and every new `time.Now()` reads through `worker.timeNow`, every new exported symbol is documented, and `gofmt -s`/`golangci-lint`/`go test -race` pass. Cross-refs: `00-decisions-and-assumptions.md` (§5.1 import model, §5.2 parsing stack, §3 heal decision, §9 register), `02-data-model.md` (job states, media status, store methods, season-pack mapping consumers), `03-external-contracts.md` (TorBox/Prowlarr/Plex/TMDB/TVDB calls), `04-internal-api.md` (grab/delete/heal endpoints, notification payloads), `05-seerr-emulation.md` (search-on-add invokes selection), `08-config-deploy-ci.md` (`BOXARR_SELECT_*`/`_LIMIT`/`_RECONCILE_INTERVAL`/`_HEAL_PROWLARR_FALLBACK` env reference).
