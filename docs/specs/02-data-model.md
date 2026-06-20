# Boxarr — Data Model: SQLite Schema & Store Layer (Spec 02)

**Date:** 2026-06-20
**Status:** Approved for planning

This spec owns the **full persistence layer**: the verbatim sab2torbox baseline (migrations `001`–`003`), the two distinct status concepts (job `state` vs. catalog `status`), the new goose migrations `004`–`009`, the Go domain structs, and the store methods that extend `internal/store`. It is grounded verbatim in the real sab2torbox source (`internal/store`, `internal/job`) and the pinned external contracts. Decisions are fixed in `00-decisions-and-assumptions.md` (esp. Assumption C and §5.4, the polymorphic media reference); this spec does **not** re-decide them — it instantiates them as DDL.

**One database. One writer.** Boxarr keeps sab2torbox's single SQLite file, single-writer serialization, WAL, and application-level dedup. Everything below is additive; **no baseline table or column changes shape** (Assumption C — *reuse the foundation verbatim where it works*). See `01-architecture-and-packages.md` for where `internal/store` sits and `06-pipelines.md` for who mutates these rows.

---

## 1. Baseline (the verbatim starting point — locked)

sab2torbox ships **3 migrations** (`001_init.sql`, `002_add_eta.sql`, `003_heal.sql`) managed by goose v3, embedded with `//go:embed migrations/*.sql` (verified `internal/store/store.go:17-18`). Boxarr keeps these **byte-for-byte** and continues the sequence at `004`.

### 1.1 Migration 001_init.sql — `jobs` (verbatim, `internal/store/migrations/001_init.sql:1-28`)

```sql
-- +goose Up
CREATE TABLE jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    state TEXT NOT NULL,
    category TEXT NOT NULL,
    nzb_name TEXT NOT NULL,
    nzb_content BLOB,
    nzb_url TEXT,
    nzb_sha256 TEXT,
    torbox_id INTEGER,
    torbox_hash TEXT,
    storage_path TEXT,
    total_bytes INTEGER DEFAULT 0,
    downloaded_bytes INTEGER DEFAULT 0,
    progress_pct INTEGER DEFAULT 0,
    fail_message TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    submitted_at TIMESTAMP,
    completed_at TIMESTAMP
);
CREATE INDEX idx_jobs_state ON jobs(state);
CREATE INDEX idx_jobs_torbox_id ON jobs(torbox_id);
CREATE INDEX idx_jobs_updated ON jobs(updated_at);
CREATE INDEX idx_jobs_sha256 ON jobs(nzb_sha256);

-- +goose Down
DROP TABLE jobs;
```

### 1.2 Migration 002_add_eta.sql + 003_heal.sql (verbatim)

```sql
-- 002_add_eta.sql
-- +goose Up
ALTER TABLE jobs ADD COLUMN eta_seconds INTEGER NOT NULL DEFAULT 0;
-- +goose Down
ALTER TABLE jobs DROP COLUMN eta_seconds;
```

```sql
-- 003_heal.sql
-- +goose Up
ALTER TABLE jobs ADD COLUMN heal_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE jobs ADD COLUMN last_healed_at TIMESTAMP;
ALTER TABLE jobs ADD COLUMN last_heal_error TEXT;

CREATE TABLE imported_symlinks (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id        INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    symlink_path  TEXT NOT NULL UNIQUE,
    target_path   TEXT NOT NULL,
    discovered_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_verified TIMESTAMP,
    is_broken     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_imported_symlinks_job ON imported_symlinks(job_id);
CREATE INDEX idx_imported_symlinks_target ON imported_symlinks(target_path);
CREATE INDEX idx_imported_symlinks_broken ON imported_symlinks(is_broken);

-- +goose Down
DROP TABLE imported_symlinks;
ALTER TABLE jobs DROP COLUMN heal_count;
ALTER TABLE jobs DROP COLUMN last_healed_at;
ALTER TABLE jobs DROP COLUMN last_heal_error;
```

### 1.3 Cumulative `jobs` columns after 001–003 (physical order, verified `01-sab-store-schema.md`)

`id` · `state` · `category` · `nzb_name` · `nzb_content` · `nzb_url` · `nzb_sha256` · `torbox_id` · `torbox_hash` · `storage_path` · `total_bytes` · `downloaded_bytes` · `progress_pct` · `fail_message` · `created_at` · `updated_at` · `submitted_at` · `completed_at` · `eta_seconds` · `heal_count` · `last_healed_at` · `last_heal_error`.

### 1.4 Baseline invariants Boxarr inherits unchanged (locked)

| Invariant | Value | Source |
|---|---|---|
| **DSN** | `file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_txlock=immediate` | `store.go:28-46` |
| **Driver** | `modernc.org/sqlite`, registered name `"sqlite"` (CGo-free) | `store.go` blank import |
| **Single writer** | `db.SetMaxOpenConns(1)` — serializes all writes, avoids `SQLITE_BUSY` churn | `store.go:31` |
| **Tx lock** | `_txlock=immediate` → every tx begins `BEGIN IMMEDIATE` | DSN |
| **FK enforcement** | ON (`foreign_keys(1)`) — so `ON DELETE CASCADE` fires | DSN |
| **Migrations** | goose v3, dialect `"sqlite3"`, `goose.SetBaseFS(migrationsFS)`, `goose.Up(db, "migrations")`, `NopLogger` | `store.go:48-59` |
| **Version table** | goose default **`goose_db_version`** (not overridden) | runtime-verify, 00 §9 |
| **App-level dedup** | **No `UNIQUE` on hashes.** `nzb_sha256` is `idx_jobs_sha256` only; dedup via `FindBySHA256(sha,cat)` / `FindByURL(url,cat)` returning most-recent (`id DESC LIMIT 1`) before insert | `store.go`, `01-sab-store-schema.md` |
| **DB dedup (exception)** | `imported_symlinks.symlink_path TEXT NOT NULL UNIQUE` + `ON CONFLICT(symlink_path) DO UPDATE` — the *only* real UNIQUE-driven upsert in the schema | `003_heal.sql` |
| **Update shape** | one full-row `UpdateJob` rewrites every mutable column + `updated_at=CURRENT_TIMESTAMP`; no targeted UPDATEs | `store.go:187-195` |

**Boxarr keeps `nzb_sha256` index-only (no UNIQUE), and applies the identical pattern to the new `torrent_hash` column** (locked — Assumption C; mirrors the existing dedup convention rather than introducing a DB constraint).

---

## 2. Two status concepts (make this unambiguous)

Boxarr has **two independent status vocabularies that must never be conflated**. One tracks a *download attempt*; the other tracks a *catalog item's acquisition lifecycle*. They live in different columns, on different tables, and are advanced by different workers (`06-pipelines.md`).

### 2.1 Concept (a) — `jobs.state`: the DOWNLOAD lifecycle (existing Go enum, extended)

`state` is the per-job (per-submission) machine. It is `type State string` validated **in Go only** (no DB `CHECK` — locked, Assumption C). Baseline has **11 states** (verified `internal/job/job.go:10-52`); Boxarr **adds one** torrent-specific state, `StateSeeding`, into the Go `transitions` map (00 §6 — "extend the existing enum *in Go only*").

```go
// internal/job/job.go — State constants (baseline 11 + Boxarr's seeding = 12)
StatePending          State = "pending"
StateSubmitting       State = "submitting"
StateQueued           State = "queued"
StateDownloading      State = "downloading"
StateSeeding          State = "seeding"            // NEW — torrent uploading/ratio phase; files present, optional
StateCompleted        State = "completed"
StateImported         State = "imported"
StateDeleted          State = "deleted"            // terminal
StateFailed           State = "failed"             // terminal
StateHealing          State = "healing"
StateHealFailed       State = "heal_failed"
StateManuallyResolved State = "manually_resolved"  // terminal
```

Allowed transitions (baseline map, verified `job.go`, **plus** the two `seeding` edges Boxarr adds):

```
pending           -> [submitting, failed]
submitting        -> [queued, pending, failed]
queued            -> [downloading, completed, failed]
downloading       -> [seeding, completed, failed]     # +seeding (torrents finish download then upload)
seeding           -> [completed, failed]               # NEW
completed         -> [imported, deleted, failed]
imported          -> [deleted, failed, healing]
healing           -> [imported, heal_failed]
heal_failed       -> [healing, manually_resolved]
manually_resolved -> []   (terminal)
deleted           -> []   (terminal)
failed            -> []   (terminal)
```

- `(s State) CanTransitionTo(next State) bool` — linear scan of `transitions[s]` (unchanged).
- `(s State) IsTerminal() bool` — true when `transitions[s]` is empty (`failed`, `deleted`, `manually_resolved`) (unchanged).
- **`StateSeeding` is non-terminal and optional**: usenet jobs never enter it; torrent jobs *may* pass through it when `download_finished && !still uploading-required`. Pollers treat `download_finished && download_present` as the completion gate regardless of `download_state` (TorBox `download_state` may read `"uploading"` while files are already present — verified `05-ext-torbox-api.md` field reference). See `06-pipelines.md` §torrent-poll for the exact mapping of TorBox `download_state` strings (`"downloading"`, `"uploading"`, `"stalled (no seeds)"`, `"metaDL"`, `"completed"`, `"cached"`) onto these states. The literal `"stalled (no seeds)"` string (parens + space, runtime-verify 00 §9) is matched as a **failure substring** by the existing `Failed()` helper (matches `failed`/`error`/`stalled`).

`jobs.state` is plain `TEXT NOT NULL` — **no migration touches it**; the new `seeding` value is simply a string the existing column already accepts.

### 2.2 Concept (b) — media `status`: the per-item CATALOG lifecycle (new enum)

Distinct from a download attempt, **every monitored movie and every episode carries a `status`** representing whether Boxarr *has* the content (requirements §6 FR-UI-2/3). This is a **new Go enum** stored as `TEXT` in two columns: **`movie.status`** and **`episode.status`** (seasons/series derive their roll-up in the API layer, not in a column — `04-internal-api.md`). Like `jobs.state`, it has **no DB `CHECK`** (consistency with Assumption C).

```go
// internal/media/status.go — MediaStatus (catalog acquisition lifecycle)
type MediaStatus string

const (
    MediaWanted      MediaStatus = "wanted"        // monitored, missing, eligible (aired/released) — should be searched
    MediaSearching   MediaStatus = "searching"     // a search/grab is in flight (job pending..submitting)
    MediaDownloading MediaStatus = "downloading"   // a linked job is queued/downloading/seeding
    MediaAvailable   MediaStatus = "available"     // imported; library symlink present and not broken (has_file=1)
    MediaMissing     MediaStatus = "missing"       // monitored but NOT eligible yet (unaired/unreleased) OR not-yet-wanted
    MediaExpired     MediaStatus = "expired_broken" // was available; WebDAV target gone / symlink broken → heal candidate
)
```

Mapping to requirements §6 wording (`wanted / searching / downloading / available / expired-broken`, plus `missing`):

| Requirements term | `MediaStatus` value | Set by |
|---|---|---|
| wanted | `wanted` | catalog/reconciler when monitored + aired/released + no file |
| searching | `searching` | grab pipeline on search start |
| downloading | `downloading` | grab pipeline when job enters queued/downloading/seeding |
| available | `available` | importer after symlink created (sets `has_file=1`) |
| (missing/unaired) | `missing` | catalog when monitored but `air_date`/`release_date` in future, or unmonitored |
| expired/broken | `expired_broken` | healer/reconciler on broken symlink detection |

**Status is a projection, not a source of truth for downloads.** The authority for "is a download happening" is `jobs.state` reachable via `(media_type, media_ref)`; `episode.status`/`movie.status` is the **denormalized, fast-to-query** field the library views read (requirements FR-UI-9 — views read the DB, never scan WebDAV). The reconciler (`06-pipelines.md`) reconciles the two: e.g. a job that reached `StateImported` flips its `media_ref` row to `MediaAvailable`; a job that hit `StateFailed` returns the item to `wanted`.

**Defaults:** `movie.status` and `episode.status` default to `'missing'` at insert (SQL `DEFAULT 'missing'`); the catalog promoter recomputes to `wanted` once eligibility/monitoring is evaluated.

---

## 3. New migrations (full verbatim DDL, sequential `004`–`009`)

All files live in `internal/store/migrations/`, carry `-- +goose Up` / `-- +goose Down`, and continue the numeric sequence. **Every `ALTER TABLE ADD COLUMN` that is `NOT NULL` carries a `DEFAULT`** so it back-fills existing rows (SQLite requirement; runtime-verify 00 §9 — confirmed valid when a DEFAULT is supplied). New tables use `ON DELETE CASCADE` consistent with the `imported_symlinks` precedent (00 §5.4).

### 3.1 `004_protocol_media.sql` — extend `jobs` for torrents + media linkage

The polymorphic media pointer (**00 §5.4**): SQLite has no polymorphic FKs, so a job references its catalog item via the **pair** `(media_type, media_ref)` where `media_type ∈ {'movie','episode','season','series'}` and `media_ref` is the integer id in the corresponding catalog table (`movie.id` or `episode.id`; `season`/`series` reserved for season-pack/whole-series grabs). **No FK constraint is declared on `media_ref`** (it can point at two different tables) — referential integrity is maintained in application code, and catalog deletes null/clear the pointer rather than cascading through it. `protocol` distinguishes which submission columns are populated: `'usenet'` rows use `nzb_*`; `'torrent'` rows use `torrent_*`.

```sql
-- +goose Up
ALTER TABLE jobs ADD COLUMN protocol TEXT NOT NULL DEFAULT 'usenet';  -- 'usenet' | 'torrent'
ALTER TABLE jobs ADD COLUMN media_type TEXT;                          -- 'movie'|'episode'|'season'|'series'|NULL
ALTER TABLE jobs ADD COLUMN media_ref INTEGER;                        -- movie.id | episode.id (polymorphic, no FK)
ALTER TABLE jobs ADD COLUMN torrent_magnet TEXT;                      -- magnet URI (parallels nzb_url)
ALTER TABLE jobs ADD COLUMN torrent_hash TEXT;                        -- info-hash hex (parallels nzb_sha256; INDEX-only, app dedup)
ALTER TABLE jobs ADD COLUMN torrent_file BLOB;                        -- .torrent bytes (parallels nzb_content)

CREATE INDEX idx_jobs_torrent_hash ON jobs(torrent_hash);
CREATE INDEX idx_jobs_media ON jobs(media_type, media_ref);

-- +goose Down
DROP INDEX idx_jobs_media;
DROP INDEX idx_jobs_torrent_hash;
ALTER TABLE jobs DROP COLUMN torrent_file;
ALTER TABLE jobs DROP COLUMN torrent_hash;
ALTER TABLE jobs DROP COLUMN torrent_magnet;
ALTER TABLE jobs DROP COLUMN media_ref;
ALTER TABLE jobs DROP COLUMN media_type;
ALTER TABLE jobs DROP COLUMN protocol;
```

**Quirks baked in:** `protocol NOT NULL DEFAULT 'usenet'` back-fills every legacy row as usenet (correct — all existing jobs are usenet). `torrent_hash` is **indexed, not UNIQUE** (mirrors `nzb_sha256`; dedup is `FindByTorrentHash` returning `id DESC LIMIT 1`, §5). `idx_jobs_media(media_type, media_ref)` is the composite index that powers "find the job for this episode/movie".

### 3.2 `005_catalog.sql` — `series` / `season` / `episode` / `movie`

The catalog hierarchy. **Real FKs with `ON DELETE CASCADE`** (deleting a series cascades its seasons → episodes; matching the `imported_symlinks` precedent). External ids are stored as first-class columns (`tmdb_id`, `tvdb_id`, `imdb_id`) per `07`/`08`: TMDB is primary (`NOT NULL` where it is the discovery key), TVDB is nullable until resolved (required for the Sonarr emulation, `08-ext-tvdb-api.md` recommendation), IMDB nullable. A `metadata_json` TEXT column caches the raw provider blob (posters paths, genres, overview, networks) so the UI never re-hits TMDB per page load (FR-UI-9). Timestamps mirror the baseline (`created_at`/`updated_at` non-null with `CURRENT_TIMESTAMP` defaults).

```sql
-- +goose Up

----------------------------------------------------------------------
-- series
----------------------------------------------------------------------
CREATE TABLE series (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    tmdb_id            INTEGER NOT NULL,                 -- primary discovery key (TMDB tv id)
    tvdb_id            INTEGER,                          -- required for Sonarr emulation; nullable until resolved
    imdb_id            TEXT,                             -- 'tt...'; nullable
    title              TEXT NOT NULL,
    sort_title         TEXT NOT NULL DEFAULT '',
    year               INTEGER,                          -- first_air_date year; nullable
    overview           TEXT NOT NULL DEFAULT '',
    series_type        TEXT NOT NULL DEFAULT 'standard', -- 'standard'|'daily'|'anime' (drives episode->file mapping)
    tmdb_status        TEXT NOT NULL DEFAULT '',         -- TMDB string: 'Returning Series'|'Ended'|... (07)
    monitored          INTEGER NOT NULL DEFAULT 1,       -- series-level monitor flag
    season_folder      INTEGER NOT NULL DEFAULT 1,       -- Plex season-folder layout (echoed to Seerr)
    quality_profile_id INTEGER NOT NULL DEFAULT 1,       -- FK-ish into quality_profile (009)
    root_folder_path   TEXT NOT NULL DEFAULT '',         -- TV library root (matches a root_folder.path)
    library_path       TEXT,                             -- resolved '<root>/<Title> (<Year>)'; nullable until created
    poster_path        TEXT,                             -- TMDB relative path (reconstruct URL at render, 07)
    backdrop_path      TEXT,
    metadata_json      TEXT,                             -- cached raw TMDB/TVDB blob (genres, networks, etc.)
    last_metadata_sync TIMESTAMP,                        -- last successful refresh (FR-CAT-5)
    added_at           TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_series_tmdb ON series(tmdb_id);   -- one catalog row per TMDB series (catalog identity, not a dedup hash)
CREATE INDEX idx_series_tvdb ON series(tvdb_id);
CREATE INDEX idx_series_monitored ON series(monitored);

----------------------------------------------------------------------
-- season
----------------------------------------------------------------------
CREATE TABLE season (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id     INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    season_number INTEGER NOT NULL,               -- 0 = Specials
    monitored     INTEGER NOT NULL DEFAULT 1,     -- per-season monitor flag (FR-CAT-3)
    episode_count INTEGER NOT NULL DEFAULT 0,     -- from TMDB season summary
    air_date      TEXT,                            -- 'YYYY-MM-DD' season premiere; nullable
    poster_path   TEXT,
    metadata_json TEXT,
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_season_series_num ON season(series_id, season_number);

----------------------------------------------------------------------
-- episode
----------------------------------------------------------------------
CREATE TABLE episode (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    series_id       INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
    season_id       INTEGER NOT NULL REFERENCES season(id) ON DELETE CASCADE,
    season_number   INTEGER NOT NULL,             -- denormalized for fast (series,season,ep) lookups
    episode_number  INTEGER NOT NULL,             -- within the (aired/official) ordering
    absolute_number INTEGER,                       -- nullable; anime continuous count (TVDB absoluteNumber, 08)
    tmdb_id         INTEGER,                       -- TMDB episode id; nullable
    tvdb_id         INTEGER,                       -- TVDB episode id; nullable
    title           TEXT NOT NULL DEFAULT '',
    overview        TEXT NOT NULL DEFAULT '',
    air_date        TEXT,                          -- 'YYYY-MM-DD'; nullable (unaired). Drives air-date-aware wanted (FR-CAT-4)
    runtime         INTEGER,                       -- minutes; nullable
    still_path      TEXT,                          -- TMDB episode thumbnail relative path
    status          TEXT NOT NULL DEFAULT 'missing', -- MediaStatus (§2.2)
    monitored       INTEGER NOT NULL DEFAULT 1,    -- per-episode monitor (inherits season default)
    has_file        INTEGER NOT NULL DEFAULT 0,    -- 1 once a library symlink exists
    job_id          INTEGER REFERENCES jobs(id) ON DELETE SET NULL, -- last/active grab job; nullable
    library_path    TEXT,                          -- '<root>/<Series> (<Year>)/Season NN/<file>.ext'; nullable
    metadata_json   TEXT,
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_episode_series_se ON episode(series_id, season_number, episode_number);
CREATE INDEX idx_episode_season ON episode(season_id);
CREATE INDEX idx_episode_status ON episode(status);
CREATE INDEX idx_episode_job ON episode(job_id);
CREATE INDEX idx_episode_absolute ON episode(series_id, absolute_number);

----------------------------------------------------------------------
-- movie
----------------------------------------------------------------------
CREATE TABLE movie (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    tmdb_id              INTEGER NOT NULL,         -- primary discovery key (TMDB movie id)
    imdb_id              TEXT,                     -- present in /movie/{id}; nullable
    title                TEXT NOT NULL,
    sort_title           TEXT NOT NULL DEFAULT '',
    year                 INTEGER,                  -- release_date year; nullable
    overview             TEXT NOT NULL DEFAULT '',
    tmdb_status          TEXT NOT NULL DEFAULT '', -- 'Released'|'Post Production'|... (07)
    minimum_availability TEXT NOT NULL DEFAULT 'released', -- 'announced'|'inCinemas'|'released'|'preDB' (Radarr/Seerr, 10)
    release_date         TEXT,                     -- 'YYYY-MM-DD' primary; nullable. Drives air-date-aware wanted
    digital_release      TEXT,                     -- 'YYYY-MM-DD' from /release_dates type=4; nullable
    physical_release     TEXT,                     -- 'YYYY-MM-DD' from /release_dates type=5; nullable
    runtime              INTEGER,
    status               TEXT NOT NULL DEFAULT 'missing', -- MediaStatus (§2.2)
    monitored            INTEGER NOT NULL DEFAULT 1,
    has_file             INTEGER NOT NULL DEFAULT 0,
    quality_profile_id   INTEGER NOT NULL DEFAULT 1,
    root_folder_path     TEXT NOT NULL DEFAULT '', -- movie library root (matches a root_folder.path)
    library_path         TEXT,                     -- '<root>/<Title> (<Year>)/<Title> (<Year>).ext'; nullable
    job_id               INTEGER REFERENCES jobs(id) ON DELETE SET NULL, -- last/active grab job; nullable
    poster_path          TEXT,
    backdrop_path        TEXT,
    metadata_json        TEXT,                     -- cached raw TMDB blob (genres, collection, etc.)
    last_metadata_sync   TIMESTAMP,
    added_at             TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at           TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_movie_tmdb ON movie(tmdb_id);
CREATE INDEX idx_movie_imdb ON movie(imdb_id);
CREATE INDEX idx_movie_status ON movie(status);
CREATE INDEX idx_movie_monitored ON movie(monitored);
CREATE INDEX idx_movie_job ON movie(job_id);

-- +goose Down
DROP TABLE movie;
DROP TABLE episode;
DROP TABLE season;
DROP TABLE series;
```

**Quirks baked in (numbered):**
1. **`idx_series_tmdb` / `idx_movie_tmdb` ARE `UNIQUE`** — but this is *catalog identity* (one row per real-world title), not the hash-dedup pattern. The hash-dedup rule (no UNIQUE) applies to *download artifacts* (`nzb_sha256`, `torrent_hash`), not catalog primary keys. A movie/series is a singleton in the catalog; a grab is not.
2. **`episode.job_id` / `movie.job_id` use `ON DELETE SET NULL`**, not `CASCADE` — a hard-deleted job must not delete the catalog row; the item simply loses its grab link and falls back to `wanted` on reconcile.
3. **`absolute_number` is nullable** (TVDB only populates it for anime / when editors enter it — `08`). `series_type='anime'` selects absolute-order mapping in `06-pipelines.md`.
4. **`air_date`/`release_date` stored as `TEXT 'YYYY-MM-DD'`** (matches TMDB/TVDB string format verbatim; no date parsing at the storage layer). Air-date-aware "wanted" (FR-CAT-4) compares these strings against `date('now')` in SQL — lexicographic compare is correct for ISO dates.
5. **`has_file` is the fast availability flag** the library grid reads; it is set by the importer and cleared by the healer, kept in lockstep with `status='available'`/`'expired_broken'`.

### 3.3 `006_notifications.sql` — `notification`

The notification center (FR-NC-*). `payload` is a JSON TEXT blob (event-shaped per `04-internal-api.md`); `job_id` is a nullable FK so event notifications can reference a job but unknown-content notifications need not. `read` is the unread/read flag (0/1). Index on `(read, created_at)` powers the newest-first unread feed + badge count.

```sql
-- +goose Up
CREATE TABLE notification (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    type       TEXT NOT NULL,                   -- 'download_completed'|'grab_failed'|'heal_triggered'|
                                                --   'heal_succeeded'|'heal_failed'|'deletion_completed'|
                                                --   'limit_reached'|'unknown_content' (FR-NC-2/3)
    payload    TEXT NOT NULL DEFAULT '{}',      -- JSON: {name,size,category,torbox_id,error,...} per type
    job_id     INTEGER REFERENCES jobs(id) ON DELETE CASCADE, -- nullable; CASCADE so a deleted job drops its events
    read       INTEGER NOT NULL DEFAULT 0,      -- 0=unread, 1=read
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    read_at    TIMESTAMP                        -- when marked read; nullable
);
CREATE INDEX idx_notification_read_created ON notification(read, created_at);
CREATE INDEX idx_notification_job ON notification(job_id);

-- +goose Down
DROP TABLE notification;
```

**Quirks baked in:** `job_id ON DELETE CASCADE` (an event notification is meaningless once its job is hard-deleted), but the column is **nullable** so `unknown_content` notifications (no owning job) are valid. The badge count is `COUNT(*) WHERE read=0`; the feed is `ORDER BY created_at DESC, id DESC` (id breaks ties for same-second inserts).

### 3.4 `007_webdav_items.sql` — `webdav_item`

The WebDAV mount view (FR-WD-1/2/3), modeled directly on `imported_symlinks`: a real `UNIQUE` on the path with `ON CONFLICT ... DO UPDATE` upsert, an `is_broken INTEGER`, and a `last_seen` verification timestamp. One row per release folder on the mount. `category` is `'movie'|'series'|'unknown'`; `known`/`job_id` link it back to a Boxarr-submitted job when matched.

```sql
-- +goose Up
CREATE TABLE webdav_item (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,                  -- release-folder display name (TorBox mylist 'name')
    remote_path TEXT NOT NULL UNIQUE,           -- absolute path on the flat WebDAV mount (UNIQUE → upsert key)
    size        INTEGER NOT NULL DEFAULT 0,     -- total bytes of the folder
    category    TEXT NOT NULL DEFAULT 'unknown',-- 'movie'|'series'|'unknown' (FR-WD-2)
    known       INTEGER NOT NULL DEFAULT 0,     -- 1 if matched to a Boxarr job/catalog item
    job_id      INTEGER REFERENCES jobs(id) ON DELETE SET NULL, -- owning job when known; nullable
    is_broken   INTEGER NOT NULL DEFAULT 0,     -- 1 if previously seen but now absent (mirrors imported_symlinks)
    first_seen  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, -- updated each reconcile sweep (FR-NC-1)
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_webdav_item_known ON webdav_item(known);
CREATE INDEX idx_webdav_item_category ON webdav_item(category);
CREATE INDEX idx_webdav_item_job ON webdav_item(job_id);

-- +goose Down
DROP TABLE webdav_item;
```

**Quirks baked in:** `remote_path TEXT NOT NULL UNIQUE` is the **only** new UNIQUE-driven upsert (intentional — it mirrors `imported_symlinks.symlink_path` exactly; the reconciler re-sees the same path every 15 min and upserts size/last_seen). `job_id ON DELETE SET NULL` so deleting a job leaves the mount row (it still physically exists until TorBox rotates it). `is_broken=1` marks a row whose `remote_path` was present in a prior sweep but absent now — feeds the same broken-detection used by the healer.

### 3.5 `008_settings.sql` — `settings`

Simple KV for runtime-mutable config (settings UI, FR-CFG-1 / NFR-5). Env vars remain the source of defaults (`08-config-deploy-ci.md`); this table holds operator overrides written from the UI. Secret-bearing keys (tokens) are stored here only when set via UI; never logged (NFR-4).

```sql
-- +goose Up
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,               -- e.g. 'prowlarr.url', 'selection.preferred_resolution'
    value      TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE settings;
```

**Quirks baked in:** `key TEXT PRIMARY KEY` makes `INSERT ... ON CONFLICT(key) DO UPDATE` the natural upsert; values are always strings (callers parse). No `created_at` (a KV row's identity is its key; only `updated_at` matters).

### 3.6 `009_servarr.sql` — `quality_profile` / `root_folder` / `tag` (+ seed)

Minimal real tables so the Seerr emulation (`05-seerr-emulation.md`) returns **stable, real ids** for `GET /api/v3/qualityprofile`, `/rootfolder`, `/tag` — and so that ids Seerr echoes back in `POST /series`/`POST /movie` (`qualityProfileId`, `rootFolderPath`, `tags[]`) resolve to rows that exist. Seeded with **one TV root + one movie root + one default profile + (no tags)** in the same migration (a goose `Up` may contain `INSERT`s). The `freeSpace`/`totalSpace`/`accessible` fields Seerr's rootfolder shape needs are computed at request time (`05`), not stored.

```sql
-- +goose Up
CREATE TABLE quality_profile (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL,
    is_default INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE root_folder (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    path       TEXT NOT NULL UNIQUE,           -- absolute library root; must match what POST bodies send back
    media_kind TEXT NOT NULL,                  -- 'tv' | 'movie' (so /sonarr and /radarr return the right root)
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE tag (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    label      TEXT NOT NULL UNIQUE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Seed stable ids for Seerr emulation (05). Paths are the BOXARR_*_LIBRARY_ROOT defaults (08).
INSERT INTO quality_profile (id, name, is_default) VALUES (1, 'Any', 1);
INSERT INTO root_folder (id, path, media_kind) VALUES (1, '/data/tv', 'tv');
INSERT INTO root_folder (id, path, media_kind) VALUES (2, '/data/movies', 'movie');

-- +goose Down
DROP TABLE tag;
DROP TABLE root_folder;
DROP TABLE quality_profile;
```

**Quirks baked in (numbered):**
1. **Seeded ids are fixed and explicit** (`VALUES (1, ...)`) so Seerr config dropdowns always see profile id `1` and root ids `1`/`2` across restarts — stability is the point (`05`, runtime-verify of the rootfolder/profile shapes is in 00 §9 Seerr).
2. **`root_folder.path` is `UNIQUE`** and is the value Boxarr returns in `/rootfolder` *and* the value it must accept verbatim in `POST` `rootFolderPath` (00 §9 Seerr — "paths you return must match what you'll accept"). Default paths `/data/tv` + `/data/movies` are overridable via env (`08`); if the operator changes `BOXARR_TV_LIBRARY_ROOT`, an `UpsertRootFolder` call (settings UI) updates the seeded row's path rather than re-migrating.
3. **`tag` seeded empty** — `GET /api/v3/tag` legitimately returns `[]` (verified `10-ext-seerr-servarr-emulation.md` — empty array is valid). Tags are created lazily if Seerr ever references one.
4. The **default `quality_profile_id=1`** referenced by `series.quality_profile_id` / `movie.quality_profile_id` defaults always resolves to this seeded row.

---

## 4. Go domain structs (mirror the existing `job.Job` style)

These extend `internal/job` (the extended `Job`) and add a new `internal/media` package for catalog types (and `internal/notify`, `internal/webdav` types may co-locate — `01-architecture-and-packages.md`). Style matches the baseline verbatim: exported fields, pointer `*time.Time` for nullable timestamps, plain zero-values for nullable strings/ints handled via the `nullStr`/`nullInt`/`nullTime` helpers at the store boundary (verified `store.go:369-388`).

### 4.1 Extended `job.Job` (baseline fields + 004 columns)

```go
// internal/job/job.go — Job extended with protocol + media + torrent columns (004)
type Job struct {
    // --- baseline (001-003), unchanged ---
    ID              int64
    State           State
    Category        string
    NZBName         string
    NZBContent      []byte
    NZBURL          string
    NZBSHA256       string
    TorBoxID        int64
    TorBoxHash      string
    StoragePath     string
    TotalBytes      int64
    DownloadedBytes int64
    ProgressPct     int
    HealCount       int64
    LastHealedAt    *time.Time
    LastHealError   string
    ETASeconds      int64
    FailMessage     string
    CreatedAt       time.Time
    UpdatedAt       time.Time
    SubmittedAt     *time.Time
    CompletedAt     *time.Time

    // --- NEW (004_protocol_media) ---
    Protocol      string // "usenet" | "torrent"
    MediaType     string // "movie"|"episode"|"season"|"series"|"" (polymorphic, 00 §5.4)
    MediaRef      int64  // movie.id | episode.id; 0 = unset
    TorrentMagnet string
    TorrentHash   string
    TorrentFile   []byte
}

func (j *Job) NzoID() string { return "sab2tb_" + itoa(j.ID) } // baseline; SAB nzo_id format retained for log continuity
```

### 4.2 Catalog structs

```go
// internal/media/types.go
type MediaStatus string // §2.2 enum

type Series struct {
    ID               int64
    TMDBID           int64
    TVDBID           int64   // 0 = unresolved
    IMDBID           string
    Title            string
    SortTitle        string
    Year             int
    Overview         string
    SeriesType       string  // "standard"|"daily"|"anime"
    TMDBStatus       string
    Monitored        bool
    SeasonFolder     bool
    QualityProfileID int64
    RootFolderPath   string
    LibraryPath      string
    PosterPath       string
    BackdropPath     string
    MetadataJSON     string
    LastMetadataSync *time.Time
    AddedAt          time.Time
    CreatedAt        time.Time
    UpdatedAt        time.Time
}

type Season struct {
    ID           int64
    SeriesID     int64
    SeasonNumber int
    Monitored    bool
    EpisodeCount int
    AirDate      string // "YYYY-MM-DD" | ""
    PosterPath   string
    MetadataJSON string
    CreatedAt    time.Time
    UpdatedAt    time.Time
}

type Episode struct {
    ID             int64
    SeriesID       int64
    SeasonID       int64
    SeasonNumber   int
    EpisodeNumber  int
    AbsoluteNumber int    // 0 = none (nullable in DB)
    TMDBID         int64
    TVDBID         int64
    Title          string
    Overview       string
    AirDate        string // "YYYY-MM-DD" | ""
    Runtime        int
    StillPath      string
    Status         MediaStatus
    Monitored      bool
    HasFile        bool
    JobID          int64  // 0 = none
    LibraryPath    string
    MetadataJSON   string
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

type Movie struct {
    ID                  int64
    TMDBID              int64
    IMDBID              string
    Title               string
    SortTitle           string
    Year                int
    Overview            string
    TMDBStatus          string
    MinimumAvailability string // "announced"|"inCinemas"|"released"|"preDB"
    ReleaseDate         string // "YYYY-MM-DD" | ""
    DigitalRelease      string
    PhysicalRelease     string
    Runtime             int
    Status              MediaStatus
    Monitored           bool
    HasFile             bool
    QualityProfileID    int64
    RootFolderPath      string
    LibraryPath         string
    JobID               int64 // 0 = none
    PosterPath          string
    BackdropPath        string
    MetadataJSON        string
    LastMetadataSync    *time.Time
    AddedAt             time.Time
    CreatedAt           time.Time
    UpdatedAt           time.Time
}
```

### 4.3 Notification / WebDAVItem / settings / servarr structs

```go
// internal/notify/types.go
type Notification struct {
    ID        int64
    Type      string // "download_completed"|"grab_failed"|... (see 006)
    Payload   string // JSON
    JobID     int64  // 0 = none (nullable FK)
    Read      bool
    CreatedAt time.Time
    ReadAt    *time.Time
}

// internal/webdav/types.go  (or internal/media — see 01)
type WebDAVItem struct {
    ID         int64
    Name       string
    RemotePath string
    Size       int64
    Category   string // "movie"|"series"|"unknown"
    Known      bool
    JobID      int64  // 0 = none
    IsBroken   bool
    FirstSeen  time.Time
    LastSeen   time.Time
    CreatedAt  time.Time
}

// internal/servarr/types.go  (Seerr emulation backing rows, 05)
type QualityProfile struct {
    ID        int64
    Name      string
    IsDefault bool
    CreatedAt time.Time
}
type RootFolder struct {
    ID        int64
    Path      string
    MediaKind string // "tv" | "movie"
    CreatedAt time.Time
}
type Tag struct {
    ID        int64
    Label     string
    CreatedAt time.Time
}
```

---

## 5. Store methods to add (signatures + SQL sketches)

All new methods hang off the existing `*store.Store`, reuse the `findOne`-style helpers, the `nullStr/nullInt/nullTime` boundary helpers, and the **full-row Update** pattern (baseline convention — one canonical column list per table, scanned in fixed order). **App-level dedup + single-writer are preserved**: there are no new UNIQUE-on-hash constraints, and every method runs under `SetMaxOpenConns(1)`.

### 5.1 Extended `jobs` access (004)

The canonical `jobColumns` constant **gains the six new columns appended in physical order** (so existing scan order is undisturbed; new columns trail):

```go
// jobColumns (extended) — append, never reorder:
// ...existing 22 columns..., protocol, media_type, media_ref, torrent_magnet, torrent_hash, torrent_file
```

`UpdateJob` is extended to rewrite the six new columns too (single full-row update — baseline convention; `store.go:187-195`). New finders mirror `FindBySHA256`:

```go
// FindByTorrentHash mirrors FindBySHA256 — app-level dedup, most-recent wins, category-scoped.
func (s *Store) FindByTorrentHash(ctx context.Context, hash, category string) (*job.Job, error)
//   -> findOne(ctx, "torrent_hash=? AND category=?", hash, category)
//   SELECT <jobColumns> FROM jobs WHERE torrent_hash=? AND category=? ORDER BY id DESC LIMIT 1
//   returns (nil, nil) on sql.ErrNoRows  (the dedup-before-insert lookup)

// FindJobByMedia — the job currently linked to a catalog item (powers status reconcile).
func (s *Store) FindJobByMedia(ctx context.Context, mediaType string, mediaRef int64) (*job.Job, error)
//   -> findOne(ctx, "media_type=? AND media_ref=?", mediaType, mediaRef)

// CreateJob extended: INSERT now also sets protocol, media_type, media_ref, torrent_magnet, torrent_hash, torrent_file.
func (s *Store) CreateJob(ctx context.Context, j *job.Job) (int64, error)
//   INSERT INTO jobs (state, category, nzb_name, nzb_content, nzb_url, nzb_sha256,
//     protocol, media_type, media_ref, torrent_magnet, torrent_hash, torrent_file)
//   VALUES (?,?,?,?,?,?, ?,?,?,?,?,?)
```

**Dedup-before-insert at grab time (locked):** torrent grabs call `FindByTorrentHash(hash, cat)` first and fall back to `FindByURL(magnet, cat)`; usenet grabs keep calling `FindBySHA256` then `FindByURL` (unchanged). Existing job returned → no double-submit (FR-GP-5).

### 5.2 Catalog CRUD + wanted-episode queries (005)

```go
// --- series ---
func (s *Store) CreateSeries(ctx, *media.Series) (int64, error)        // INSERT, returns id
func (s *Store) GetSeries(ctx, id int64) (*media.Series, error)
func (s *Store) GetSeriesByTMDB(ctx, tmdbID int64) (*media.Series, error)   // findOne "tmdb_id=?"
func (s *Store) GetSeriesByTVDB(ctx, tvdbID int64) (*media.Series, error)   // for Sonarr lookup (05)
func (s *Store) UpdateSeries(ctx, *media.Series) error                  // full-row rewrite, updated_at=CURRENT_TIMESTAMP
func (s *Store) ListSeries(ctx) ([]*media.Series, error)               // ORDER BY sort_title
func (s *Store) DeleteSeries(ctx, id int64) error                       // DELETE -> CASCADE seasons/episodes

// --- season ---
func (s *Store) UpsertSeason(ctx, *media.Season) (int64, error)
//   INSERT ... ON CONFLICT(series_id, season_number) DO UPDATE SET
//     monitored=excluded.monitored, episode_count=excluded.episode_count, air_date=excluded.air_date, ...
func (s *Store) ListSeasons(ctx, seriesID int64) ([]*media.Season, error)
func (s *Store) SetSeasonMonitored(ctx, id int64, monitored bool) error

// --- episode ---
func (s *Store) UpsertEpisode(ctx, *media.Episode) (int64, error)
//   INSERT ... ON CONFLICT(series_id, season_number, episode_number) DO UPDATE SET
//     title=excluded.title, air_date=excluded.air_date, absolute_number=excluded.absolute_number, ...
//   (metadata refresh path, FR-CAT-5 — does NOT clobber status/has_file/job_id/library_path)
func (s *Store) GetEpisode(ctx, id int64) (*media.Episode, error)
func (s *Store) ListEpisodes(ctx, seriesID int64) ([]*media.Episode, error)
func (s *Store) UpdateEpisode(ctx, *media.Episode) error                // full-row rewrite (status, has_file, job_id, library_path)
func (s *Store) SetEpisodeStatus(ctx, id int64, st media.MediaStatus) error // targeted: UPDATE episode SET status=?, updated_at=CURRENT_TIMESTAMP WHERE id=?

// Wanted-episode query — air-date-aware (FR-CAT-4): monitored, no file, eligible (aired), series+season monitored.
func (s *Store) WantedEpisodes(ctx) ([]*media.Episode, error)
//   SELECT e.<episodeColumns> FROM episode e
//   JOIN series sr ON sr.id = e.series_id
//   JOIN season sn ON sn.id = e.season_id
//   WHERE e.monitored=1 AND sn.monitored=1 AND sr.monitored=1
//     AND e.has_file=0
//     AND e.status IN ('wanted','missing','expired_broken')
//     AND e.air_date IS NOT NULL AND e.air_date <> '' AND e.air_date <= date('now')
//   ORDER BY e.air_date

// --- movie ---
func (s *Store) CreateMovie(ctx, *media.Movie) (int64, error)
func (s *Store) GetMovie(ctx, id int64) (*media.Movie, error)
func (s *Store) GetMovieByTMDB(ctx, tmdbID int64) (*media.Movie, error)  // Radarr lookup (05)
func (s *Store) UpdateMovie(ctx, *media.Movie) error
func (s *Store) ListMovies(ctx) ([]*media.Movie, error)                 // ORDER BY sort_title
func (s *Store) DeleteMovie(ctx, id int64) error
func (s *Store) SetMovieStatus(ctx, id int64, st media.MediaStatus) error
func (s *Store) WantedMovies(ctx) ([]*media.Movie, error)
//   SELECT <movieColumns> FROM movie
//   WHERE monitored=1 AND has_file=0 AND status IN ('wanted','missing','expired_broken')
//     AND release_date IS NOT NULL AND release_date <> '' AND release_date <= date('now')
//   ORDER BY release_date
```

**Conventions enforced:** `Create*` for catalog singletons (caller checks `Get*ByTMDB` first — there is a UNIQUE on `tmdb_id`, so a duplicate INSERT errors loudly, which is the desired catalog-identity guard); `Upsert*` for season/episode metadata-refresh (idempotent, preserves lifecycle columns). `SetXStatus` are the targeted single-column updates the reconciler uses for hot status flips (a deliberate, narrow exception to the full-row rule — kept narrow to avoid clobbering concurrent metadata writes).

### 5.3 Notification enqueue / list / mark-read (006)

```go
func (s *Store) EnqueueNotification(ctx, n *notify.Notification) (int64, error)
//   INSERT INTO notification (type, payload, job_id) VALUES (?, ?, ?)   // read defaults 0
func (s *Store) ListNotifications(ctx, unreadOnly bool, limit int) ([]*notify.Notification, error)
//   SELECT <cols> FROM notification [WHERE read=0] ORDER BY created_at DESC, id DESC LIMIT ?
func (s *Store) UnreadCount(ctx) (int64, error)
//   SELECT COUNT(*) FROM notification WHERE read=0     // badge count (FR-NC-4)
func (s *Store) MarkNotificationRead(ctx, id int64) error
//   UPDATE notification SET read=1, read_at=CURRENT_TIMESTAMP WHERE id=?
func (s *Store) MarkAllNotificationsRead(ctx) error
//   UPDATE notification SET read=1, read_at=CURRENT_TIMESTAMP WHERE read=0
```

### 5.4 WebDAV item upsert / list (007)

```go
func (s *Store) UpsertWebDAVItem(ctx, w *webdav.WebDAVItem) error
//   INSERT INTO webdav_item (name, remote_path, size, category, known, job_id, is_broken, last_seen)
//   VALUES (?,?,?,?,?,?,?, CURRENT_TIMESTAMP)
//   ON CONFLICT(remote_path) DO UPDATE SET
//     name=excluded.name, size=excluded.size, category=excluded.category,
//     known=excluded.known, job_id=excluded.job_id, is_broken=0,
//     last_seen=CURRENT_TIMESTAMP
//   (mirrors UpsertImportedSymlink exactly — the reconciler re-sees each path every sweep)
func (s *Store) ListWebDAVItems(ctx) ([]*webdav.WebDAVItem, error)       // ORDER BY name
func (s *Store) ListUnknownWebDAVItems(ctx) ([]*webdav.WebDAVItem, error) // WHERE known=0 (FR-WD-3 -> notifications)
func (s *Store) MarkWebDAVItemsBrokenNotSeenSince(ctx, sweep time.Time) (int64, error)
//   UPDATE webdav_item SET is_broken=1 WHERE last_seen < ?   // rows missed by this sweep are gone from the mount
func (s *Store) WebDAVUsageBytes(ctx) (int64, error)
//   SELECT COALESCE(SUM(size),0) FROM webdav_item WHERE is_broken=0   // storage overview (FR-ST-1)
```

### 5.5 Settings get / set (008)

```go
func (s *Store) GetSetting(ctx, key string) (string, bool, error)
//   SELECT value FROM settings WHERE key=?    // (value, found, err)
func (s *Store) SetSetting(ctx, key, value string) error
//   INSERT INTO settings (key, value) VALUES (?, ?)
//   ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=CURRENT_TIMESTAMP
func (s *Store) AllSettings(ctx) (map[string]string, error)            // settings UI bulk read
```

### 5.6 Servarr reads + root-folder upsert (009)

```go
func (s *Store) ListQualityProfiles(ctx) ([]*servarr.QualityProfile, error) // GET /api/v3/qualityprofile (05)
func (s *Store) ListRootFolders(ctx, kind string) ([]*servarr.RootFolder, error) // kind "tv"|"movie"|"" (all)
func (s *Store) ListTags(ctx) ([]*servarr.Tag, error)                  // GET /api/v3/tag (often [])
func (s *Store) UpsertRootFolder(ctx, id int64, path, kind string) error
//   INSERT INTO root_folder (id, path, media_kind) VALUES (?, ?, ?)
//   ON CONFLICT(id) DO UPDATE SET path=excluded.path, media_kind=excluded.media_kind
//   (settings UI re-points the seeded root when BOXARR_*_LIBRARY_ROOT changes — 009 quirk 2)
func (s *Store) CreateTag(ctx, label string) (int64, error)            // lazy tag creation if Seerr references one
```

---

## 6. Migration safety notes (referencing the 00 register)

1. **`ADD COLUMN NOT NULL` requires `DEFAULT`.** SQLite rejects adding a `NOT NULL` column without a default to a populated table; every such column in `004` (`protocol`) and the catalog/notification/webdav tables that use `NOT NULL` on *new tables* (fine — no rows yet) carries one. `protocol TEXT NOT NULL DEFAULT 'usenet'` back-fills all existing usenet rows correctly. (Confirmed valid-with-default; runtime-verify item *"ALTER ADD COLUMN with NOT NULL DEFAULT works on populated tables"* — 00 §9 / `01-sab-store-schema.md`.)
2. **goose default version table.** goose v3 is not given an override, so it uses **`goose_db_version`**; new files `004`–`009` are applied with the existing `goose.Up(db, "migrations")` call — no code change beyond adding the `.sql` files (the `//go:embed migrations/*.sql` glob picks them up automatically). Confirm the table name at runtime against the live goose version (00 §9 — "Confirm goose default version table name").
3. **Driver vs dialect mismatch is benign.** `modernc.org/sqlite` registers as `"sqlite"` while goose's dialect is `"sqlite3"`; goose only needs the `*sql.DB` handle, so new migrations work unchanged (00 §9 — sab2torbox foundation note). Sanity-check when first running `004`.
4. **`Down` migrations are exact inverses** and drop indexes before columns (SQLite drops dependent indexes with the table, but explicit `DROP INDEX` before `DROP COLUMN` in `004` keeps the down path deterministic). Catalog/notification/webdav/settings/servarr downs simply `DROP TABLE` in FK-reverse order (children before parents) so `ON DELETE CASCADE` parents drop last.
5. **No DB `CHECK` on either status vocabulary.** `jobs.state` and `movie.status`/`episode.status` stay plain `TEXT` — validity is enforced in Go (Assumption C). This lets the new `seeding` state and any future status be added without a migration.
6. **App-level dedup preserved end-to-end.** No migration adds a UNIQUE to `nzb_sha256` or `torrent_hash`; the only UNIQUEs introduced are *identity* keys (`series.tmdb_id`, `movie.tmdb_id`, `season(series_id,season_number)`, `episode(series_id,season_number,episode_number)`, `webdav_item.remote_path`, `root_folder.path`, `tag.label`, `settings.key`), each chosen because the row is a genuine singleton, not a download artifact (00 §9 — the index-only-vs-UNIQUE decision is resolved here in favor of index-only for hashes, UNIQUE for identities).

## Definition of done

Boxarr's persistence layer is done when: the three baseline migrations are present byte-for-byte and `goose.Up` applies `004`–`009` cleanly on both a fresh DB and a DB already at version `003` (the `protocol` back-fill leaves every legacy job as `'usenet'`); the extended `Job` round-trips all six new columns through `CreateJob`/`GetJob`/`UpdateJob` with `FindByTorrentHash` deduping exactly as `FindBySHA256` does (most-recent, category-scoped, `nil` on miss, **no** UNIQUE constraint); the catalog tables enforce identity (`tmdb_id` UNIQUE) while cascading deletes from `series`→`season`→`episode` and SET-NULLing `job_id` on job deletion; `WantedEpisodes`/`WantedMovies` return only monitored, file-less, **aired/released** items via lexicographic `air_date <= date('now')`; `movie.status`/`episode.status` carry the six-value `MediaStatus` enum independently of `jobs.state`'s twelve-value machine, with the reconciler reconciling the two; notifications, webdav items, settings, and the seeded servarr tables all upsert via their declared `ON CONFLICT` keys under `SetMaxOpenConns(1)`; and `gofmt -s`/`golangci-lint` pass with every new exported store method and domain type documented. Cross-refs: `00-decisions-and-assumptions.md` (§5.4 polymorphic ref, Assumption C), `01-architecture-and-packages.md` (package placement), `05-seerr-emulation.md` (servarr id consumers), `06-pipelines.md` (status/state transitions, season-pack mapping), `04-internal-api.md` (status roll-ups, notification payload shapes).
