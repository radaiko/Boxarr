# Boxarr — Decisions & Assumptions (Spec 00)

**Date:** 2026-06-20
**Status:** Approved for planning
**Role:** This is the **canonical foundation** for the companion specs `01`–`09`. Where any companion spec conflicts with this document, this document wins. `docs/boxarr-requirements.md` remains the high-level requirements (the "what"); the `docs/specs/*` set is the "predefined" detail (the "how").

This document is grounded in a deep read of the real `radaiko/sab2torbox` codebase (cloned at review time) and in fetched, primary-source contracts for every external system. Anything that could **not** be confirmed from source is collected in the [Runtime-verify register](#9-runtime-verify-register) rather than guessed silently.

---

## 1. Product framing

Boxarr is a single self-hosted application that **replaces three tools at once**:

- **sab2torbox** — it absorbs and evolves that codebase (TorBox client, SQLite store, symlink/heal/delete/reaper workers, rate-limit handling, CI, distroless image). sab2torbox's SABnzbd download-client emulation is **removed** (see Assumption B).
- **Sonarr** — series catalog, monitored seasons/episodes, search, grab, import, lifecycle.
- **Radarr** — the same for movies.

It is single-operator and local-first (requirements §3). Indexers are delegated to **Prowlarr**, catalog/metadata to **TMDB + TVDB**, and final filename→episode parsing assistance to Plex; what Boxarr owns is orchestration plus a lifecycle model that Sonarr/Radarr structurally lack (content lives on TorBox and can disappear).

---

## 2. Stack decisions (locked)

| Layer | Decision | Rationale |
|---|---|---|
| **Backend** | **Go**, evolving the `sab2torbox` codebase in this repo | Reuses the entire proven pipeline; Go is also the language the implementer (Claude) writes most reliably for concurrent-I/O orchestration. The two factors align. |
| **Go version** | **1.25** (match `sab2torbox`'s `go 1.25.7` directive, `golang:1.25-alpine` builder, `setup-go` `1.25`) | Live code/CI/Dockerfile already on 1.25; standardize there. |
| **Frontend** | **React + TypeScript + Vite**, built to static assets and **embedded in the Go binary via `embed.FS`** (no Node at runtime) | The stack the implementer is most fluent in; all supply-chain hardening (§17.2 of requirements) is framework-independent and still applies. |
| **Persistence** | **SQLite** (`modernc.org/sqlite`, CGo-free) + **goose v3** embedded migrations | Reused verbatim from sab2torbox. |
| **HTTP router** | **chi v5** (`github.com/go-chi/chi/v5`) | Already used; promote from indirect to direct dependency. |
| **Config** | `kelseyhightower/envconfig`, prefix changed `sab2torbox` → **`boxarr`** (all vars become `BOXARR_*`) | Reused mechanism. |
| **Deploy** | Single multi-arch distroless image (`gcr.io/distroless/static-debian12:nonroot`, `CGO_ENABLED=0`), published to `ghcr.io/radaiko/boxarr` | Reused; add one frontend build stage. |

**Rust was considered and rejected** for the backend: this is I/O-bound orchestration (no hot CPU path), async-Rust friction is high for the worker/poller pattern, and it would discard the proven Go foundation, turning a mostly-additive project into a full rewrite of the riskiest code. (Rationale recorded in memory `implementer-effectiveness-stack`.)

---

## 3. Resolved open decisions (requirements §19)

| § | Decision | Notes |
|---|---|---|
| 19.1 Backend & repo | **Go, evolve `sab2torbox` in this repo** | Module renamed (Assumption A). |
| 19.2 Frontend | **React + TypeScript + Vite** (was Svelte) | Supersedes the Svelte choice in requirements §4.1/§17.2/§19.2; those sections to be updated. Supply-chain controls (§17.2) carried over and re-expressed for the React/pnpm toolchain in `08`. |
| 19.3 Metadata | **TMDB (primary) + TVDB (supplement)** | TMDB for catalog/posters/movie+TV; TVDB for scene/absolute episode ordering and because Seerr's Sonarr surface keys on TVDB ids. See `03`, `05`. |
| 19.4 Heal strategy | **Resubmit stored artifact AND fresh-Prowlarr-re-search fallback** | Resubmit first (reuse existing healer); on dead artifact, re-search Prowlarr and best-match repoint. See `06`. |
| 19.5 Torrent WebDAV path | **Empirical — configurable, runtime-verified** | Default: torrents surface under the same flat mount root as usenet; expose `BOXARR_WEBDAV_TORRENT_SUBPATH` (default empty) mirroring the existing `WEBDAV_USENET_SUBPATH`. See [register](#9-runtime-verify-register). |
| 19.6 Name | **Boxarr** | `github.com/radaiko/boxarr`, `ghcr.io/radaiko/boxarr`. |

---

## 4. Load-bearing assumptions

- **A — Repo evolution.** This repository *is* the evolved sab2torbox. Its Go packages (`internal/torbox`, `internal/store`, `internal/worker`, `internal/job`, `internal/config`, `internal/api`) are copied in and the module path is renamed **`github.com/radaiko/sab2torbox` → `github.com/radaiko/boxarr`**. The binary/cmd is renamed `cmd/sab2torbox` → `cmd/boxarr`. sab2torbox is **not** imported as a dependency.
- **B — SAB emulation dropped.** The entire SABnzbd download-client surface (modes `version/get_config/fullstatus/addurl/addfile/queue/history` + delete, served at `/api` and `/sabnzbd/api`, plus all SAB response structs in `responses.go` and `SAB_API_KEY`) is **removed**. Boxarr is the replacement for Sonarr/Radarr, so nothing pushes NZBs to it via SAB; Seerr reaches it through the Sonarr/Radarr v3 emulation (`05`) and the grab pipeline drives TorBox directly.
- **C — Reuse the foundation verbatim where it works.** Keep, unchanged in behavior: the goose+`//go:embed` migration pattern; the SQLite DSN (`busy_timeout(5000)` + `journal_mode(WAL)` + `foreign_keys(1)` + `_txlock=immediate`) with `SetMaxOpenConns(1)`; **application-level dedup-before-insert** (no `UNIQUE` on hashes); the Go-side job state machine (no DB `CHECK` constraints); the `{success,error,detail,data}` envelope, `FlexInt`, and rate-limit helpers (`RateLimit`, `Retryable`, `parseRetryAfter`); the `healthcheck` CLI subcommand pattern for the distroless `HEALTHCHECK`; the `Server`-with-injected-deps + `chi` router shape; the symlink heal/repoint/reaper machinery.
- **D — Same-path WebDAV mount.** Every container that resolves media (Boxarr, Plex) mounts the TorBox WebDAV path at the **identical absolute path**, or symlinks dangle (requirements §11.1). This is a deployment contract, not enforced in code today; Boxarr **may** add an explicit guard.

---

## 5. Key implementation decisions derived from grounding

These are decided here so the companion specs don't re-litigate them:

1. **Import model (reframes requirements §11.1/§11.6).** Boxarr is its own importer — there is no external Sonarr/Radarr move-import step. Boxarr writes the **library symlink directly at its final Plex path**, pointing at the file inside the flat WebDAV release folder. The historical sab2torbox `_incoming` symlink-farm + literal "rename within one filesystem, never copy" constraint is therefore superseded by two simpler invariants: (a) the library symlink targets the WebDAV release file by absolute path; (b) the WebDAV mount is at the same absolute path in every container (Assumption D). The existing broken-symlink detection + atomic repoint (`atomicReplaceSymlink`) and the empty-dir `reaper` are reused on the library symlinks. `06` details this; `01` places it in the package map.
2. **Release-name parsing (requirements §11.4) — pure-Go, no sidecar.** Primary `github.com/chill-institute/torrentname@v1.4.0` (MIT, zero-dep, has `EpisodeEnd`/`Complete`), supplemented by `github.com/nssteinbrenner/anitogo@v1.0.0` (MPL-2.0, anime absolute) and **three in-house regexes** Boxarr must own: adjacent multi-episode `S01E01E02`, daily-show `YYYY-MM-DD`, and bare season packs (`S01` with no `COMPLETE` keyword). The Python/JS `guessit`/`parsett` route is rejected — it breaks the distroless single-binary deploy. Season-pack→episode mapping uses the parser's `Complete`/`Season` then the TVDB/TMDB episode list. `06` owns the algorithm + golden tests.
3. **Seerr emulation must advertise a Sonarr *v3* version string (`3.x.x.x`, e.g. `3.0.10.1567`).** A `4.x` `system/status.version` makes Overseerr/Jellyseerr skip `languageprofile` and breaks the v3 contract this spec targets; Radarr emulation advertises a Radarr version. `05` owns exact payloads. Auth accepts the api key via **both** `X-Api-Key` header and `apikey` query param.
4. **Polymorphic media reference.** SQLite has no polymorphic FKs, so `jobs` references media via the pair `(media_type, media_ref)` where `media_ref` is `movie.id` or `episode.id`. Catalog tables (`series`/`season`/`episode`/`movie`) use real FKs with `ON DELETE CASCADE` (matching the `imported_symlinks` precedent). `02` owns the DDL.
5. **`/user/me` and torrent field names are not in the existing code and only partly documented.** `03` pins the most-likely shapes from TorBox docs/SDKs and flags the rest in the register; the client maps unknown fields defensively (everything through `FlexInt`/optional fields).
6. **Health check repurposed.** `/healthz` keeps its shape but its `Pinger` checks the dependencies Boxarr actually needs (DB + TorBox token; optionally Prowlarr/TMDB reachability) rather than the SAB path. The 5-minute TorBox ping cache is kept.

---

## 6. Conventions (apply across all specs)

- **Module path:** `github.com/radaiko/boxarr`. **Binary/cmd:** `cmd/boxarr` → `/boxarr`. **DB default:** `BOXARR_DATABASE_PATH=/config/boxarr.db`.
- **Env prefix:** `BOXARR_*` via `envconfig.Process("boxarr", &c)`. Full reference lives in `08`. Slice vars stay comma-separated (proven by `Categories`).
- **HTTP namespaces (no collisions):**
  - `/api/v1/...` — Boxarr's **own** JSON/REST API consumed by the React SPA (`04`).
  - `/sonarr/api/v3/...` and `/radarr/api/v3/...` — the Seerr emulation surfaces (`05`).
  - `/` — the embedded React SPA (static assets via `embed.FS`).
  - `/healthz` — liveness/readiness (kept).
- **Library layout (Plex-standard, requirements §11.2):**
  - Movies: `<MOVIE_LIBRARY_ROOT>/<Title> (<Year>)/<Title> (<Year>).<ext>`
  - TV: `<TV_LIBRARY_ROOT>/<Series Title> (<Year>)/Season <NN>/<Series Title> - S<NN>E<MM> - <Episode Title>.<ext>`
  - Zero-padded season/episode numbers; sanitize illegal path chars. `06` owns the exact namer + edge cases (multi-episode files, anime, daily).
- **State machine:** extend the existing 11-state Go enum *in Go only* (no DB `CHECK`). New states (if any, e.g. torrent `seeding`) are added to the `transitions` map. `02`/`06` own this.
- **Migrations:** continue sequential goose files `004_*.sql`, `005_*.sql`, … with `-- +goose Up`/`-- +goose Down`; `ALTER TABLE ADD COLUMN` must carry a `DEFAULT` so it applies to existing rows. `02` owns the list.
- **Spec voice (match `sab2torbox`'s design spec):** header block `**Date:** / **Status:**`; terse, decisive engineering prose; **bold** for invariants; tables for config/endpoints; numbered lists for sequential behavior and "quirks to bake in"; fenced SQL/JSON/Go for concrete artifacts; `(locked)` / `(verified <source>)` parentheticals; end substantial specs with a one-paragraph "Definition of done".
- **Coding standards (carried from sab2torbox):** `gofmt -s` + `golangci-lint` clean; all exported symbols documented; no `panic` outside `main`; wrapped errors `fmt.Errorf("doing X: %w", err)`; structured `slog` carrying `job_id`/`torbox_id`/`tmdb_id`; context propagated everywhere.

### Document map (cross-link targets)

| File | Owns |
|---|---|
| `00-decisions-and-assumptions.md` | this — decisions, assumptions, conventions, runtime-verify register |
| `01-architecture-and-packages.md` | Go package layout (reuse/extend/new), worker topology, embed serving, repo evolution mechanics |
| `02-data-model.md` | full SQLite DDL — new migrations `004+`, all tables, state machine, store methods |
| `03-external-contracts.md` | pinned TorBox / Prowlarr / TMDB / TVDB / Plex request+response contracts |
| `04-internal-api.md` | Boxarr's own `/api/v1` REST surface consumed by the SPA |
| `05-seerr-emulation.md` | Sonarr v3 + Radarr v3 inbound emulation (`/sonarr`, `/radarr`) |
| `06-pipelines.md` | grab / import / delete / heal / reconcile state machines, selection score, naming, parsing, limits |
| `07-frontend.md` | React+TS+Vite SPA structure, routes/views, build + supply-chain config |
| `08-config-deploy-ci.md` | `BOXARR_*` env reference, Dockerfile, CI/release workflows, compose |
| `09-phase-plan.md` | phased delivery with acceptance criteria + FR-traceability matrix |

---

## 7. What is reused vs. new (one-line index; `01` expands)

- **Reused ~as-is:** `internal/torbox` envelope/FlexInt/rate-limit/usenet methods; `internal/store` DSN/goose/dedup/`imported_symlinks`; `internal/job` state machine; `internal/worker` poller/submitter/webdav-resolve/healer/deleter/reaper/clock; `internal/api` server shape + `/healthz` + `writeJSON` + constant-time api-key check; `cmd` healthcheck subcommand; CI/release/Dockerfile skeleton.
- **Extended:** TorBox client (+torrents, +`/user/me`, +`checkcached`); store/schema (+catalog/notification/webdav/settings, +torrent job fields); workers (+torrent submit/poll, +Prowlarr-re-search heal, +torrent delete, +reconciler); config (`BOXARR_*` superset).
- **New:** Prowlarr client; TMDB client; TVDB client; Plex client; metadata/catalog service; selection/scoring; importer + namer + release-name parser; reconciler + notification center; Boxarr `/api/v1` REST; Seerr `/sonarr` + `/radarr` emulation; React SPA + embed; frontend build stage + CI step.
- **Dropped:** SABnzbd emulation surface + `SAB_API_KEY` + SAB response structs (Assumption B).

---

## 8. Out of scope (carried from requirements §3)

Indexer protocols/scrapers (Prowlarr owns them); Sonarr's full custom-formats engine (only a simple configurable score, `06`); rewriting the proven usenet pipeline; downloading media bytes to local disk (imports are symlinks only); any public/multi-tenant service; availability back-sync to Seerr (Seerr derives availability from Plex — requirements §10 FR-SEERR-7).

---

## 9. Runtime-verify register

Every item below was flagged by grounding research as **not confirmable from source**. Each must be verified against a live instance during implementation; none should be silently assumed. The spec encodes the most-likely behavior and a defensive fallback.

### TorBox
- `/torrents/createtorrent` success-data field name (`torrent_id` vs `id` vs `queued_id`) — confirmed only for usenet (`usenetdownload_id`). Decode defensively.
- Torrent `mylist` item field set (`seeds`, `peers`, `ratio`, `upload_speed`, `owner`, `private`, `tags`, `alternative_hashes`, `files[].opensubtitles_hash`) — inferred by analogy + third-party SDKs; verify against a live response.
- `checkcached` response shape (`format=list` array vs object keyed by hash; key case; `files[]` field set; empty = `{}` vs `null`) — verify.
- `controltorrent` valid `operation` strings (`delete|pause|resume|reannounce`) and the `all` boolean — verify.
- `createtorrent` multipart field names (`magnet`, `file`, `name`, `allow_zip`, `as_queued`, `seed`) and hash>magnet>file precedence — verify.
- Whether torrent endpoints accept the same `bypass_cache`/`limit` query params as usenet — verify.
- `/user/me` field names (plan tier + integer ordering, active-slot limit per tier, monthly usage/cap, cooldown/expiry, subscription, `settings` shape) — endpoint is unused in existing code; verify entirely.
- Exact torrent failure/state strings (e.g. is it literally `stalled (no seeds)` or just `stalled`) — verify; `Failed()` already matches on `failed`/`error`/`stalled` substrings.

### Torrent WebDAV path
- Whether torrent downloads surface under the same flat mount root as usenet or a separate subpath (requirements §19.5) — verify on the live mount; `BOXARR_WEBDAV_TORRENT_SUBPATH` (default empty) covers both.

### Prowlarr
- `downloadVolumeFactor`/`uploadVolumeFactor` presence in the JSON REST `ReleaseResource` (absent from official SDKs; seen lowercase in some feeds) — verify.
- `leechers` vs `peers` JSON key in release results — verify.
- Multi-category passing: comma-separated returns HTTP 400; use **repeated** `&categories=` keys; pass `indexerIds=-1` explicitly for "all" — verify both.
- `type` param value for movie search (`movie` vs `search`/`tvsearch`/`moviesearch`) — inspect the instance Swagger.
- Whether `magnetUrl` is populated independent of the per-indexer `PreferMagnetUrl` setting — verify; always fall back to `downloadUrl`.
- No documented cache-bust for the ~30-min result cache — verify behavior.

### TMDB
- Always call `/configuration` at startup for image base URL + sizes (don't hardcode).
- `/find?external_source=tvdb_id` resolves series-level TVDB ids; behavior for season-specific TVDB ids unconfirmed — test.
- TV `status` string values and any integer↔string mapping — read the live string; don't rely on integer codes.
- Movies generally have no `tvdb_id` in `external_ids` — confirm before any cross-ref.
- Read the actual `Retry-After` on 429; treat ~40 req/s as the safe ceiling.

### TVDB (v4)
- `SeasonType` numeric ids (`1` official, `2` dvd, `3` absolute, `4` alternate, `regional`=?) — call `GET /seasons/types` and cache.
- Whether `absoluteNumber` is populated for non-`absolute` orderings — test; match the TMDB cross-id by `sourceName` string (`TheMovieDB.com`) rather than the numeric `type`.
- Token lifetime ("~1 month") — read the JWT `exp` claim; v4 rate limits undocumented.
- Whether `/series/{id}/extended?meta=episodes` returns episodes for all orderings or only the default — may need explicit `/episodes/{season-type}` calls.
- `/search/remoteid` reliability for TMDB ids — prefer TMDB `/find` for tvdb→tmdb and TVDB only for ordering.

### Plex
- Path encoding for partial scan (`quote_plus` spaces→`+` vs percent-encoding) — verify with a path containing a space.
- Whether `path` may be any subdirectory of a section Location and error behavior when outside all Locations — verify.
- Minimum PMS version for `?path=` partial scan support (community: ~`1.20.0.3125`) — verify.
- Whether the newer `POST /library/sections/{id}/refresh` supports a partial-scan `path` — verify; default to the legacy `GET ...?path=`.
- Success status code (200 vs other 2xx) and any throttling on rapid scans — verify.

### Seerr (Overseerr/Jellyseerr ↔ Sonarr/Radarr v3 emulation)
- Advertise a **Sonarr `3.x.x.x`** / **Radarr v3** version string (a `4.x` Sonarr string changes Seerr's behavior). Verify against both Overseerr and Jellyseerr "Test".
- Jellyseerr reads `system/status` synchronously (fails the test on error) where Overseerr tolerates it — emulate a clean `200` either way.
- Return `201` (or `200`) from `POST /series` and `POST /movie` with the body echoing an assigned numeric `id` (Seerr checks `response.data.id`, not the status code, but Axios throws on 4xx/5xx).
- `GET /api/v3/episode?seriesId=` + `PUT /api/v3/episode/monitor` are only hit on the *update* path (series already exists) — support them if Boxarr's store already has the series.
- Return minimal `qualityprofile` `{id,name}` and `rootfolder` `{id,path,accessible,freeSpace}`; extra fields are ignored. Accept (and may ignore) Radarr's legacy `profileId` and `titleSlug` in the POST body.

### sab2torbox foundation
- Confirm goose default version table name (`goose_db_version`) at runtime; `modernc.org/sqlite` registers driver name `sqlite` (goose dialect stays `sqlite3`).
- Confirm `go.sum` exists when forking; promote `chi` to a direct `require`.
- The same-filesystem "rename" invariant is a deployment contract, not enforced in code (Assumption D / decision §5.1) — consider adding an explicit guard.
