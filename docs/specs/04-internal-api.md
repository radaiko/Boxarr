# Boxarr — Internal REST API: `/api/v1` for the React SPA (Spec 04)

**Date:** 2026-06-20
**Status:** Approved for planning

This spec owns Boxarr's **own JSON/REST API**, served under `/api/v1` by `internal/api/v1` (placed in the package map by `01-architecture-and-packages.md` §5) and consumed exclusively by the embedded React+TS+Vite SPA (`07-frontend.md`). It is **not** the Overseerr/Jellyseerr inbound emulation — that is the Sonarr v3 / Radarr v3 surface owned by `05-seerr-emulation.md` under `/sonarr/api/v3` and `/radarr/api/v3`. The two never overlap: `/api/v1` is the *outbound* contract the SPA calls; `05` is the *inbound* contract Seerr calls.

Cross-cutting decisions, naming, the HTTP-namespace map, and the runtime-verify register are fixed in `00-decisions-and-assumptions.md` and are **not** re-decided here. Entity and field names are taken verbatim from `02-data-model.md`; the behaviors that a grab/search/delete/heal endpoint *triggers* are owned by `06-pipelines.md`; the external contracts the data is sourced from (TorBox, Prowlarr, TMDB, TVDB, Plex) are owned by `03-external-contracts.md`. This document defines the **HTTP shapes** only: methods, paths, query/body JSON, response JSON, and status codes.

---

## 1. Conventions (apply to every `/api/v1` endpoint)

- **Base path:** `/api/v1`. Mounted via `r.Mount("/api/v1", s.v1.Router())` (verified `01-architecture-and-packages.md` §5), **before** the catch-all SPA route so the SPA never shadows the API.
- **Transport:** JSON over HTTP/1.1. Every request/response body is `Content-Type: application/json; charset=utf-8` (write path reuses the baseline `writeJSON`, verified `/tmp/sab2torbox/internal/api/handlers.go:70-77`, which sets the header and `json.NewEncoder(w).Encode(v)`). Mutating verbs that take no body (toggle, mark-read) accept an empty body.
- **Auth (locked).** Reuse the baseline **constant-time api-key check** verbatim — `validAPIKey(got, want)` wrapping `subtle.ConstantTimeCompare` (verified `handlers.go:78-81`), re-pointed from the deleted `SAB_API_KEY` (`00` Assumption B) to a new **`BOXARR_API_KEY`** (`08-config-deploy-ci.md`). The key is read from the **`X-Api-Key` header** (decided — header-only for the SPA, to keep the key out of access logs, matching the Prowlarr recommendation `06-ext-prowlarr-api.md` Rec 1). **Localhost default:** when `BOXARR_API_KEY` is empty *and* the request's `RemoteAddr` is loopback (`127.0.0.0/8` or `::1`), the request is allowed unauthenticated — the single-operator local-first default (requirements §3, NFR-4). A non-empty key is **always** required for non-loopback clients; a non-empty key set also requires loopback requests to present it (no localhost bypass once a key exists). This bypass is **never** applied to `/sonarr`/`/radarr` (those use their own Servarr `apikey`-query auth, `05`).
- **Auth middleware.** A single chi middleware fronts the whole `/api/v1` subrouter; on failure it returns **`401`** with the error envelope `{"error":{"code":"unauthorized","message":"invalid or missing api key"}}` and **does not** leak which check failed. `/healthz` stays outside `/api/v1` and stays unauthenticated (verified `01` §5 namespace table).
- **Error envelope (locked — distinct from the dropped SAB `{status,error}` shape).** Every non-2xx response carries:
  ```json
  { "error": { "code": "snake_case_machine_code", "message": "human readable", "details": { "field": "optional context" } } }
  ```
  `code` is a stable machine token (table §1.1); `message` is for the UI toast; `details` is optional. **`2xx` responses never include an `error` key.** This intentionally diverges from the SAB envelope (`{success,error,detail,data}`) which is **dropped with the SAB surface** (`00` Assumption B) — that envelope remains the shape of the *upstream TorBox* client (`03`), not Boxarr's own API.
- **Success envelope.** **List endpoints** wrap results: `{ "items": [...], "total": <int>, "limit": <int>, "offset": <int> }`. **Single-resource** endpoints return the bare object (no wrapper). **Mutations** return the mutated resource (or `204 No Content` where there is nothing useful to return, noted per-endpoint).
- **Pagination (locked).** Query params `limit` (default `50`, max `200`) and `offset` (default `0`). `total` in the list envelope is the unfiltered-by-page count for the active filter set. Library grids that always render everything from the DB (FR-UI-9) MAY pass `limit=0` to mean "no page cap" (server clamps to a hard ceiling of `2000`).
- **Sorting (locked).** Query param `sort=<field>` and `order=asc|desc` (default `asc`). Each list endpoint documents its allowed `sort` fields; an unknown field is a `400 bad_request`. Default sorts mirror the store's `ORDER BY` (`02` §5: series/movies by `sort_title`, notifications by `created_at DESC,id DESC`, webdav items by `name`).
- **Filtering (locked).** Filters are explicit query params per endpoint (e.g. `monitored=true`, `status=wanted`, `category=movie`, `protocol=torrent`, `q=<text>`). Booleans accept `true|false`; enums must be a documented value or the call is `400 bad_request` with `details.field`.
- **Timestamps (locked).** All timestamps in/out are **RFC 3339 / ISO 8601 UTC with a `Z` suffix** (e.g. `2026-06-20T10:00:00Z`) — the same string format TorBox and TMDB already emit (`03`). **Calendar dates** (air dates, release dates) are bare `"YYYY-MM-DD"` strings, passed through verbatim from the catalog columns (`02` §3.2 stores them as `TEXT 'YYYY-MM-DD'`); the API never reformats them.
- **Sizes** are **bytes** as JSON integers (matches TorBox `size`, mylist `total_bytes`); the SPA formats for display. **Progress** is an integer **percent 0–100** (matches `jobs.progress_pct`, `02` §1.1), not a 0.0–1.0 float (that float is the upstream TorBox `progress`, converted at the worker boundary, `06`).
- **IDs** are Boxarr's own SQLite integer primary keys (`series.id`, `movie.id`, `episode.id`, `season.id`, `notification.id`, `webdav_item.id`) unless a field name says otherwise (`tmdbId`, `tvdbId`, `torboxId`). JSON keys are **camelCase** (SPA-idiomatic; the Go structs use `json:"camelCase"` tags). **Date columns keep their snake source only internally** — the wire is camelCase throughout.
- **Concurrency / idempotency (NFR-1).** All mutations are safe to retry. Grabs dedup at the store boundary (`FindByTorrentHash`/`FindBySHA256`/`FindByURL`, `02` §5.1) so a double-clicked "grab" returns the existing job rather than double-submitting (FR-GP-5). Toggling monitored to its current value is a no-op `200`.
- **Not-found.** Any `GET`/mutation on a missing id returns **`404`** `{"error":{"code":"not_found",...}}`.

### 1.1 Error codes (stable machine tokens)

| `code` | HTTP | Meaning |
|---|---|---|
| `unauthorized` | 401 | Missing/invalid `X-Api-Key` (and not a permitted loopback request). |
| `bad_request` | 400 | Malformed body, unknown enum/sort field, missing required field (`details.field`). |
| `not_found` | 404 | No resource for the given id. |
| `conflict` | 409 | Resource already exists (e.g. add a title already in the catalog — returns existing id in `details`). |
| `upstream_unavailable` | 502 | A required dependency (Prowlarr/TMDB/TVDB/Plex/TorBox) failed or timed out; `details.service` names it. |
| `rate_limited` | 429 | An upstream returned 429 (`details.service`, `details.retryAfterSeconds`) — surfaced, not retried in-band (TorBox `ACTIVE_LIMIT`/`COOLDOWN_LIMIT`/`MONTHLY_LIMIT`, `05-ext-torbox-api.md`). |
| `unprocessable` | 422 | Valid JSON but semantically rejected (e.g. grab a release with no usable `magnetUrl`/`downloadUrl`). |
| `internal` | 500 | Unexpected server error; `message` is generic, detail is logged with `slog` not returned (NFR-4 — never leak internals). |

---

## 2. Endpoint catalog overview

| Group | Prefix | Owns (requirements) |
|---|---|---|
| Series | `/api/v1/series` | FR-UI-1/2, FR-CAT-1/3/4, FR-SR-1/2/3 (TV side) |
| Movies | `/api/v1/movies` | FR-UI-1/3, FR-CAT-2/3, FR-SR-1/2/3 (movie side) |
| Search | `/api/v1/search` | FR-SR-1/2 free-text Prowlarr search + grab |
| Catalog lookup | `/api/v1/lookup` | FR-CAT-1/2 TMDB/TVDB search to add new titles |
| WebDAV | `/api/v1/webdav` | FR-WD-1/2/3 mount listing |
| Storage | `/api/v1/storage` | FR-ST-1/2/3, FR-LIM-4 |
| Notifications | `/api/v1/notifications` | FR-NC-2/3/4, FR-WD-3 |
| Settings | `/api/v1/settings` | FR-UI-8, FR-CFG-1, NFR-5 |
| System | `/api/v1/status` (+ recap of `/healthz`) | NFR-3 |

**All paths below are relative to `/api/v1`.** All require auth per §1 unless noted.

---

## 3. Series

The series library reads entirely from Boxarr's DB (`series`/`season`/`episode`, `02` §3.2) — never from a live WebDAV scan (FR-UI-9). The per-item `status` is the denormalized `MediaStatus` (`02` §2.2: `wanted|searching|downloading|available|missing|expired_broken`); season and series **roll-ups are computed in this API layer** (`02` §2.2 — "seasons/series derive their roll-up in the API layer, not in a column").

### 3.1 Endpoints

| # | Method | Path | Purpose |
|---|---|---|---|
| S1 | GET | `/series` | Poster-grid list of monitored/owned series. |
| S2 | GET | `/series/{id}` | Series detail: seasons + episodes + per-item status. |
| S3 | POST | `/series` | Add a series from a lookup result (TMDB/TVDB id). |
| S4 | PUT | `/series/{id}/monitored` | Toggle series-level monitored. |
| S5 | PUT | `/series/{id}/seasons/{seasonNumber}/monitored` | Toggle a season's monitored flag. |
| S6 | PUT | `/series/{id}/episodes/{episodeId}/monitored` | Toggle an episode's monitored flag. |
| S7 | DELETE | `/series/{id}` | Delete the whole series (cascades downloads + symlinks). |
| S8 | DELETE | `/series/{id}/seasons/{seasonNumber}` | Delete one season's content. |
| S9 | DELETE | `/series/{id}/episodes/{episodeId}` | Delete one episode's content. |
| S10 | GET | `/series/{id}/search` | Manual search for the **series** (full/latest season scope). |
| S11 | GET | `/series/{id}/seasons/{seasonNumber}/search` | Manual search for a **season** (pack). |
| S12 | GET | `/series/{id}/episodes/{episodeId}/search` | Manual search for a single **episode**. |
| S13 | POST | `/series/{id}/grab` | Grab a chosen release for series/season/episode scope. |
| S14 | POST | `/series/{id}/refresh` | Force a TMDB/TVDB metadata refresh for this series. |

**Query params (S1):** `monitored` (bool), `status` (any `MediaStatus`, matches the series roll-up), `q` (title substring), `sort` ∈ `{sortTitle,title,year,addedAt,status}` (default `sortTitle`), `order`, `limit`, `offset`.

### 3.2 List item (S1) — JSON

The grid needs only what a poster card renders. `posterPath` is the **relative TMDB path** (`02` §3.2 stores `poster_path`); the SPA reconstructs the URL using the `/configuration` base+size the settings endpoint exposes (`03` TMDB / §10 here) — **never store the full URL** (`07-ext-tmdb-api.md` key fact). `status` is the series roll-up (§3.4).

```json
{
  "items": [
    {
      "id": 12,
      "tmdbId": 1399,
      "tvdbId": 121361,
      "title": "Game of Thrones",
      "year": 2011,
      "monitored": true,
      "status": "available",
      "seriesType": "standard",
      "posterPath": "/1XS1oqL89opfnbLl8WnZY1O1uJx.jpg",
      "episodeCount": 73,
      "episodeFileCount": 73,
      "seasonCount": 8,
      "addedAt": "2026-06-01T12:00:00Z"
    }
  ],
  "total": 1, "limit": 50, "offset": 0
}
```

### 3.3 Series detail (S2) — representative JSON (heaviest series response)

Joins `series` + its `season[]` + each season's `episode[]` (`02` store methods `GetSeries`/`ListSeasons`/`ListEpisodes`, §5.2). Each season carries a computed roll-up; each episode carries its stored `status`, `hasFile`, `monitored`, and `airDate`. The optional `linkedJob` block is present only when an episode has an in-flight or recent grab (`episode.job_id`, joined via `FindJobByMedia`/`GetJob`) so the UI can show live progress without a second call.

```json
{
  "id": 12,
  "tmdbId": 1399,
  "tvdbId": 121361,
  "imdbId": "tt0944947",
  "title": "Game of Thrones",
  "sortTitle": "game of thrones",
  "year": 2011,
  "overview": "Seven noble families fight for control of the mythical land of Westeros.",
  "seriesType": "standard",
  "tmdbStatus": "Ended",
  "monitored": true,
  "seasonFolder": true,
  "qualityProfileId": 1,
  "rootFolderPath": "/data/tv",
  "libraryPath": "/data/tv/Game of Thrones (2011)",
  "posterPath": "/1XS1oqL89opfnbLl8WnZY1O1uJx.jpg",
  "backdropPath": "/qsD5OHqW7DSnaQ2afwz8Ptl991q.jpg",
  "status": "available",
  "lastMetadataSync": "2026-06-20T03:00:00Z",
  "addedAt": "2026-06-01T12:00:00Z",
  "statistics": {
    "seasonCount": 8,
    "episodeCount": 73,
    "episodeFileCount": 71,
    "monitoredEpisodeCount": 73,
    "sizeOnDisk": 0
  },
  "seasons": [
    {
      "seasonNumber": 1,
      "monitored": true,
      "episodeCount": 10,
      "episodeFileCount": 10,
      "airDate": "2011-04-17",
      "posterPath": "/zwaj4egrhnXOBIit1tyeQAE3cF.jpg",
      "status": "available",
      "episodes": [
        {
          "id": 4501,
          "episodeNumber": 1,
          "seasonNumber": 1,
          "absoluteNumber": 1,
          "tmdbId": 63056,
          "tvdbId": 3254641,
          "title": "Winter Is Coming",
          "overview": "Jon Arryn, the Hand of the King, is dead.",
          "airDate": "2011-04-17",
          "runtime": 62,
          "stillPath": "/9hGF3WUkBf7cSjMg0cdMDHJkByd.jpg",
          "status": "available",
          "monitored": true,
          "hasFile": true,
          "libraryPath": "/data/tv/Game of Thrones (2011)/Season 01/Game of Thrones - S01E01 - Winter Is Coming.mkv",
          "linkedJob": null
        },
        {
          "id": 4555,
          "episodeNumber": 6,
          "seasonNumber": 1,
          "absoluteNumber": 6,
          "title": "A Golden Crown",
          "airDate": "2011-05-22",
          "status": "downloading",
          "monitored": true,
          "hasFile": false,
          "libraryPath": null,
          "linkedJob": {
            "id": 980,
            "state": "downloading",
            "protocol": "torrent",
            "progressPct": 73,
            "etaSeconds": 120,
            "downloadedBytes": 783741824,
            "totalBytes": 1073741824
          }
        }
      ]
    }
  ]
}
```

### 3.4 Roll-up rule (locked — computed here, not stored)

Season `status` and series `status` are derived from their episodes' `MediaStatus`, evaluated over **monitored** episodes only (unmonitored episodes never make a series look "wanted"). **Precedence (first match wins):** `downloading` (any monitored child downloading) → `searching` → `expired_broken` → `wanted` → `available` (all monitored children available) → `missing` (default, includes "all unmonitored" and "monitored but unaired"). A series with **no** monitored episodes rolls up to `missing`. Empty seasons (no episodes yet from TMDB) roll up to `missing`. This is a pure function of the joined rows; it is recomputed per request (cheap — the DB query already loaded them).

### 3.5 Add series (S3) — request/response

Body comes from a `/lookup` result (§7). `tmdbId` is the discovery key (catalog identity, UNIQUE `02` §3.2); `tvdbId` is resolved during ingest if absent (required for Sonarr emulation, `02`/`05`). Ingest fans out to TMDB `/tv/{id}?append_to_response=external_ids` + per-season `/tv/{id}/season/{n}` (`07-ext-tmdb-api.md` Rec (a)) — owned by `06-pipelines.md` (catalog ingest). `searchOnAdd` honors FR-SR-4 (optional auto-search) by enqueuing wanted episodes into the grab orchestrator.

```json
// POST /series  (request)
{
  "tmdbId": 1399,
  "tvdbId": 121361,
  "monitored": true,
  "qualityProfileId": 1,
  "rootFolderPath": "/data/tv",
  "seriesType": "standard",
  "seasonFolder": true,
  "monitoredSeasons": [1, 2, 3],        // optional; omitted = monitor all aired seasons
  "searchOnAdd": false                   // optional; default false (MVP is manual search, FR-SR-4)
}
```

- **`201 Created`** with the full §3.3 detail body of the newly ingested series (so the SPA can route straight to the detail view).
- **`409 conflict`** if `tmdbId` already in the catalog (the `idx_series_tmdb` UNIQUE guard, `02` §3.2 quirk 1): `{"error":{"code":"conflict","message":"series already in catalog","details":{"id":12}}}`.
- **`502 upstream_unavailable`** `details.service:"tmdb"` if metadata fetch fails.
- **`400 bad_request`** missing `tmdbId` or unknown `rootFolderPath`/`qualityProfileId`.

### 3.6 Monitored toggles (S4/S5/S6)

Body `{ "monitored": true }`. Returns `200` with the affected resource's minimal projection `{ "id": ..., "monitored": ... }` (series) or `{ "seasonNumber": ..., "monitored": ... }` / `{ "id": ..., "monitored": ... }` (episode). Toggling a **season** cascades the flag to its episodes' `monitored` default unless an episode was individually overridden (store: `SetSeasonMonitored` + the catalog recompute, `02` §5.2). Setting monitored re-evaluates `MediaStatus` (eligible+monitored+no file ⇒ `wanted`) — that recompute is `06`'s catalog promoter; the API just persists the flag and returns.

### 3.7 Deletes (S7/S8/S9) — propagate to TorBox

Per FR-DEL-1/2: deleting an episode removes its **library symlink(s)** and **deletes the underlying download from TorBox** via `controlusenetdownload`/`controltorrent` `operation=delete`, branching on `jobs.protocol` (verified deleter reuse, `01` §4 deleter row). Series/season deletes fan out to every contained episode's job. **This API does not block on TorBox** — it marks the linked job(s) `deleted` (the existing deleter worker drains them with retry, exactly as the SAB `handleDelete` set state `deleted`, verified `04-sab-api-config-ci.md` §2) and removes the catalog rows (CASCADE for series→season→episode, `02` §3.2). 

- **S7 `DELETE /series/{id}`** query `?deleteFiles=true` (default `true`). `204 No Content` on success. With `deleteFiles=false`, only the catalog rows + symlinks go; TorBox content is left (becomes "unknown" on next reconcile, raising a notification, FR-WD-3).
- **S8/S9** same `?deleteFiles` semantics, scoped to season/episode; `204 No Content`.
- The deletion-completed event is recorded as a `deletion_completed` notification (`02` §3.3, FR-NC-3) by the deleter worker, not synchronously here.

### 3.8 Manual search (S10/S11/S12) — ranked releases

Issues a Prowlarr `/api/v1/search` (`06-ext-prowlarr-api.md`): `type=tvsearch`, `categories=5000` (TV parent, catches subcats; `+5070` for anime when `seriesType=anime`), `indexerIds=-1` (all), repeated-key encoding (NOT comma-separated — `06-ext-prowlarr-api.md` key fact). The `query` is built by the selection/search layer (`06`): series→`{Series Title}`, season→`{Series Title} S{NN}` (pack), episode→`{Series Title} S{NN}E{MM}` (Newznab `{TvdbId:X} {Season:Y}` syntax where the indexer supports it, `06-ext-prowlarr-api.md` examples). Results are **scored and ranked** by `internal/selection` per the configurable score (`06`, FR-SR-5) and **cached-checked** for torrents via TorBox `checkcached` (`05-ext-torbox-api.md`) so the UI shows instant vs will-download.

These are **synchronous reads** (may take seconds — Prowlarr aggregates indexers). Query params: `sort` ∈ `{score,seeders,size,age,grabs}` (default `score`), `order`, `protocol` (`usenet|torrent` filter), `cachedOnly` (bool). Response is the **Release list** shape (§3.9). On a Prowlarr failure: `502 upstream_unavailable` `details.service:"prowlarr"`; Prowlarr itself returns `[]` (not an error) when individual indexers fail (`06-ext-prowlarr-api.md` key fact), so an **empty** `items` array is a valid `200`.

### 3.9 Release list — representative JSON (heaviest search response; shared by S10–S12, M9, Search)

One row per Prowlarr `ReleaseResource` (`06-ext-prowlarr-api.md`), enriched with Boxarr's `score`, parsed `quality`, and (torrent) `cached`. `releaseId` is an **opaque server token** Boxarr mints and caches for the grab call (it carries `indexerId`+`guid`+`protocol`+chosen URL so the grab need not re-search; mirrors Prowlarr's own 30-min `IndexerId_Guid` cache, `06-ext-prowlarr-api.md` key fact). `seeders`/`leechers` are torrent-only; `grabs` is the usenet/torznab equivalent. `quality` is parsed by `internal/importer/release` (`06`). `cached` is `null` for usenet, `true|false` for torrents (from `checkcached`).

```json
{
  "items": [
    {
      "releaseId": "rel_8f3a1c…",
      "title": "Game.of.Thrones.S01E01.1080p.BluRay.x265-RARBG",
      "indexer": "1337x",
      "indexerId": 3,
      "protocol": "torrent",
      "size": 2147483648,
      "quality": "Bluray-1080p",
      "seeders": 142,
      "leechers": 7,
      "grabs": null,
      "cached": true,
      "freeleech": false,
      "indexerFlags": ["scene"],
      "publishDate": "2025-03-12T08:00:00Z",
      "score": 1850,
      "rejected": false,
      "rejectionReason": null,
      "hasMagnet": true,
      "categories": [5040]
    },
    {
      "releaseId": "rel_2b9e77…",
      "title": "Game of Thrones S01E01 1080p WEB-DL DD5.1 H264-NTb",
      "indexer": "DrunkenSlug",
      "indexerId": 5,
      "protocol": "usenet",
      "size": 1825361100,
      "quality": "WEBDL-1080p",
      "seeders": null,
      "leechers": null,
      "grabs": 312,
      "cached": null,
      "freeleech": false,
      "indexerFlags": [],
      "publishDate": "2025-03-12T06:30:00Z",
      "score": 1600,
      "rejected": false,
      "rejectionReason": null,
      "hasMagnet": false,
      "categories": [5040]
    }
  ],
  "total": 2, "limit": 50, "offset": 0
}
```

`rejected`/`rejectionReason` flag releases that fail a hard filter (e.g. below the seeders threshold, size out of the per-quality min/max, blocked release group — FR-SR-5); they are still returned (greyed in the UI) so the user can override, matching Sonarr's interactive search.

### 3.10 Grab (S13) — hand a chosen release to the pipeline

`POST /series/{id}/grab` body `{ "releaseId": "rel_8f3a1c…", "scope": "episode", "episodeId": 4501 }` (or `"scope":"season","seasonNumber":1`, or `"scope":"series"`). The server resolves the cached release, **stores the artifact locally** (NZB bytes / magnet / `.torrent`, FR-GP-1), **dedups** (`FindByTorrentHash`/`FindBySHA256`/`FindByURL`, `02` §5.1, FR-GP-5), creates a `pending` job with `(media_type, media_ref)` pointing at the scoped catalog row (`02` §3.1 polymorphic ref) and `protocol` set, and flips the target episode(s) `status→searching` (`06` grab pipeline owns the state machine and submission). 

- **`202 Accepted`** `{ "jobId": 980, "state": "pending", "deduped": false }` — accepted means *enqueued*, not *completed* (submission/poll are async workers). `deduped:true` means an existing job was returned (FR-GP-5).
- **`422 unprocessable`** if the chosen release has no usable URL (no `magnetUrl` and `downloadUrl` fetch failed; `06-ext-prowlarr-api.md` URL-selection tree).
- **`404 not_found`** if `releaseId` expired from cache (UI must re-search) or the episode id is unknown.

### 3.11 Refresh (S14)

`POST /series/{id}/refresh` → `202 Accepted` `{ "queued": true }`; enqueues a targeted metadata refresh (TMDB re-fetch + new-episode discovery, FR-CAT-5) on the metadata-refresh worker (`01` §4). Returns immediately; the SPA re-fetches detail after.

---

## 4. Movies

Movies mirror series minus the season/episode tree (Phase 1 spine, `00`/requirements §18). Reads from `movie` (`02` §3.2); `status` is the stored `MediaStatus`. No roll-up — a movie *is* its leaf.

### 4.1 Endpoints

| # | Method | Path | Purpose |
|---|---|---|---|
| M1 | GET | `/movies` | Poster-grid list. |
| M2 | GET | `/movies/{id}` | Movie detail + status + linked job. |
| M3 | POST | `/movies` | Add a movie from a lookup result (TMDB id). |
| M4 | PUT | `/movies/{id}/monitored` | Toggle monitored. |
| M5 | DELETE | `/movies/{id}` | Delete (propagates to TorBox). |
| M6 | GET | `/movies/{id}/search` | Manual search for this movie → ranked releases. |
| M7 | POST | `/movies/{id}/grab` | Grab a chosen release. |
| M8 | POST | `/movies/{id}/refresh` | Force TMDB metadata refresh. |

**Query params (M1):** `monitored`, `status` (`MediaStatus`), `q`, `sort` ∈ `{sortTitle,title,year,addedAt,status}` (default `sortTitle`), `order`, `limit`, `offset`.

### 4.2 List item (M1) — JSON

```json
{
  "items": [
    {
      "id": 7,
      "tmdbId": 11,
      "imdbId": "tt0076759",
      "title": "Star Wars",
      "year": 1977,
      "monitored": true,
      "status": "available",
      "hasFile": true,
      "posterPath": "/6FfCtAuVAW8XJjZ7eWeLibRLWTw.jpg",
      "addedAt": "2026-06-02T09:00:00Z"
    }
  ],
  "total": 1, "limit": 50, "offset": 0
}
```

### 4.3 Movie detail (M2) — JSON

```json
{
  "id": 7,
  "tmdbId": 11,
  "imdbId": "tt0076759",
  "title": "Star Wars",
  "sortTitle": "star wars",
  "year": 1977,
  "overview": "Princess Leia is captured and held hostage…",
  "tmdbStatus": "Released",
  "minimumAvailability": "released",
  "releaseDate": "1977-05-25",
  "digitalRelease": "2004-09-21",
  "physicalRelease": "2004-09-21",
  "runtime": 121,
  "status": "available",
  "monitored": true,
  "hasFile": true,
  "qualityProfileId": 1,
  "rootFolderPath": "/data/movies",
  "libraryPath": "/data/movies/Star Wars (1977)/Star Wars (1977).mkv",
  "posterPath": "/6FfCtAuVAW8XJjZ7eWeLibRLWTw.jpg",
  "backdropPath": "/2w4xG178RpB4MDAIfTkqAuSJzec.jpg",
  "lastMetadataSync": "2026-06-20T03:00:00Z",
  "addedAt": "2026-06-02T09:00:00Z",
  "linkedJob": null
}
```

`linkedJob` matches the §3.3 shape (present while a grab is in flight; sourced from `movie.job_id` → `GetJob`).

### 4.4 Add / toggle / delete / search / grab / refresh

- **M3 `POST /movies`** body `{ "tmdbId": 11, "monitored": true, "qualityProfileId": 1, "rootFolderPath": "/data/movies", "minimumAvailability": "released", "searchOnAdd": false }`. `minimumAvailability` defaults from TMDB status mapping (`07-ext-tmdb-api.md` Rec (d): Released→`released`, Post Production→`inCinemas`, etc.); the API accepts an explicit override. Responses identical in spirit to S3: `201` with M2 body; `409 conflict` (`idx_movie_tmdb` UNIQUE) with existing `id`; `502` on TMDB failure.
- **M4** body `{ "monitored": true }` → `200 { "id":7, "monitored":true }`.
- **M5 `DELETE /movies/{id}`** `?deleteFiles=true` → `204`; same propagate-to-TorBox semantics as §3.7.
- **M6 `GET /movies/{id}/search`** → Release list (§3.9). Prowlarr `type=movie`, `categories=2000` (`+2045` UHD optional), `indexerIds=-1`; query `{Title} {Year}` (`{TmdbId:X}`/`{ImdbId:X}` where supported, `06-ext-prowlarr-api.md`). Movies skip episode/pack parsing (`00`/requirements §18 Phase 1).
- **M7 `POST /movies/{id}/grab`** body `{ "releaseId": "rel_…" }` → `202 { "jobId":…, "state":"pending", "deduped":false }`; `(media_type='movie', media_ref=movie.id)`.
- **M8 `POST /movies/{id}/refresh`** → `202 { "queued": true }`.

---

## 5. Search (free-text Prowlarr)

A protocol-agnostic, title-agnostic free-text search (FR-UI-4 "as a free-text search", FR-SR-1/2) — the SPA's global search box. Distinct from the per-title searches (§3.8/§4.4) in that it is **not** scoped to a catalog id and returns raw ranked releases the user can grab into a *chosen* catalog target.

| # | Method | Path | Purpose |
|---|---|---|---|
| F1 | GET | `/search` | Free-text Prowlarr search → ranked Release list. |
| F2 | POST | `/search/grab` | Grab a chosen release, optionally binding it to a catalog item. |
| F3 | GET | `/search/indexers` | List configured Prowlarr indexers (for UI filters/diagnostics). |

**F1 query params:** `q` (required), `type` ∈ `{search,tvsearch,movie}` (default `search`; maps to Prowlarr `type`, `06-ext-prowlarr-api.md`), `categories` (repeated, optional; passed through as repeated keys to Prowlarr), `protocol` (`usenet|torrent` filter), `indexerIds` (repeated, optional; default all `-1`), `cachedOnly`, `sort` ∈ `{score,seeders,size,age,grabs}` (default `score`), `order`, `limit`, `offset`. Returns Release list (§3.9). `502 upstream_unavailable` `details.service:"prowlarr"` on Prowlarr error; `[]` items on zero results.

**F2 `POST /search/grab`** body:
```json
{
  "releaseId": "rel_8f3a1c…",
  "bind": { "mediaType": "movie", "mediaRef": 7 }   // optional; null = grab as unmanaged (becomes a "known" webdav item but not catalog-linked)
}
```
`bind` ties the grab to a catalog row via the polymorphic `(media_type, media_ref)` pair (`02` §3.1); omit it to grab content not yet in the catalog (it lands as a Boxarr-submitted job and a `known` webdav item on next reconcile). Response `202 { "jobId":…, "state":"pending", "deduped":false }`. Same `422`/`404` as §3.10.

**F3 `GET /search/indexers`** — proxies Prowlarr `GET /api/v1/indexer` (`06-ext-prowlarr-api.md`), projected to what UI filters need:
```json
{
  "items": [
    { "id": 3, "name": "1337x", "protocol": "torrent", "enable": true, "privacy": "public",
      "categories": [{ "id": 5000, "name": "TV" }, { "id": 2000, "name": "Movies" }] },
    { "id": 5, "name": "DrunkenSlug", "protocol": "usenet", "enable": true, "privacy": "private",
      "categories": [{ "id": 5000, "name": "TV" }] }
  ],
  "total": 2
}
```

---

## 6. Catalog lookup (add new titles)

TMDB/TVDB search used by the "add series/movie" flows (FR-CAT-1/2). Pure metadata search — adds nothing to the catalog; the SPA POSTs the chosen result to `/series` or `/movies`.

| # | Method | Path | Purpose |
|---|---|---|---|
| L1 | GET | `/lookup/series?term=` | TMDB `/search/tv` (+ TVDB cross-id) for series candidates. |
| L2 | GET | `/lookup/movies?term=` | TMDB `/search/movie` for movie candidates. |

**Query:** `term` (required). `term` may be a raw title, `tmdb:{id}`, `tvdb:{id}`, or `imdb:{id}` — prefix-routed (Servarr convention; `tvdb:` resolves via TMDB `/find?external_source=tvdb_id`, `07-ext-tmdb-api.md` §4, returning `tv_results[0].id`). Optional `year`. Returns a flat list (no pagination wrapper needed — TMDB returns ≤20).

**L1 response — JSON** (built from TMDB `/search/tv`, `07-ext-tmdb-api.md` §10; `tvdbId` filled from `external_ids` on demand, `inLibrary`/`libraryId` reflect whether `GetSeriesByTMDB` already has it so the UI can show "Added"):
```json
{
  "items": [
    {
      "tmdbId": 1396,
      "tvdbId": 81189,
      "imdbId": "tt0903747",
      "title": "Breaking Bad",
      "year": 2008,
      "overview": "When Walter White, a chemistry teacher, is diagnosed…",
      "posterPath": "/ggFHVNu6YYI5L9pCfOacjizRGt.jpg",
      "backdropPath": "/bsNm9z2TJfe0WO3RedPGWQ8mG1X.jpg",
      "tmdbStatus": "Ended",
      "inLibrary": false,
      "libraryId": null
    }
  ]
}
```

**L2 response — JSON** (TMDB `/search/movie`, `07-ext-tmdb-api.md` §11):
```json
{
  "items": [
    {
      "tmdbId": 550,
      "imdbId": "tt0137523",
      "title": "Fight Club",
      "year": 1999,
      "overview": "A ticking-time-bomb insomniac…",
      "posterPath": "/pB8BM7pdSp6B6Ih7QZ4DrQ3PmJK.jpg",
      "backdropPath": "/hZkgoQYus5vegHoetLkCJzb17zJ.jpg",
      "tmdbStatus": "Released",
      "inLibrary": false,
      "libraryId": null
    }
  ]
}
```

`502 upstream_unavailable` `details.service:"tmdb"` (or `"tvdb"`) on provider failure; `[]` on no matches (a valid `200`).

---

## 7. WebDAV view

Lists every release folder on the flat TorBox mount (FR-WD-1/2/3), served from the cached `webdav_item` table (`02` §3.4) — **not** a live mount scan per request (FR-UI-9, NFR-7; the expensive scan is the 15-min reconciler, `06`). One row per release folder with name, size, category, and known-flag.

| # | Method | Path | Purpose |
|---|---|---|---|
| W1 | GET | `/webdav` | List all mount items (filterable). |
| W2 | POST | `/webdav/refresh` | Trigger an out-of-band reconcile sweep. |

**W1 query params:** `category` ∈ `{movie,series,unknown}`, `known` (bool), `sort` ∈ `{name,size,lastSeen}` (default `name`), `order`, `limit`, `offset`. `is_broken=1` rows (present in a prior sweep, gone now, `02` §3.4) are **excluded by default**; pass `includeBroken=true` to show them.

**W1 response — representative JSON (webdav list):**
```json
{
  "items": [
    {
      "id": 301,
      "name": "Star.Wars.1977.1080p.BluRay.x264-GROUP",
      "remotePath": "/mnt/torbox/Star.Wars.1977.1080p.BluRay.x264-GROUP",
      "size": 8589934592,
      "category": "movie",
      "known": true,
      "jobId": 412,
      "isBroken": false,
      "firstSeen": "2026-06-02T09:05:00Z",
      "lastSeen": "2026-06-20T15:00:00Z"
    },
    {
      "id": 318,
      "name": "Some.Random.Pack.2024",
      "remotePath": "/mnt/torbox/Some.Random.Pack.2024",
      "size": 32212254720,
      "category": "unknown",
      "known": false,
      "jobId": null,
      "isBroken": false,
      "firstSeen": "2026-06-19T22:00:00Z",
      "lastSeen": "2026-06-20T15:00:00Z"
    }
  ],
  "total": 2, "limit": 50, "offset": 0
}
```

`category` for `known` items comes from Boxarr's mapping at submit time; for `unknown` items it is the best-effort parse (`internal/importer/release`, `06`, FR-WD-2). `unknown` items are also what feed `unknown_content` notifications (FR-WD-3 → §8); the actions on an unknown item live on the **notification**, not on the webdav row (§8.5).

**W2 `POST /webdav/refresh`** → `202 { "queued": true }`; kicks the reconciler immediately (FR-NC-1 sweep, bypassing the 15-min cadence) for users who just added content. Uses the existing forced WebDAV `/refresh` cooldown machinery (`01` §4 reconciler / `maybeRefreshWebDAV`).

---

## 8. Storage overview

Total used, plan limits, monthly usage/cooldown, and per-category breakdown (FR-ST-1/2/3, FR-LIM-4). Sourced from the cached `webdav_item` sizes (`WebDAVUsageBytes`, `02` §5.4) + the TorBox `/user/me` account fields (`05-ext-torbox-api.md`).

| # | Method | Path | Purpose |
|---|---|---|---|
| ST1 | GET | `/storage` | Aggregate storage + plan + usage. |

**ST1 response — representative JSON (storage):**
```json
{
  "usedBytes": 53687091200,
  "byCategory": {
    "movie": 32212254720,
    "series": 19327352832,
    "unknown": 2147483648
  },
  "downloads": {
    "active": 2,
    "queued": 1,
    "totalKnownItems": 184
  },
  "plan": {
    "tier": 2,
    "tierName": "Pro",
    "concurrentSlots": 10,
    "isSubscribed": true,
    "premiumExpiresAt": "2026-12-01T00:00:00Z"
  },
  "usage": {
    "monthlyDownloadedBytes": 10737418240,
    "cooldownUntil": null,
    "inCooldown": false
  },
  "limits": {
    "createPerHour": 60,
    "torrentCreatePerMinute": 300,
    "note": "uncached torrent + usenet creates share the 60/hr cap"
  }
}
```

**Field provenance & locked defaults:**
- `usedBytes` = `SUM(size) WHERE is_broken=0` from `webdav_item` (`02` §5.4 `WebDAVUsageBytes`, FR-ST-1); equals the sum of the usenet + torrent `mylist` sizes the reconciler last persisted.
- `byCategory` = `SUM(size) GROUP BY category` over non-broken webdav items (FR-ST-3).
- `plan.tier` = the **integer** TorBox `plan` field (`0=Free,1=Essential,2=Pro,3=Standard`, `05-ext-torbox-api.md`). `plan.concurrentSlots` is **derived** (TorBox does not return it): **chosen default map `{0:1, 1:3, 2:10, 3:5}`** with **fallback** `1` for an unrecognized tier (cited runtime-verify register, `00` §9 TorBox — "active-slot limit per tier"). This drives the FR-LIM-2 concurrency gate display.
- `usage.monthlyDownloadedBytes` = TorBox `total_downloaded` (bytes, 30-day window per `05-ext-torbox-api.md`); `cooldownUntil`/`inCooldown` from `cooldown_until` (set when `MONTHLY_LIMIT`/`COOLDOWN_LIMIT` hit, `05-ext-torbox-api.md`). All of `/user/me` is on the runtime-verify register (`00` §9 TorBox — "field names…verify entirely"); the client maps defensively via `FlexInt`/optional fields (`00` §5.5).
- `limits.*` are the **fixed published caps** (`05-ext-torbox-api.md`: createtorrent 60/hr uncached, 300/min cached shared, createusenetdownload 60/hr) the worker's rate-limit budget enforces (`06`, FR-LIM-3); shown for operator awareness.

The TorBox `/user/me` call is cached server-side (reuse the 5-minute health-ping cache pattern, `00` §5.6) so the storage view does not hammer the account endpoint on every page load.

---

## 9. Notifications

The notification center (FR-NC-2/3/4): persistent, newest-first, unread badge, with actions on unknown content. Served from `notification` (`02` §3.3); listing is `ORDER BY created_at DESC, id DESC` (`02` §5.3), badge is `COUNT(*) WHERE read=0`.

| # | Method | Path | Purpose |
|---|---|---|---|
| N1 | GET | `/notifications` | Newest-first list (+ unread filter). |
| N2 | GET | `/notifications/unread-count` | Badge count (cheap, polled by the SPA). |
| N3 | PUT | `/notifications/{id}/read` | Mark one read. |
| N4 | PUT | `/notifications/read-all` | Mark all read. |
| N5 | POST | `/notifications/{id}/action` | Act on an `unknown_content` notification. |

**N1 query params:** `unreadOnly` (bool, default `false`), `type` (filter by notification `type`), `sort` fixed (newest-first; no override), `limit` (default `50`), `offset`. 

**N1 response — JSON.** `payload` is the parsed JSON blob (`02` §3.3 stores it as TEXT) typed by `type`; the SPA renders per-type. `type` ∈ the eight values fixed in `02` §3.3 (`download_completed | grab_failed | heal_triggered | heal_succeeded | heal_failed | deletion_completed | limit_reached | unknown_content`).
```json
{
  "items": [
    {
      "id": 5012,
      "type": "unknown_content",
      "read": false,
      "createdAt": "2026-06-20T15:00:30Z",
      "readAt": null,
      "jobId": null,
      "payload": {
        "name": "Some.Random.Pack.2024",
        "size": 32212254720,
        "category": "unknown",
        "remotePath": "/mnt/torbox/Some.Random.Pack.2024",
        "webdavItemId": 318,
        "actions": ["adopt", "ignore", "delete"]
      }
    },
    {
      "id": 5009,
      "type": "grab_failed",
      "read": false,
      "createdAt": "2026-06-20T14:12:00Z",
      "readAt": null,
      "jobId": 977,
      "payload": {
        "title": "Some.Movie.2024.2160p",
        "error": "stalled (no seeds)",
        "torboxId": 44213,
        "mediaType": "movie",
        "mediaRef": 7
      }
    },
    {
      "id": 5004,
      "type": "limit_reached",
      "read": true,
      "createdAt": "2026-06-20T11:00:00Z",
      "readAt": "2026-06-20T11:05:00Z",
      "jobId": null,
      "payload": { "limit": "COOLDOWN_LIMIT", "cooldownUntil": "2026-06-21T00:00:00Z" }
    }
  ],
  "total": 3, "limit": 50, "offset": 0,
  "unreadCount": 2
}
```
The list envelope carries `unreadCount` so the SPA gets the badge in the same round trip.

**N2 `GET /notifications/unread-count`** → `{ "unreadCount": 2 }` (`02` §5.3 `UnreadCount`). This is the cheap endpoint the SPA polls on an interval; `200` always.

**N3 `PUT /notifications/{id}/read`** → `200 { "id":5012, "read":true, "readAt":"2026-06-20T15:30:00Z" }` (`MarkNotificationRead`). `404` on unknown id.

**N4 `PUT /notifications/read-all`** → `200 { "markedRead": 7 }` (`MarkAllNotificationsRead`, `02` §5.3).

### 8/9.5 Act on unknown content (N5) — adopt / ignore / delete-from-TorBox

`POST /notifications/{id}/action` is valid **only** on `type=unknown_content` notifications (else `422 unprocessable`). It is the bridge from FR-WD-3 to FR-NC-2 — the actions the user takes on a mount item Boxarr did not submit.

```json
// request — one of:
{ "action": "ignore" }
{ "action": "delete" }
{ "action": "adopt", "adopt": { "mediaType": "movie", "tmdbId": 11, "category": "movie" } }
```

| `action` | Effect | Result |
|---|---|---|
| `ignore` | Mark the notification read and the `webdav_item` known (so it stops re-raising each sweep) without touching TorBox. | `200 { "resolved": true, "outcome": "ignored" }` |
| `delete` | Delete the underlying download from TorBox (`controltorrent`/`controlusenetdownload` `operation=delete`, branch by detected protocol) via the deleter worker; mark the notification read. | `202 { "queued": true, "outcome": "delete_enqueued" }` |
| `adopt` | Categorize/attach the item: link the `webdav_item` to a catalog row (creating the movie/series from `tmdbId` if not present, then importing the existing files via the importer's best-match repoint, `06`) and set its `category`. | `202 { "queued": true, "outcome": "adopt_enqueued", "mediaType": "movie" }` |

`delete`/`adopt` are `202` because they hand off to async workers (deleter / importer); `ignore` is a synchronous `200`. All three mark the notification read. An unknown `action` value is `400 bad_request`.

---

## 10. Settings

All connections + options, with per-connection test endpoints (FR-UI-8, FR-CFG-1, NFR-5). Reads merge env defaults (`08-config-deploy-ci.md`) with operator overrides from the `settings` KV table (`02` §3.5); writes go to the `settings` table (`SetSetting`/`AllSettings`, `02` §5.5). **Secrets are write-only:** the GET never returns token/key values — only a boolean `*Configured` flag (NFR-4 — keys never logged/leaked). The settings UI also exposes the seeded servarr profiles/roots (`02` §3.6) so the operator can repoint a library root (`UpsertRootFolder`).

| # | Method | Path | Purpose |
|---|---|---|---|
| C1 | GET | `/settings` | All settings (secrets redacted to booleans). |
| C2 | PUT | `/settings` | Update settings (partial; only present keys change). |
| C3 | POST | `/settings/test/torbox` | Test the TorBox token. |
| C4 | POST | `/settings/test/prowlarr` | Test Prowlarr URL+key. |
| C5 | POST | `/settings/test/tmdb` | Test the TMDB key. |
| C6 | POST | `/settings/test/tvdb` | Test the TVDB key. |
| C7 | POST | `/settings/test/plex` | Test Plex URL+token. |
| C8 | POST | `/settings/test/seerr` | Test reachability of a configured Seerr instance. |
| C9 | GET | `/settings/quality-profiles` | List seeded quality profiles (`02` §3.6). |
| C10 | GET | `/settings/root-folders` | List seeded root folders (`?kind=tv|movie`). |
| C11 | PUT | `/settings/root-folders/{id}` | Repoint a root folder path (`UpsertRootFolder`). |

**C1 response — JSON** (secrets as `*Configured` booleans; non-secret values returned verbatim; `tmdb.imageBase`/`tmdb.posterSizes` come from the cached `/configuration` call so the SPA can build poster URLs, `07-ext-tmdb-api.md` §3):
```json
{
  "torbox":   { "tokenConfigured": true },
  "prowlarr": { "url": "http://prowlarr:9696", "apiKeyConfigured": true },
  "tmdb":     { "apiKeyConfigured": true,
                "imageBase": "https://image.tmdb.org/t/p/",
                "posterSizes": ["w92","w154","w185","w342","w500","w780","original"],
                "stillSizes": ["w92","w185","w300","original"] },
  "tvdb":     { "apiKeyConfigured": true },
  "plex":     { "url": "http://plex:32400", "tokenConfigured": false },
  "seerr":    { "url": "http://overseerr:5055", "apiKeyConfigured": true },
  "library":  { "tvRoot": "/data/tv", "movieRoot": "/data/movies",
                "webdavMountRoot": "/mnt/torbox", "webdavTorrentSubpath": "" },
  "selection": { "preferredResolutions": ["2160p","1080p","720p"],
                 "preferredQualities": ["WEB-DL","BluRay","WEBRip","HDTV"],
                 "minSeeders": 1, "minGrabs": 0, "requireCached": false, "minScore": 0,
                 "preferredGroups": [], "blockedGroups": [], "blockedKeywords": [] },
  "intervals": { "pollInterval": "1m", "reconcileInterval": "15m",
                 "metadataInterval": "24h", "searchInterval": "6h", "healInterval": "1h" },
  "limits":    { "maxConcurrentDownloads": 10, "healEnabled": false }
}
```

**C2 `PUT /settings`** body is the **same shape, partial** — only present keys are written (each maps to a `settings.key`, e.g. `prowlarr.url`, `selection.preferred_resolution`, `02` §3.5). Secrets are accepted as plain values on write (`{"prowlarr":{"apiKey":"abc…"}}`) and stored; they never come back on read. Returns `200` with the redacted C1 shape. `400 bad_request` on an invalid value (e.g. an interval that does not parse as a Go `time.Duration`, `details.field`).

**C3–C8 test endpoints** take the relevant connection params in the body (so the operator can test *before* saving) and return a uniform result; on success they perform a single live, read-only probe of the upstream:
```json
// request example (C4 Prowlarr) — uses saved values if body omits them
{ "url": "http://prowlarr:9696", "apiKey": "abc…" }
// response (success)
{ "ok": true, "service": "prowlarr", "detail": "12 indexers reachable" }
// response (failure) — HTTP 200 with ok:false (a test "failing" is a normal result, not an HTTP error)
{ "ok": false, "service": "prowlarr", "detail": "connection refused" }
```
Probes (read-only, no mutation): TorBox → `GET /user/me` (validates the Bearer token, `05-ext-torbox-api.md`); Prowlarr → `GET /api/v1/indexer` (`06-ext-prowlarr-api.md`); TMDB → `GET /configuration` (`07-ext-tmdb-api.md`); TVDB → `POST /login` then a trivial GET (JWT lifecycle, `03`/`08-ext-tvdb-api.md`); Plex → `GET /` or `/identity` with `X-Plex-Token` (`09-ext-plex-api.md`); Seerr → `GET {url}/api/v1/status` with the Seerr key. A test is **always HTTP 200** with `ok:true|false` — the connection failing is a normal UI outcome, not a transport error. A malformed request body (e.g. missing `url`) is `400 bad_request`.

**C9 `GET /settings/quality-profiles`** → `{ "items": [{ "id":1, "name":"Any", "isDefault":true }] }` (`ListQualityProfiles`, `02` §5.6). These are the same ids the Seerr emulation returns (`05`), so the SPA and Seerr agree.

**C10 `GET /settings/root-folders?kind=tv`** → `{ "items": [{ "id":1, "path":"/data/tv", "mediaKind":"tv", "accessible":true, "freeSpaceBytes":4398046511104 }] }` (`ListRootFolders`; `accessible`/`freeSpaceBytes` computed at request time via `os.Statfs`, matching the Seerr rootfolder shape, `02` §3.6 / `05`).

**C11 `PUT /settings/root-folders/{id}`** body `{ "path": "/data/tv2" }` → `200` with the updated row (`UpsertRootFolder`, `02` §3.6 quirk 2 — keeps the seeded id stable so Seerr config stays valid). `400` if the path does not exist/isn't a directory (mirrors the baseline `os.Stat` validation on `SymlinkRoot`, `04-sab-api-config-ci.md` §1).

---

## 11. System

| # | Method | Path | Purpose |
|---|---|---|---|
| Y1 | GET | `/healthz` | **Liveness/readiness** — kept verbatim at the **root** (not under `/api/v1`), unauthenticated. |
| Y2 | GET | `/api/v1/status` | Version, worker run times, catalog/job counts (authenticated). |

**Y1 `/healthz`** is unchanged from the baseline (verified `04-sab-api-config-ci.md` §2 / `01` §5): `200` body `"ok"` or `503` body `"unhealthy: <err>"`; the `Health.Check` pings DB then TorBox (5-minute cached, `00` §5.6). The Pinger is repointed to Boxarr's deps (DB + TorBox token; optionally Prowlarr/TMDB reachability, `00` §5.6) but the shape and the `healthcheck` CLI subcommand that self-GETs it (for the distroless `HEALTHCHECK`) are kept. It lives **outside** `/api/v1` so the container healthcheck needs no api key. Documented here for completeness; owned mechanically by `01` §2 step 9.

**Y2 `GET /api/v1/status`** — the SPA dashboard/footer recap. Worker run times come from the reused `HealReporter.HealRunInfo()` (verified `04-sab-api-config-ci.md` §2) generalized to per-loop `last/next` (`01` §4 worker topology); counts come from cheap store aggregates.
```json
{
  "version": "1.420",
  "startedAt": "2026-06-20T08:00:00Z",
  "uptimeSeconds": 28800,
  "workers": {
    "usenetPoller":     { "lastRun": "2026-06-20T15:59:00Z", "nextRun": "2026-06-20T16:00:00Z" },
    "torrentPoller":    { "lastRun": "2026-06-20T15:59:00Z", "nextRun": "2026-06-20T16:00:00Z" },
    "reconciler":       { "lastRun": "2026-06-20T15:45:00Z", "nextRun": "2026-06-20T16:00:00Z" },
    "metadataRefresh":  { "lastRun": "2026-06-20T03:00:00Z", "nextRun": "2026-06-21T03:00:00Z" },
    "healer":           { "lastRun": "2026-06-20T15:00:00Z", "nextRun": "2026-06-20T16:00:00Z", "enabled": false }
  },
  "counts": {
    "series": 42,
    "movies": 187,
    "episodesWanted": 13,
    "moviesWanted": 4,
    "activeJobs": 3,
    "failedJobs": 1,
    "brokenSymlinks": 0,
    "healFailed": 0,
    "unknownWebdavItems": 1,
    "unreadNotifications": 2
  }
}
```
`brokenSymlinks`/`healFailed` reuse the existing `SymlinkCounts`/heal-failed aggregates (verified `handlers.go:303-308`, the reshaped `/health/symlinks` data, `01` §2 step 7 "reshaped into `/api/v1`"). `version` is the ldflags-injected `main.version` (verified `04-sab-api-config-ci.md` §3).

---

## 12. Quirks to bake in

1. **Two envelopes never mix.** Boxarr's `/api/v1` uses `{error:{code,message,details}}` + bare/`{items,total,…}` bodies. The `{success,error,detail,data}` envelope is the **upstream TorBox** shape only (`03`) and the SAB shape is **deleted** (`00` Assumption B). Do not reintroduce `{status:bool}` anywhere in `/api/v1`.
2. **Header-only auth for the SPA; query-param auth for Seerr.** `/api/v1` reads `X-Api-Key` (keep the key out of access logs); the Seerr surfaces read `apikey` query (Servarr convention, `05`). The constant-time compare helper is shared, but the *transport* differs by namespace.
3. **Localhost bypass is loopback-AND-empty-key only.** Once `BOXARR_API_KEY` is set, even loopback must present it. The bypass never touches `/sonarr`/`/radarr`.
4. **Grabs are `202`, not `200`.** Accepting a release means *enqueued* (artifact stored + `pending` job created); submission/poll/import are async workers (`06`). The SPA shows the job moving through states via the `linkedJob` on detail (§3.3) or `/api/v1/status`.
5. **Deletes are async + propagate to TorBox.** The API marks the job `deleted` and returns `204`; the deleter worker drains it with retry (FR-DEL-1, reused deleter). `?deleteFiles=false` leaves TorBox content (which then surfaces as `unknown` on reconcile).
6. **Progress is percent-int 0–100; sizes are bytes-int; timestamps are `Z`-suffixed RFC 3339; calendar dates are bare `YYYY-MM-DD`.** The 0.0–1.0 TorBox `progress` float is converted at the worker boundary (`06`), never exposed raw.
7. **Status roll-ups for season/series are computed here, never stored** (`02` §2.2). Precedence is fixed in §3.4; episodes that are unmonitored do not pull a series toward `wanted`.
8. **`releaseId` is an opaque, expiring token.** It encodes indexer+guid+protocol+chosen-URL and is cached server-side (mirroring Prowlarr's 30-min cache). A grab against an expired `releaseId` is `404` — the UI re-searches. Never let the client supply a raw `downloadUrl`/`magnetUrl` (those are Prowlarr-internal proxy URLs, `06-ext-prowlarr-api.md` Rec 5).
9. **Secrets are write-only.** GET `/settings` returns `*Configured` booleans, never token/key values (NFR-4). Tests accept inline secrets so the operator can validate before saving.
10. **Library/storage/webdav views read the DB, never scan the mount per request** (FR-UI-9, NFR-7). `POST /webdav/refresh` and `POST .../refresh` are the only ways the SPA forces a live sweep, and they return `202` (async).
11. **Plan `concurrentSlots` is a derived default, not a TorBox field** — map `{0:1,1:3,2:10,3:5}`, fallback `1`; flagged on the runtime-verify register (`00` §9 TorBox).
12. **Repeated-key encoding when this API proxies Prowlarr.** `categories`/`indexerIds` filters are passed to Prowlarr as repeated keys, never comma-separated (HTTP 400 otherwise, `06-ext-prowlarr-api.md`); `indexerIds=-1` for "all".
13. **An empty search result is `200`, not `404`.** Prowlarr returns `[]` on indexer failure (errors logged, not raised, `06-ext-prowlarr-api.md`); Boxarr surfaces that as an empty `items` list.
14. **`/healthz` stays at the root, unauthenticated, verbatim.** It is not moved under `/api/v1`; the distroless `HEALTHCHECK` self-GET depends on its current shape (`01` §2 step 9).

---

## Definition of done

Boxarr's internal API is done when `internal/api/v1` serves every endpoint in §3–§11 under `/api/v1` (mounted before the SPA catch-all), fronted by the reused constant-time `X-Api-Key` middleware with the loopback-and-empty-key bypass and the §1.1 error envelope; the series surface returns the §3.3 detail with API-computed season/series roll-ups (§3.4), and add/toggle/delete/search/grab/refresh behave per §3.5–§3.11 with grabs returning `202` (enqueued, deduped) and deletes returning `204` (async, propagating to TorBox by `protocol`); the movie surface mirrors it (§4); free-text search (§5), TMDB/TVDB lookup (§6), the DB-served WebDAV view (§7), the storage overview with derived plan slots and `/user/me` usage (§8), the persistent newest-first notification center with unread badge and adopt/ignore/delete actions on unknown content (§9), the secret-redacting settings surface with per-connection read-only test probes (§10), and `/api/v1/status` plus the kept-verbatim `/healthz` (§11) all return exactly the JSON shapes and status codes documented; every list endpoint honors the §1 pagination/sorting/filtering conventions and emits RFC 3339-`Z` timestamps / bare `YYYY-MM-DD` dates / byte-int sizes / percent-int progress; no `/api/v1` response uses the dropped SAB `{status,error}` envelope; and `gofmt -s` + `golangci-lint` pass with every handler and DTO documented. Cross-refs: `00-decisions-and-assumptions.md` (auth, namespaces, runtime-verify register), `01-architecture-and-packages.md` (`internal/api/v1` placement, router, SPA catch-all), `02-data-model.md` (entity/field names, store methods, `MediaStatus`, notification/webdav/settings shapes), `03-external-contracts.md` (TorBox/Prowlarr/TMDB/TVDB/Plex contracts behind lookups, search, storage, tests), `05-seerr-emulation.md` (the *inbound* `/sonarr`+`/radarr` surface this doc is deliberately not), `06-pipelines.md` (what grab/search/delete/heal/adopt actually trigger).
