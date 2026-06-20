# Boxarr — Requirements Specification

**Name:** Boxarr — a TorBox-backed media manager for TV and movies (binary and image: `boxarr`).
**Audience:** Claude Code (implementation agent)
**Status:** Draft for implementation
**Related project:** [`radaiko/sab2torbox`](https://github.com/radaiko/sab2torbox) — the proven Usenet→TorBox→WebDAV pipeline this tool extends and absorbs.
**Detailed design:** the implementer-ready companion specs live in [`docs/specs/`](./specs/) (`00`–`09`). Where this high-level document and a companion spec differ, the companion specs are authoritative (they were ground-truthed against the real `sab2torbox` code and the live external APIs). Start at [`docs/specs/00-decisions-and-assumptions.md`](./specs/00-decisions-and-assumptions.md).

---

## 1. Purpose

A single self-hosted application that replaces Sonarr **and** Radarr for a TorBox-WebDAV-backed media library. It lets the user search for TV series and movies (manually, Sonarr/Radarr-style, and via Overseerr/Jellyseerr requests), submit the chosen releases to TorBox over **both** the Usenet and torrent interfaces, surface the completed files from TorBox's WebDAV mount into a Plex-readable library, and manage the full lifecycle (storage visibility, deletion that propagates to TorBox, detection of unknown content, and re-acquisition of expired content).

The core reason this is *not* a Sonarr clone: indexers are delegated to **Prowlarr**, catalog/metadata to **TMDB**, and final filename→episode parsing to **Plex**. What remains is orchestration plus a lifecycle model that Sonarr/Radarr structurally lack (content lives on TorBox and can disappear).

---

## 2. Goals

- One application, one UI, handling **both** series and movies, with the UI split into separate Series and Movies sections.
- Manual interactive search (like Sonarr/Radarr) against Prowlarr, returning both Usenet and torrent releases.
- Grab pipeline: store the chosen NZB/torrent locally, submit to TorBox (Usenet via the `sab2torbox` flow, torrents via TorBox's torrent API), monitor to completion.
- Import: route each completed release into the correct Plex library (movie vs series) and present it correctly to Plex, even though TorBox writes everything into one flat WebDAV namespace.
- Deletion of an episode or a movie removes it from the WebDAV/TorBox side as well, handled directly by this tool.
- A 15-minute reconciliation poll (matching TorBox's WebDAV refresh cadence) that detects new/unknown content and raises it in an in-app notification center.
- Overseerr/Jellyseerr support by emulating the Sonarr and Radarr v3 APIs so Seerr can add series and movies to this tool.
- A complete WebDAV browser view listing every item on the mount, categorized (movie / series / unknown) and with size.
- A storage overview showing total used size and plan limits.

## 3. Non-Goals (scope guards)

- **Do not** implement indexer protocols (Newznab/Torznab) or scrapers — Prowlarr owns all indexer logic.
- **Do not** reimplement Sonarr's full custom-formats/quality-scoring engine. A simple, configurable release-selection score is sufficient (see §8.4); richer scoring can come later.
- **Do not** rewrite the proven `sab2torbox` Usenet pipeline, symlink-farm logic, healer, or deleter. Reuse and extend them.
- **Do not** download media bytes to local disk. Bytes stay on TorBox and are reached only through the rclone WebDAV mount. Imports must be a **rename within one filesystem**, never a byte copy (see §11.1).
- **Do not** build a public/multi-tenant service. Single-operator, local-first.

---

## 4. Architecture Overview

```
  SEARCH / METADATA              THIS TOOL                         TORBOX / PLAYBACK
 ┌──────────────┐  search   ┌──────────────────────────┐  NZB    ┌──────────────┐
 │   Prowlarr   ├──────────▶│  catalog · monitor       │  magnet │   TorBox     │
 │ (indexers)   │  releases │  search · select         ├────────▶│  usenet +    │
 └──────────────┘           │  grab pipeline           │ control │  torrents    │
 ┌──────────────┐  meta /   │  Seerr-emulation API     │◀────────└──────┬───────┘
 │     TMDB     ├──posters─▶│  notification center     │  mylist        │
 └──────────────┘           │  ┌────────────────────┐  │         rclone WebDAV
 ┌──────────────┐  add via  │  │ SQLite (truth)     │  │           mount
 │ Overseerr /  ├──Sonarr/──▶  └────────────────────┘  │◀── webdav ─────┘
 │ Jellyseerr   │  Radarr   │  symlink farm (movie+tv) │   (15-min refresh)
 └──────────────┘  v3 API   └────────────┬─────────────┘
                                          │ organized libraries
                                          ▼
                                  ┌──────────────┐
                                  │  Plex (scan) │
                                  └──────────────┘
```

**Component responsibilities**

- **TorBox client** — extend the existing `internal/torbox` client to cover torrents (`/torrents/*`), account/usage (`/user/me`), and torrent `mylist`, alongside the existing usenet methods.
- **Prowlarr client** — search and indexer listing.
- **Metadata client** — TMDB (series, seasons, episodes, movies, artwork).
- **Catalog/monitor** — what the user wants (series with monitored seasons/episodes; movies), persisted.
- **Selection** — pick the best release from Prowlarr results per a configurable score.
- **Grab pipeline** — fetch NZB/torrent → store → submit to TorBox → poll → resolve on WebDAV.
- **Importer** — organize completed releases into per-show/per-movie library folders (symlinks), trigger Plex scan.
- **Deleter** — remove library entry + symlink + delete on TorBox (reuse/extend existing deleter).
- **Healer** — re-grab when a previously-imported item's WebDAV target disappears (reuse existing healer; extend to torrents).
- **Reconciler** — 15-min sweep; detect unknown content; feed notification center.
- **Web API + UI** — JSON API backend and the split Series/Movies UI, WebDAV view, storage view, notification center, settings.
- **Seerr-emulation API** — Sonarr-flavored and Radarr-flavored v3 surfaces.

### 4.1 Stack (decided — §19)

- **Backend:** Go, **evolving the `sab2torbox` codebase** (reuse TorBox client, SQLite store, symlink farm, healer, deleter, rate-limit handling). The torrent flow mirrors the existing usenet flow, so this is mostly additive.
- **Frontend:** A separate **React + TypeScript** SPA built with **Vite** and **pnpm**, consuming the backend JSON API, **built to static assets and embedded in the Go binary via `embed.FS`** (no Node at runtime). React is chosen because it is the stack the implementing agent writes most reliably; the toolchain is hardened against npm supply-chain attacks (exact-pinned, vetted versions, lifecycle scripts disabled, lean dependency tree with no heavy UI libraries — see §17.2). *(Supersedes the earlier Svelte choice — §19.2.)*
- **Persistence:** SQLite (as today), schema-migrated with goose (as today).

*Everything outside §4.1 — functional requirements, data model, and external contracts — is language-agnostic and holds regardless of stack. The stack itself is now decided (§19): a Go backend evolving `sab2torbox`, and a React + TypeScript + Vite + pnpm frontend embedded in the binary.*

---

## 5. External System Contracts

### 5.1 TorBox (`https://api.torbox.app/v1/api`, Bearer token)

Response envelope is `{ success, error, detail, data }`. IDs may serialize as number **or** string (use the existing `FlexInt`). `progress` is a 0.0–1.0 float. Completion is signalled by `download_finished && download_present`; failure states are prefixed strings like `failed (...)`.

| Concern | Method | Endpoint | Notes |
|---|---|---|---|
| Submit usenet | POST | `/usenet/createusenetdownload` | NZB file or link. **60/hour** per token. *(already implemented)* |
| List usenet | GET | `/usenet/mylist?bypass_cache=true` | *(already implemented)* |
| Control usenet | POST | `/usenet/controlusenetdownload` | `operation: delete\|pause\|resume`. *(already implemented)* |
| Submit torrent | POST | `/torrents/createtorrent` | magnet **or** `.torrent` file; params `magnet`, `file`, `name`, `allow_zip`, `as_queued`. Precedence: hash > magnet > file. **300/min shared + 60/hour uncached.** |
| List torrents | GET | `/torrents/mylist` | Same shape as usenet mylist, different field names. |
| Control torrent | POST | `/torrents/controltorrent` | `operation: delete\|pause\|resume\|reannounce`. |
| Cached check | GET | `/torrents/checkcached?hash=…` | Lets the UI show "instant" vs "will download". |
| Account / usage | GET | `/user/me` | Plan tier, active-slot limit, monthly cooldown/usage. *(confirm exact field names against live API)* |

**WebDAV behavior:** TorBox writes one folder per completed download, named after the `mylist` `name`, directly under the mount root — a single flat namespace shared by movies, series, torrents, and usenet. Listing refreshes only every ~15 minutes; this can be forced via the `/refresh` endpoint on the WebDAV host (already implemented as the optional refresh in `sab2torbox`). *Verify whether torrent downloads surface under the same mount path as usenet or under a separate subpath; expose as config if they differ.*

### 5.2 Prowlarr (`http(s)://host:9696`, `X-Api-Key`)

| Concern | Method | Endpoint | Notes |
|---|---|---|---|
| Search | GET | `/api/v1/search?query=…&type=search&categories=…&indexerIds=…` | Returns releases with `title`, `downloadUrl`, `magnetUrl`, `indexer`, `indexerId`, `protocol` (`usenet`\|`torrent`), `size`, `seeders`, `leechers`, `grabs`, `categories`, `publishDate`. |
| Indexers | GET | `/api/v1/indexer` | Configured indexers, for filtering/diagnostics. |

**Grabbing the release:** for `protocol=usenet`, fetch `downloadUrl` to obtain the `.nzb` and submit it to TorBox (store locally first). For `protocol=torrent`, prefer `magnetUrl`; otherwise fetch `downloadUrl` to obtain the `.torrent` and submit it. Store the magnet/torrent/NZB locally so re-grabs (heals) don't depend on the indexer link still being live.

### 5.3 TMDB (metadata)

Series with seasons/episodes (air dates, episode titles), movies (release dates, runtime), and artwork (posters/backdrops). API key in config. Used to: build the catalog the user searches/monitors; drive the library UI's posters; produce Plex-friendly folder/file names; and decide which episodes a monitored series still needs.

### 5.4 Plex

The tool organizes content into standard Plex library folders (per-show/per-season for TV, per-movie for movies); Plex scans those folders and never sees the flat TorBox dir. Optionally trigger a Plex **partial scan** (`/library/sections/{id}/refresh?path=…`, `X-Plex-Token`) on new/changed content so it appears within seconds rather than waiting for Plex's own schedule.

### 5.5 Overseerr / Jellyseerr (inbound — this tool emulates Sonarr & Radarr)

See §10.

---

## 6. UI Requirements

- **FR-UI-1.** The UI is split into two primary sections: **Series** and **Movies**. Each presents a poster-based library of monitored/owned titles backed by TMDB artwork.
- **FR-UI-2.** A series detail view shows seasons and episodes with per-episode status: wanted / searching / downloading / available / expired/broken.
- **FR-UI-3.** A movie detail view shows status: wanted / searching / downloading / available / expired/broken.
- **FR-UI-4.** **Manual search** (FR-SR-*) is reachable from a title (search for a specific movie / season / episode) and as a free-text search, presenting a ranked, filterable release list the user picks from — equivalent to Sonarr/Radarr's interactive search.
- **FR-UI-5.** A **WebDAV view** (FR-WD-*) lists everything on the mount with category and size.
- **FR-UI-6.** A **Storage overview** (FR-ST-*).
- **FR-UI-7.** A **Notification center** (FR-NC-*).
- **FR-UI-8.** A **Settings** area for all connections and options (FR-CFG-1).
- **FR-UI-9.** Library and status data is read from the tool's own database, **not** by scanning the WebDAV mount on each page load.

---

## 7. Catalog & Metadata

- **FR-CAT-1.** Add a series by TMDB/TVDB id (via search, or via Seerr in §10). Persist series with seasons and episodes resolved from TMDB.
- **FR-CAT-2.** Add a movie by TMDB id (via search, or via Seerr). Persist the movie.
- **FR-CAT-3.** Per-series and per-season monitored flags determine which episodes are "wanted". Per-movie monitored flag likewise.
- **FR-CAT-4.** For monitored series, compute the set of wanted-but-missing episodes from the episode list minus what is present in the library. Air-date-aware (do not search for unaired episodes).
- **FR-CAT-5.** Metadata refresh on a schedule (e.g. daily) to pick up new episodes/seasons.

---

## 8. Search & Selection

- **FR-SR-1.** Manual search issues a Prowlarr `/api/v1/search` query scoped appropriately (movie title+year; series title + `SxxEyy` or season pack), returning **both** usenet and torrent releases.
- **FR-SR-2.** Results are presented with title, size, protocol, indexer, seeders/leechers (torrent) or grabs (usenet), detected quality, and cached status (torrents, via `checkcached`). The user can sort/filter and pick one to grab.
- **FR-SR-3.** Picking a release hands it to the grab pipeline (§9).
- **FR-SR-4.** (Phase 3+) Automatic search may run on add (Seerr `searchForMovie`/`searchForMissingEpisodes`) and optionally on a schedule for monitored, still-missing items. Manual search is the MVP; automatic is additive.
- **FR-SR-5 (selection score).** When a release must be chosen automatically, rank by a simple, configurable score: preferred resolution/quality, size within a configurable min/max per quality, protocol preference (e.g. prefer cached torrents, then usenet), seeders/grabs threshold, and optional preferred/blocked release-group or keyword lists. Keep it intentionally simple; do not build Sonarr's custom-format engine.

---

## 9. Grab Pipeline

- **FR-GP-1.** On grab, fetch and **store locally** the release artifact: the `.nzb` (usenet) or the magnet/`.torrent` (torrent). Persist it on the job record so heals can resubmit without the indexer.
- **FR-GP-2.** Submit to TorBox: usenet via `createusenetdownload` (existing flow); torrent via `createtorrent` (magnet preferred, else torrent file).
- **FR-GP-3.** A poller watches the relevant TorBox `mylist` (usenet and torrents) until `download_finished && download_present`, updating job progress/ETA/state in the DB. Reuse the existing usenet poller; add a parallel torrent poll.
- **FR-GP-4.** On completion, resolve the release folder on the WebDAV mount by its `name` (reuse the existing name→folder resolution, including the up-to-30s retry and optional `/refresh`).
- **FR-GP-5.** Deduplicate submissions (NZB SHA256 / magnet hash / URL, scoped to type+context) — return the existing job rather than double-submitting. Reuse the existing dedup approach.
- **FR-GP-6.** Respect TorBox limits (§13) — both `createusenetdownload` and `createtorrent` are 60/hour, and torrents add a 300/min shared ceiling; uncached torrents count against the stricter hourly cap.
- **FR-GP-7.** Handle TorBox failure states (`failed (...)`, stalled) by marking the job failed and surfacing it in the notification center; failed grabs may trigger a re-search (selection picks the next candidate).

---

## 10. Seerr Integration (emulate Sonarr & Radarr v3)

Overseerr/Jellyseerr connects to a Sonarr server (for TV) and a Radarr server (for movies) as **separate** servers, each with a hostname/port or URL base and an API key. This tool must expose **two** API surfaces — a Sonarr-flavored one and a Radarr-flavored one — distinguished by URL base (e.g. `/sonarr/api/v3/...` and `/radarr/api/v3/...`) or by separate ports.

- **FR-SEERR-1.** Authenticate requests via `X-Api-Key` header and `apikey` query param (Servarr convention), against configured keys.
- **FR-SEERR-2.** Implement the shared Servarr endpoints so Seerr's "Test" succeeds and its config dropdowns populate:

  | Endpoint | Returns |
  |---|---|
  | `GET /api/v3/system/status` | A realistic `{ appName, version, ... }` (Sonarr or Radarr respectively) so the connection test passes. |
  | `GET /api/v3/qualityprofile` | At least one profile `{ id, name }`. |
  | `GET /api/v3/rootfolder` | At least one `{ id, path, accessible: true, freeSpace }` — the tool's TV or movie library root. |
  | `GET /api/v3/tag` | Tags (may be `[]`). |
  | `GET /api/v3/languageprofile` *(Sonarr surface)* | At least one profile (older-Sonarr compatibility). |

- **FR-SEERR-3.** Support lookups Seerr may issue before adding: `GET /api/v3/series/lookup?term=tvdb:{id}` (Sonarr) and `GET /api/v3/movie/lookup?term=tmdb:{id}` (Radarr), returning a canonical object built from TMDB.
- **FR-SEERR-4 (add series).** `POST /api/v3/series` accepting at least `{ tvdbId, title, qualityProfileId, languageProfileId, rootFolderPath, monitored, seasonFolder, seasons[], addOptions{ searchForMissingEpisodes, monitor }, tags }`. Ingest into the catalog (§7) as a monitored series; honor `searchForMissingEpisodes` by kicking off search (§8). Respond with the created series object including an assigned `id`.
- **FR-SEERR-5 (add movie).** `POST /api/v3/movie` accepting at least `{ tmdbId, title, qualityProfileId, rootFolderPath, monitored, minimumAvailability, addOptions{ searchForMovie }, tags }`. Ingest as a monitored movie; honor `searchForMovie`. Respond with the created movie object including an assigned `id`.
- **FR-SEERR-6.** Responses must be schema-compatible enough that Seerr accepts them without error. Validate against Overseerr's `server/api/servarr/` client expectations and the Sonarr/Radarr v3 schema during implementation; adjust fields as needed.
- **FR-SEERR-7.** Availability back-sync to Seerr is **not** required here (Seerr derives availability from Plex). Only add + config endpoints are in scope.

---

## 11. Import & Library Organization

- **FR-IMP-1.** The tool is the source of truth for "release folder → (movie X | series Y, SxxEyy) → target library path". This mapping is recorded at submit time, so TorBox's single flat namespace is a non-issue for the tool's own downloads.
- **FR-IMP-2.** Movies import to a movies library root; series to a separate TV library root — both organized in standard Plex layout (`Movie (Year)/...`, `Series/Season NN/Series - SxxEyy - Title.ext`).
- **FR-IMP-3.** Import creates symlinks into the flat WebDAV release folder (symlink-farm pattern, extended to two library roots). Reuse the existing symlink-farm logic.
- **FR-IMP-4 (season packs).** When a completed release is a multi-episode/season pack, enumerate its files and map each to an episode. **Use an existing parse library (`guessit` or `parsett`); do not hand-roll regexes.** Each mapped episode gets its own library symlink.
- **FR-IMP-5.** After import, optionally trigger a Plex partial scan (§5.4) on the affected library path.
- **FR-IMP-6.** Imports must be a rename within one filesystem (see §11.1) — never a byte copy.

### 11.1 The rename constraint (critical, inherited from `sab2torbox`)

The symlink move into the library must be a rename within a single local filesystem, so `SYMLINK_ROOT` and each library root must share one filesystem. Every container that reads media (this tool for analysis, Plex) must mount the WebDAV path at the **same absolute path**, or symlinks dangle. Carry forward `sab2torbox`'s existing guarantees and cleanup (reaper for empty/orphaned dirs).

---

## 12. Deletion & Lifecycle

- **FR-DEL-1.** Deleting an episode or a movie in the UI removes its library symlink(s) **and** deletes the underlying download from TorBox via `controlusenetdownload`/`controltorrent` `operation=delete`. Deletion is handled directly by this tool against TorBox — not delegated. Reuse/extend the existing deleter (transient-failure retries, then drop with an error-level log).
- **FR-DEL-2.** Deleting a whole series/season deletes all corresponding episodes' downloads and symlinks.
- **FR-HEAL-1.** When a previously-imported item's WebDAV target disappears (TorBox rotated/expired it — detected as a broken symlink), re-acquire it. Reuse the existing healer (discover symlinks, detect broken, resubmit the stored artifact, repoint on completion, with backoff, max-attempts, and a manual-resolved escape hatch). **Extend the healer to cover torrents** (resubmit the stored magnet/torrent, not just NZBs).
- **FR-HEAL-2 (optional, confirm in §19).** Optionally re-grab via a fresh Prowlarr search instead of resubmitting the original artifact, so heals self-recover even when the original release is dead — at the cost of a possibly different filename (handled by the existing best-match repointing).

---

## 13. TorBox Limits Management

- **FR-LIM-1.** Reuse the existing 429/`Retry-After` handling (`RateLimit`, `Retryable`, `parseRetryAfter`) for all TorBox calls, including the new torrent endpoints.
- **FR-LIM-2.** Gate submissions with a concurrency limit so the number of active TorBox downloads stays within the plan's active-slot allowance; queue the rest.
- **FR-LIM-3.** Track the 60/hour `create*` caps (usenet and torrent) and the torrent 300/min shared ceiling; back off and queue when approached, rather than failing grabs.
- **FR-LIM-4.** Surface plan tier, active slots in use, and monthly usage/cooldown (from `/user/me`) in the storage overview and notification center.

---

## 14. WebDAV View & Storage Overview

- **FR-WD-1.** A view lists **every** item on the WebDAV mount (one row per release folder) with its name and total size.
- **FR-WD-2.** Each item is categorized **Movie / Series / Unknown**. Category comes from the tool's DB when the item was submitted by the tool; for items not in the DB, best-effort categorize by parsing the folder/file names (`guessit`/`parsett`).
- **FR-WD-3.** Items present on the mount but not known to the tool ("unknown") are flagged and raised in the notification center (§15) — this is the bridge to FR-NC-2.
- **FR-ST-1.** A storage overview shows total used size = sum of `size` across the usenet and torrent `mylist`s (the bytes currently on TorBox/WebDAV).
- **FR-ST-2.** Show used size against plan limits and monthly usage/cooldown from `/user/me`.
- **FR-ST-3.** Optionally break storage down by category (movies vs series) using the tool's mapping.

---

## 15. Reconciliation Poll & Notification Center

- **FR-NC-1 (15-min poll).** Every 15 minutes (matching TorBox's WebDAV refresh cadence; interval configurable), reconcile: list the WebDAV mount and the TorBox `mylist`s against the tool's known jobs/items.
- **FR-NC-2 (unknown content).** Any item present on the mount that the tool did not submit (and cannot match to a known job) raises a notification in the notification center, with its name, size, and best-effort category, plus actions (e.g. adopt/categorize, ignore, or delete from TorBox).
- **FR-NC-3 (event notifications).** The notification center also records: download completed, grab failed / TorBox failure state, heal triggered/succeeded/failed, deletion completed, and limit/cooldown reached.
- **FR-NC-4.** Notifications persist (DB), have read/unread state, and are listed newest-first in the UI. A badge count shows unread items.
- **FR-NC-5.** For the tool's own in-flight downloads, continue to use the forced `/refresh` (existing) for fast import; the 15-min sweep is for reconciliation and unknown-content detection, not the primary import path.

---

## 16. Data Model (logical — extend the existing schema)

Build on `sab2torbox`'s existing `jobs` and `imported_symlinks` tables. Add:

- **Series** — `id`, `tmdb_id`, `tvdb_id`, `title`, `year`, `monitored`, `quality_profile_id`, `root_folder_path`, metadata cache, timestamps.
- **Season** — `id`, `series_id`, `season_number`, `monitored`.
- **Episode** — `id`, `series_id`, `season_number`, `episode_number`, `title`, `air_date`, `status` (wanted/searching/downloading/available/broken), `job_id?`, `library_path?`.
- **Movie** — `id`, `tmdb_id`, `title`, `year`, `monitored`, `quality_profile_id`, `root_folder_path`, `status`, `job_id?`, `library_path?`, metadata cache.
- **Job (extend existing)** — add `protocol` (`usenet`|`torrent`), `media_type` (`movie`|`series`), `media_ref` (movie/episode id), torrent fields (magnet/torrent hash/torrent file), keep existing heal/symlink fields.
- **WebDAVItem (derived/cached)** — `name`, `size`, `category`, `known` (mapped to a job?), `last_seen`.
- **Notification** — `id`, `type`, `payload`, `created_at`, `read`.
- **Settings/connections** — TorBox token, Prowlarr URL+key, TMDB key, Plex URL+token, Seerr API keys, library roots, symlink root, intervals, selection-score config, limit config.

---

## 17. Non-Functional Requirements

- **NFR-1 (idempotency & retries).** All TorBox/Prowlarr/Plex interactions retry transient failures and are safe to re-run; dedup prevents double submission.
- **NFR-2 (persistence).** SQLite, WAL, single-writer, goose migrations (as today).
- **NFR-3 (observability).** Structured logging with levels; a `/healthz` that reflects DB reachability and TorBox token validity (extend existing); health surfaces for heal-failed and broken-symlink counts.
- **NFR-4 (security / local-first).** No third-party data egress beyond the configured services; API keys stored in config and never logged. Frontend dependency supply-chain controls in §17.2.
- **NFR-5 (config).** Twelve-factor-style env vars (consistent with existing `SAB2TORBOX_*`), plus a settings UI; sensible defaults.
- **NFR-6 (deployment).** Single container for the backend (plus the rclone WebDAV mount, as today); the frontend served by the backend or as static assets. Document the same bind-mount/propagation and uid requirements as `sab2torbox`. See §17.1 for CI and the image build.
- **NFR-7 (performance).** Poll intervals configurable; library/status views served from the DB; WebDAV listing is the expensive operation and is confined to the 15-min sweep plus targeted post-completion checks.

### 17.1 CI/CD & Release

Published as a Docker image by GitHub Actions, reusing the conventions already in `sab2torbox` (GHCR, multi-arch, semver tags). "Like Sonarr" here means a versioned, multi-architecture image published continuously to a registry with `latest` + semver tags — which the existing `ci.yml`/`release.yml` already do; the only new piece is a frontend build stage.

- **FR-CI-1 (CI on every PR and push to `main`).** Require to pass: backend format check, static analysis, lint, and tests with the race detector and coverage; frontend lint + production build (`pnpm install --frozen-lockfile`, then build); and a Docker image build **without push** to prove the image still builds. Matches the existing `ci.yml` (`gofmt -s -l`, `go vet ./...`, `golangci-lint`, `go test ./... -race -cover`).
- **FR-CI-2 (release / publish).** On push to `main` and on `v*` tags, build multi-arch (`linux/amd64,linux/arm64`) and push to `ghcr.io/radaiko/boxarr`, authenticating with the built-in `GITHUB_TOKEN` (permissions `contents: read`, `packages: write`). Tag via `docker/metadata-action`: `latest` on the default branch, plus `v{version}` and `v{major}.{minor}` from semver tags; inject the version into the binary at build time. Matches the existing `release.yml`.
- **FR-CI-3 (multi-stage Dockerfile).** Stage 1 builds the frontend with **pnpm** (pinned via corepack / the `packageManager` field), installing with `--frozen-lockfile` and `ignore-scripts` (React + Vite → static assets; see §17.2). Stage 2 builds the **Go** backend and **embeds the built frontend into the binary** (`embed.FS`) so one process serves API + UI. Stage 3 is the runtime: `gcr.io/distroless/static-debian12:nonroot`, `CGO_ENABLED=0`, cross-compiled via `TARGETOS`/`TARGETARCH`, running as `nonroot`, with `EXPOSE` of the API port and a `HEALTHCHECK` calling the binary's `healthcheck` subcommand. Extends the existing `deploy/Dockerfile` with the frontend stage.
- **FR-CI-4 (single self-contained image).** No separate frontend container — the SPA is embedded and served by the backend: one image, one container (plus the rclone WebDAV mount, as in `sab2torbox`).
- **FR-CI-5 (provenance).** Apply OCI labels from `docker/metadata-action`; pin action major versions.
- **FR-CI-6 (supply-chain enforcement in CI).** CI installs frontend dependencies with `pnpm install --frozen-lockfile` and `ignore-scripts`, fails on any lockfile drift, and runs `pnpm audit` as an advisory gate. See §17.2 for the full dependency policy.

### 17.2 Frontend supply-chain hardening

The frontend toolchain is chosen and configured to minimize npm supply-chain exposure: **React + TypeScript** built with **Vite**, managed with **pnpm** (strict isolation, no phantom hoisting, content-addressable store, integrity-checked lockfile). The hardening below is framework-independent; the lean-dependency discipline (FR-SEC-4) keeps React's transitive tree small. *(Concrete `.npmrc`/`package.json`/Renovate config: `docs/specs/07-frontend.md` + `docs/specs/08-config-deploy-ci.md`.)*

- **FR-SEC-1 (exact pinning + committed lockfile).** All dependencies are pinned to exact versions — no `^`/`~` ranges (`save-exact`). `pnpm-lock.yaml` is committed and authoritative; every install (CI and image build) uses `--frozen-lockfile` and fails on drift.
- **FR-SEC-2 (vetted, not bleeding-edge).** Pin to versions that have been published long enough to be vetted (a cooldown of a few weeks) and are widely adopted, while staying within maintained release lines. Do **not** pin so far back that known-vulnerable versions are used — security patches are applied deliberately after review, never automatically.
- **FR-SEC-3 (no install scripts by default).** Disable lifecycle/`postinstall` scripts globally (`.npmrc` `ignore-scripts=true`); permit build scripts only for an explicit, reviewed allowlist (pnpm `onlyBuiltDependencies`). This closes the most common npm malware vector.
- **FR-SEC-4 (minimal surface).** Keep the dependency count low — lean on React + the Vite toolchain built-ins; avoid heavy UI/component libraries with deep transitive trees, and keep client-side routing/state minimal. Each added dependency is a deliberate, reviewed decision.
- **FR-SEC-5 (no runtime third-party fetch).** All JS/CSS is built at build time and embedded into the binary (§17.1); the running app loads no third-party scripts or styles from CDNs.
- **FR-SEC-6 (pinned package manager).** Pin the pnpm version itself via the `packageManager` field (corepack) so CI and local builds use an identical, known toolchain.
- **FR-SEC-7 (controlled updates).** Dependency bumps go through reviewed PRs with a minimum release age (e.g. Renovate `minimumReleaseAge` / Dependabot cooldown) so freshly published — possibly compromised — versions are never pulled automatically.

---

## 18. Suggested Implementation Phases

Front-load end-to-end value; defer the hardest parsing and optional automation.

- **Phase 0 — Foundation.** Extend the TorBox client (torrents, `/user/me`, torrent mylist/control); extend the schema; Prowlarr client; TMDB client; backend JSON API skeleton + UI shell + settings; CI + release workflows and the multi-stage Dockerfile (§17.1) so images publish from the first commit.
- **Phase 1 — Movies MVP (end-to-end).** Movie catalog (TMDB) → manual search (Prowlarr, usenet+torrent) → grab (both protocols) → submit → poll → resolve on WebDAV → import to movie library (symlink) → Plex scan → deletion (propagates to TorBox) → storage overview → WebDAV view. Movies skip episode/pack parsing, so this proves the whole spine fastest.
- **Phase 2 — Series.** Series/season/episode catalog; per-episode and season-pack search; pack unpack + per-file episode mapping (`guessit`/`parsett`); TV library import; the split UI.
- **Phase 3 — Seerr.** Sonarr + Radarr v3 emulation surfaces; search-on-add.
- **Phase 4 — Lifecycle & notifications.** 15-min reconciliation sweep; unknown-content detection; notification center; extend the healer to torrents.
- **Phase 5 — Automation (optional).** Scheduled/RSS monitoring for ongoing series.

---

## 19. Open Decisions (confirm before/early in implementation)

1. **Backend language & repo strategy — DECIDED.** Go, evolving the `sab2torbox` repo. It reuses the proven TorBox pipeline (client, store, symlink farm, healer, deleter), fits the concurrent-workers + HTTP-service workload, and matches the existing CI/distroless deployment — and is well within the implementation agent's strengths.
2. **Frontend — DECIDED (revised).** **React + TypeScript + Vite + pnpm**, static-built and embedded in the Go binary, with exact-pinned, vetted dependencies hardened against supply-chain attacks (§17.2). *(Revised from the earlier Svelte choice: the primary factor is the implementing agent's authoring reliability; all §17.2 controls are framework-independent. See `docs/specs/00-decisions-and-assumptions.md` §2.)*
3. **Metadata provider — DECIDED.** **TMDB (primary) + TVDB (supplement).** TMDB drives the catalog/posters/movie+TV; TVDB supplies scene/absolute episode ordering and the TVDB id Seerr's Sonarr surface keys on (`docs/specs/03-external-contracts.md`).
4. **Heal strategy (FR-HEAL-2) — DECIDED.** **Resubmit the stored artifact AND fall back to a fresh Prowlarr re-search** when the original is dead (best-match repoint). Gated by `BOXARR_HEAL_PROWLARR_FALLBACK` (default on) (`docs/specs/06-pipelines.md` §8.2).
5. **Torrent WebDAV path — empirical (runtime-verified).** Default assumption: torrents surface under the **same flat mount root** as usenet; configurable via `BOXARR_WEBDAV_TORRENT_SUBPATH` (default empty) if the live mount differs. Verify on the live mount (`docs/specs/00-decisions-and-assumptions.md` §9 register).
6. **Project name — DECIDED.** Boxarr (`github.com/radaiko/boxarr`, `ghcr.io/radaiko/boxarr`).
