# Boxarr — Phase Plan & FR Traceability (Spec 09)

**Date:** 2026-06-20
**Status:** Approved for planning

This spec owns **how Boxarr is delivered**: the phase sequence, each phase's concrete deliverables (referencing specs `01`–`08`), its **testable acceptance criteria**, the spec sections it implements, and a single **FR-traceability matrix** mapping every requirement in `docs/boxarr-requirements.md` to the spec(s) that define it and the phase that ships it. Cross-cutting decisions, naming, namespaces, and the runtime-verify register live in `00-decisions-and-assumptions.md` and are **not** re-decided here. This document does not introduce new behavior — it sequences the behavior already specified in `01`–`08`.

The sequencing principle (requirements §18, locked): **front-load end-to-end value, defer the hardest parsing and the optional automation.** Concretely — **Phase 0 ships a publishing image from the first commit**; **movies (Phase 1) before series (Phase 2)** because movies skip episode/season-pack parsing and so exercise the entire grab→import→delete spine fastest; **manual search before any automation** (FR-SR-1/2/3 in Phase 1, FR-SR-4 automation deferred to Phase 5).

---

## 1. Phase overview (the spine and what hangs off it)

| Phase | Theme | Net new value | Gating spec sections |
|---|---|---|---|
| **0** | Foundation | The repo *is* Boxarr: renamed, SAB dropped, schema migrated, all outbound clients exist, `/api/v1` + React shell + settings, **CI/release/Docker publish an image**. | `01` §2–6, `02` §3, `03` all, `04`, `07`, `08` all |
| **1** | Movies MVP (end-to-end) | A movie added from TMDB search is grabbed (usenet+torrent), polled to completion, imported as a library symlink, scanned into Plex, deletable with TorBox propagation; storage + WebDAV views. | `03`, `04`, `06` (grab/import/delete, no pack parsing), `02` (movie/job rows) |
| **2** | Series | Series/season/episode catalog; per-episode + season-pack search; pack→file episode mapping; TV import; split UI. | `02` (catalog), `06` (parsing + namer), `04`, `07` |
| **3** | Seerr | Sonarr v3 + Radarr v3 inbound emulation; search-on-add. | `05` all, `02` §3.6 (servarr tables), `06` (search) |
| **4** | Lifecycle & notifications | 15-min reconcile sweep; unknown-content detection; notification center; torrent-aware heal. | `06` (reconcile/heal), `02` (notification/webdav), `04`, `07` |
| **5** | Automation (optional) | Scheduled/RSS monitoring for ongoing series; auto-search of still-missing wanted items. | `06` (selection on schedule), `04`, `08` (intervals) |

**Each phase is independently shippable and leaves `main` green** (FR-CI-1): `gofmt -s` clean, `go vet`, `golangci-lint`, `go test ./... -race -cover`, `pnpm lint`, `pnpm build`, and a no-push `docker build` all pass on every PR. Because Phase 0 lands the release workflow, **every subsequent phase publishes a real `ghcr.io/radaiko/boxarr` image** on merge to `main` (FR-CI-2).

---

## 2. Phase 0 — Foundation

**Goal:** turn this repository from sab2torbox into Boxarr-the-skeleton — renamed, SAB-free, schema-migrated, every outbound client present, an authed `/api/v1` surface, an embedded React shell with a working **Settings** page, and a CI/release/Docker pipeline that **publishes a multi-arch image with the frontend embedded from the first commit.** No grab pipeline yet; this phase proves the chassis.

### Deliverables

1. **Module rename + SAB drop** (`01` §2 steps 1–9): `go.mod` module → `github.com/radaiko/boxarr`, every import path rewritten, `cmd/sab2torbox` → `cmd/boxarr` (binary `/boxarr`), env prefix `envconfig.Process("boxarr", …)` so all vars are `BOXARR_*`, `chi` promoted to a **direct** require (`v5.2.5`), Go pinned `1.25`, DB default `/config/boxarr.db`. **Delete the entire SABnzbd surface** — `handleAPI`, the `for _, base := range []string{"/api","/sabnzbd/api"}` loop, all `mode` handlers (`version/get_config/fullstatus/addurl/addfile/queue/history/delete`), the whole `internal/api/responses.go` (+`responses_test.go`), and `SAB_API_KEY` (verified `04-sab-api-config-ci.md` §2). **Keep** `internal/torbox`, `internal/store`, `internal/job`, `internal/worker` behavior-unchanged (only import paths move).
2. **Migrations `004`–`009`** (`02` §3): `004_protocol_media` (jobs `protocol`/`media_type`/`media_ref`/`torrent_*` + indexes), `005_catalog` (`series`/`season`/`episode`/`movie`), `006_notifications`, `007_webdav_items`, `008_settings`, `009_servarr` (seeded `quality_profile` id 1, `root_folder` ids 1/2, empty `tag`). All baseline migrations `001`–`003` kept byte-for-byte; `goose.Up` applies `004+` cleanly on a fresh DB **and** on a DB already at `003` (the `protocol NOT NULL DEFAULT 'usenet'` back-fill leaves every legacy job usenet). Store methods from `02` §5 land here so later phases call them.
3. **Outbound clients** (`03` all): extend `internal/torbox` with **torrents** (`createtorrent`/`mylist`/`controltorrent`), **`checkcached`**, and **`/user/me`** — all through the reused `do()`/`Envelope`/`FlexInt`/`RateLimit`/`Retryable`/`parseRetryAfter` infrastructure. New clients `internal/prowlarr` (`/api/v1/search` + `/api/v1/indexer`), `internal/metadata/tmdb` (configuration/search/movie/tv), `internal/metadata/tvdb` (v4 JWT, season types, ordering), `internal/plex` (partial-scan trigger). Each copies the TorBox `do()` shape (30 s client, `Retry-After` parsed every response, `*APIError`).
4. **`BOXARR_*` config superset** (`08`): drop `SAB_API_KEY`; add `BOXARR_PROWLARR_URL`/`_API_KEY`, `BOXARR_TMDB_API_KEY`, `BOXARR_TVDB_API_KEY`, `BOXARR_PLEX_URL`/`_TOKEN`, `BOXARR_MOVIE_LIBRARY_ROOT`, `BOXARR_TV_LIBRARY_ROOT`, `BOXARR_WEBDAV_TORRENT_SUBPATH` (default empty, `00` §19.5), the interval/selection/limit vars, and the Seerr keys. `Load()` keeps fail-fast `os.Stat` validation for the library roots, mirroring the `SymlinkRoot`/`HealLibraryRoots` pattern.
5. **`/api/v1` skeleton + auth + health repoint** (`04`, `01` §5): chi router serving `r.Mount("/api/v1", …)`, `/healthz` kept (Pinger repointed to DB + TorBox token, 5-min cache retained), `writeJSON` + the constant-time `subtle.ConstantTimeCompare` api-key check reused for `/api/v1` and Seerr auth. Phase-0 `/api/v1` endpoints: `GET /api/v1/health` summary, `GET/PUT /api/v1/settings`, `GET /api/v1/account` (proxies `/user/me`), and the search/catalog routes stubbed so the SPA can compile against a stable contract.
6. **React shell + Settings + embed** (`07`, `01` §5): a React+TS+Vite SPA under `web/`, built to `web/dist`, embedded via `//go:embed all:dist` in `internal/web` with an `index.html` SPA-fallback handler mounted **last**. Phase 0 ships the app frame (nav: Movies / Series / WebDAV / Storage / Notifications / Settings) plus a **working Settings page** that round-trips `GET/PUT /api/v1/settings`. A committed placeholder `internal/web/dist/index.html` keeps `go build` green before the frontend is built.
7. **CI + release + multi-stage Dockerfile with the frontend stage** (`08`, FR-CI-*): extend `ci.yml` with a `frontend` job (`pnpm/action-setup@v4`, `pnpm install --frozen-lockfile`, `pnpm lint`, `pnpm build`) and keep the Go stages verbatim (`gofmt -s -l`, `go vet`, `golangci-lint-action@v8`, `go test ./... -race -cover`) + the no-push `docker-build`. Splice a `node:22-alpine` **frontend stage** before the Go builder that `pnpm build`s to `/web/dist` and `COPY --from=frontend /web/dist ./internal/web/dist`; runtime stays `gcr.io/distroless/static-debian12:nonroot`, `CGO_ENABLED=0`, `HEALTHCHECK ["/boxarr","healthcheck"]`. `release.yml` renamed to `images: ghcr.io/radaiko/boxarr`, multi-arch `linux/amd64,linux/arm64`, semver+`latest` tags, `VERSION=${{ github.run_number }}` ldflags inject. Frontend supply-chain controls applied: exact-pinned deps, committed `pnpm-lock.yaml`, `.npmrc` `ignore-scripts=true`, `packageManager` pin (FR-SEC-1/3/6).

### Acceptance criteria (testable)

1. **Build identity:** `go build ./...` produces binary `/boxarr` from `cmd/boxarr`; `grep -r 'sab2torbox' --include='*.go'` returns **nothing**; `chi` appears in the direct `require` block; `go.mod` reads `module github.com/radaiko/boxarr` and `go 1.25.x`.
2. **SAB gone:** `curl /api?mode=version` and `curl /sabnzbd/api?mode=version` both **404** (routes deleted); the binary contains no `VersionResponse`/`AddResponse`/`QueueResponse` symbols; no env var named `BOXARR_SAB_API_KEY` is read.
3. **Migrations:** starting from a sab2torbox DB at goose version `003`, boot applies `004`–`009` to version `009` with zero errors; `SELECT protocol FROM jobs` returns `'usenet'` for every pre-existing row; `quality_profile` has id `1`, `root_folder` has ids `1` (`/data/tv`,`tv`) and `2` (`/data/movies`,`movie`).
4. **Clients reachable:** with valid creds, `GET /api/v1/health` reports DB+TorBox healthy; `GET /api/v1/account` returns the parsed `/user/me` plan/usage; a unit test exercises each new client's `do()` against a recorded fixture and decodes defensively (unknown fields ignored, IDs via `FlexInt`).
5. **SPA served + settings round-trip:** `GET /` returns the embedded React shell; a deep link `GET /movies/123` also returns the shell (index.html fallback) not 404; the Settings page loads current config via `GET /api/v1/settings` and a `PUT` persists to the `settings` table and survives restart.
6. **Pipeline publishes:** a push to `main` runs CI green (Go stages + `frontend` lint/build + no-push docker build that **exercises the frontend stage**) and the release workflow **pushes `ghcr.io/radaiko/boxarr:latest` and a `v{run_number}`-style tag** as a multi-arch image whose single layer embeds the built SPA; `docker run … /boxarr healthcheck` returns exit 0 against a running instance.

### Implements

`01` §2 (repo evolution), §3 (package layout), §4 (worker wiring scaffold), §5 (router + embed); `02` §1 (baseline kept), §3 (migrations `004`–`009`), §5 (store methods); `03` (all five clients); `04` (settings/health/account routes, auth); `07` (shell + Settings + embed); `08` (config superset, Dockerfile frontend stage, `ci.yml`/`release.yml`).

---

## 3. Phase 1 — Movies MVP (end-to-end)

**Goal:** prove the **entire spine** on the simplest media type. A movie has exactly one file and needs no episode/season-pack parsing, so it exercises catalog→search→grab→submit→poll→resolve→import→scan→delete→storage/WebDAV-view without the hardest code. This is the first phase a user can actually *use*.

### Deliverables

1. **Movie catalog from TMDB** (`02` §3.2 `movie`, `04`, `03` TMDB): `GET /api/v1/movies/lookup?term=…` (TMDB search), `POST /api/v1/movies` (add by `tmdbId`, persist via `CreateMovie`, `status` computed `wanted` vs `missing` by `release_date <= date('now')`), `GET /api/v1/movies`, `GET /api/v1/movies/{id}`, `PUT /api/v1/movies/{id}` (monitored toggle), `DELETE /api/v1/movies/{id}`.
2. **Manual interactive search** (`06` selection input, `04`, `03` Prowlarr): `GET /api/v1/movies/{id}/releases` (and free-text `GET /api/v1/search`) issuing a Prowlarr `/api/v1/search` scoped to title+year, returning **both** usenet and torrent releases with title/size/protocol/indexer/seeders/leechers/grabs/detected-quality and, for torrents, **cached status via `checkcached`** (FR-SR-2). User picks one to grab (FR-SR-3).
3. **Grab pipeline, both protocols** (`06` grab, `02` §5.1): on grab, **fetch and store the artifact locally** — `.nzb` (usenet) or magnet/`.torrent` (torrent) — onto the job row; **dedup-before-insert** (`FindBySHA256`/`FindByURL` for usenet, `FindByTorrentHash`/`FindByURL` for torrent) returns the existing job rather than double-submitting (FR-GP-5). Submit: usenet via existing `CreateUsenetDownload`; torrent via new `CreateTorrent` (magnet preferred, else file).
4. **Poll + WebDAV resolve** (`01` §4 torrent loops, `06`): the reused usenet submitter/poller plus the **new parallel torrent submitter/poller** with their **own** `torrentSubmitBackoffUntil`/`torrentMissingPolls` so a usenet 429 never pauses torrents. Pollers update progress/eta/state until `download_finished && download_present`, then resolve the release folder on the WebDAV mount (`TorrentPath() = Join(WebDAVMountRoot, BOXARR_WEBDAV_TORRENT_SUBPATH)` + `name`, default subpath empty) reusing the up-to-30s retry + optional `/refresh` (FR-GP-3/4).
5. **Movie importer + namer + Plex scan** (`06` import + `importer/namer`, `internal/plex`): build the library symlink **directly at its final Plex path** `<MOVIE_LIBRARY_ROOT>/<Title> (<Year>)/<Title> (<Year>).<ext>` pointing at the file in the flat WebDAV release folder (reusing `atomicReplaceSymlink`); set `movie.has_file=1`, `status='available'`, `library_path`; trigger a Plex partial scan on the affected path (FR-IMP-1/2/3/5/6). **Movies skip §11.4 pack parsing entirely** — single largest video file wins.
6. **Deletion with TorBox propagation** (`06` delete, `01` §4 deleter): `DELETE /api/v1/movies/{id}` (and a delete-file variant) removes the library symlink **and** deletes the underlying download on TorBox — branch on `protocol`: `ControlUsenet(id,"delete")` vs `ControlTorrent(id,"delete")` — reusing the `deleteGiveUpAttempts=60` retry + guarded `removeSymlinkDir` (FR-DEL-1).
7. **Storage overview + WebDAV view (movies)** (`02` §5.4, `04`): `GET /api/v1/storage` (total used = sum of `size` across usenet+torrent `mylist`s, plus plan tier/usage/cooldown from `/user/me`), `GET /api/v1/webdav` (one row per release folder, name+size, category from the DB when Boxarr submitted it). Both served **from the DB**, never by scanning the mount on page load (FR-UI-9).
8. **Movies UI** (`07`): poster-grid Movies library backed by TMDB artwork, a movie detail view with status (wanted/searching/downloading/available/expired-broken), the interactive release picker, plus first-cut Storage and WebDAV views.

### Acceptance criteria (testable)

1. **End-to-end golden path (the canonical acceptance scenario):** a movie added from TMDB search → its release list shows usenet+torrent results with cached badges → the user grabs a torrent → the job is polled to `completed` → it is **imported as a symlink at `<MOVIE_LIBRARY_ROOT>/<Title> (<Year>)/<Title> (<Year>).mkv`** that resolves to the WebDAV file → a Plex partial scan fires for that path → the movie shows **`available`** in the UI → deleting it removes the symlink **and** calls `controltorrent operation=delete` on TorBox, after which the WebDAV row and `has_file` clear.
2. **Both protocols:** the same scenario passes with a usenet release (`createusenetdownload` + `controlusenetdownload delete`) and a torrent release (`createtorrent` + `controltorrent delete`); a usenet 429 backoff does not stall the torrent poller (separate backoff state asserted in a worker test).
3. **Artifact stored + dedup:** after a grab the job row carries the stored `.nzb`/magnet/`.torrent`; re-grabbing the same release returns the **same job id** (no second TorBox submission) — asserted for usenet SHA256 and torrent info-hash.
4. **Cached check surfaced:** torrent releases display "instant" vs "will download" from `checkcached`; an uncached grab still completes via polling.
5. **Storage + WebDAV from DB:** `GET /api/v1/storage` total equals the summed `mylist` sizes and shows plan/usage from `/user/me`; `GET /api/v1/webdav` lists the imported movie's release folder categorized **Movie**; neither endpoint walks the mount (asserted: no WebDAV listing call in the request path).
6. **Failure surfaced:** a TorBox `failed (...)`/`stalled (no seeds)` state marks the job `failed` and the movie returns to `wanted` (notification deferred to Phase 4; the state transition is asserted now).

### Implements

`02` §2 (job `state` vs movie `status`), §3.1/§3.2/§3.4, §5.1/§5.2/§5.4; `03` TorBox (torrents/checkcached/user-me), Prowlarr, TMDB, Plex; `04` movies/search/storage/webdav routes; `06` grab/submit/poll/resolve/import(movie)/delete + selection input; `07` Movies/Storage/WebDAV views.

---

## 4. Phase 2 — Series

**Goal:** add the dimension movies deliberately skipped — **per-episode and season-pack handling**, which is the hardest parsing in the project. Built on the proven Phase-1 spine, so only the catalog shape, parsing, naming, and split UI are new.

### Deliverables

1. **Series/season/episode catalog** (`02` §3.2, `03` TMDB+TVDB): add a series by TMDB/TVDB id, resolve seasons+episodes from TMDB (TVDB for scene/absolute ordering when `series_type='anime'`), persist via `CreateSeries`/`UpsertSeason`/`UpsertEpisode`. Per-series/per-season/per-episode monitored flags; **air-date-aware wanted** via `WantedEpisodes` (`air_date <= date('now')`, lexicographic ISO compare). `GET/POST /api/v1/series*`, `GET /api/v1/series/{id}` (seasons+episodes with per-episode status).
2. **Episode + season-pack search** (`06`, `04`): manual search scoped to `SxxEyy` (single episode) or season pack; results returned both protocols, same picker as movies.
3. **Release-name parser + season-pack→episode mapping** (`06` `importer/release`, `01` §6): pure-Go parsing — `chill-institute/torrentname@v1.4.0` (primary, `EpisodeEnd`/`Complete`/`Season`), `nssteinbrenner/anitogo@v1.0.0` (anime absolute), and the **three in-house regexes** Boxarr owns: adjacent multi-episode `S01E01E02`, daily-show `YYYY-MM-DD`, bare season packs (`S01` without `COMPLETE`). For a completed multi-file release, enumerate its files and map each to an episode via the parser + the TVDB/TMDB episode list; each mapped episode gets its **own** library symlink. **No Python/JS sidecar** (`00` §5.2 — supersedes requirements §11.4's `guessit`/`parsett` wording).
4. **TV importer + namer** (`06` `importer/namer`): build `<TV_LIBRARY_ROOT>/<Series Title> (<Year>)/Season <NN>/<Series Title> - S<NN>E<MM> - <Episode Title>.<ext>` with zero-padding and illegal-char sanitize; multi-episode files named `S01E01-E02`; per-episode `has_file`/`status`/`library_path` set; Plex partial scan on the season path.
5. **Series deletion** (`06` delete): deleting an episode/season/whole series removes the corresponding symlinks **and** propagates each underlying download delete to TorBox (FR-DEL-2).
6. **Split Series UI** (`07`): poster-grid Series library; series detail view showing seasons/episodes with per-episode status; the split Series/Movies navigation finalized.

### Acceptance criteria (testable)

1. **Series catalog:** adding a series persists seasons+episodes; `WantedEpisodes` returns only monitored, aired, file-less episodes (an unaired episode with a future `air_date` is **not** returned).
2. **Single-episode end-to-end:** an `SxxEyy` search → grab → poll → import at the exact Plex TV path → `available`; the episode detail view reflects the status transitions.
3. **Season-pack mapping (the deferred-hard acceptance):** a completed season-pack release with N video files is enumerated and **each file is mapped to its episode and gets its own symlink**; golden tests cover `S01E01E02` adjacency, a daily `YYYY-MM-DD` show, a bare `S01` pack (no `COMPLETE`), and an anime absolute-numbered pack — all mapping to the correct `(season,episode)` rows with **no Python/JS process spawned** (asserted: pure-Go, single binary).
4. **TV naming:** generated paths match `<Series Title> (<Year>)/Season <NN>/<Series Title> - S<NN>E<MM> - <Episode Title>.<ext>` with zero-padded `NN`/`MM` and sanitized illegal chars; a multi-episode file is named `…S01E01-E02…`.
5. **Series deletion cascades:** deleting a season deletes all its episodes' symlinks and issues a TorBox delete per underlying download; the catalog rows fall back to `wanted` only if still monitored, else `missing`.

### Implements

`02` §3.2 (`series`/`season`/`episode`), §5.2 (`Upsert*`, `WantedEpisodes`); `03` TMDB (seasons/episodes) + TVDB (ordering/season-types); `06` parsing + season-pack mapping + TV namer + TV import + series delete; `04` series routes; `07` split Series UI.

---

## 5. Phase 3 — Seerr

**Goal:** let Overseerr/Jellyseerr add movies and series by emulating **Sonarr v3** and **Radarr v3** as two inbound surfaces. The catalog ingest reuses Phase 1/2 `POST /api/v1/movies` / series add; this phase is the protocol shell plus search-on-add.

### Deliverables

1. **Two inbound surfaces** (`05`, `01` §5): `r.Mount("/sonarr/api/v3", …)` and `r.Mount("/radarr/api/v3", …)`, auth via **both** `X-Api-Key` header and `apikey` query param (Servarr convention) against `BOXARR_SEERR_API_KEY(S)` using the reused constant-time check (FR-SEERR-1).
2. **Test/config endpoints** (`05`, `02` §3.6 servarr tables): `GET /api/v3/system/status` advertising a **Sonarr `3.x.x.x`** / **Radarr v3** version string (`00` §5.3 — a `4.x` Sonarr string breaks the contract); `GET /api/v3/qualityprofile` (≥ profile id 1), `GET /api/v3/rootfolder` (`{id,path,accessible:true,freeSpace}` computed at request time over the seeded roots), `GET /api/v3/tag` (`[]` ok), and `GET /api/v3/languageprofile` on the Sonarr surface (FR-SEERR-2).
3. **Lookups + add** (`05`, `06` search): `GET /api/v3/series/lookup?term=tvdb:{id}` and `GET /api/v3/movie/lookup?term=tmdb:{id}` returning canonical objects built from TMDB; `POST /api/v3/series` and `POST /api/v3/movie` ingesting into the Phase-1/2 catalog as monitored, honoring `addOptions.searchForMissingEpisodes`/`searchForMovie` by kicking off search (FR-SEERR-3/4/5), and responding `201`/`200` with a body echoing an assigned numeric `id`. The update path `GET /api/v3/episode?seriesId=` + `PUT /api/v3/episode/monitor` is supported when the series already exists.

### Acceptance criteria (testable)

1. **Both "Test" buttons pass:** Overseerr **and** Jellyseerr "Test" against the Sonarr surface and the Radarr surface both succeed; `system/status` returns a `3.x.x.x` Sonarr / valid Radarr version and Jellyseerr's synchronous read of it does not error.
2. **Dropdowns populate:** Seerr's quality-profile and root-folder dropdowns populate from `GET /qualityprofile` (id 1) and `GET /rootfolder` (the TV root on Sonarr, the movie root on Radarr, each with `accessible:true` and a real `freeSpace`).
3. **Add round-trips:** a Seerr request for a movie issues `POST /radarr/api/v3/movie` (with `tmdbId`) → Boxarr ingests it as a monitored movie, kicks off search when `searchForMovie` is set, and responds with `response.data.id` populated (Axios does not throw); the equivalent `POST /sonarr/api/v3/series` (with `tvdbId`) ingests a monitored series and honors `searchForMissingEpisodes`.
4. **Id stability:** the `qualityProfileId`/`rootFolderPath` Seerr echoes back resolve to the seeded rows (no "unknown root folder" error); restarting Boxarr keeps the same ids.

### Implements

`05` (all Sonarr v3 + Radarr v3 payloads, auth, version strings); `02` §3.6/§5.6 (servarr reads + root-folder upsert); `06` (search-on-add) reusing Phase 1/2 ingest.

---

## 6. Phase 4 — Lifecycle & notifications

**Goal:** make TorBox's expire-able storage a first-class concern — the 15-minute reconcile sweep, unknown-content detection, a persistent notification center, and a **torrent-aware healer**.

### Deliverables

1. **15-minute reconciler** (`06` reconcile, `01` §4): `BOXARR_RECONCILE_INTERVAL` (default `15m`, matching TorBox's WebDAV refresh cadence) sweep listing the WebDAV mount + both `mylist`s against known jobs; upsert `webdav_item` rows (size/last_seen) and `MarkWebDAVItemsBrokenNotSeenSince` for vanished paths (FR-NC-1). The forced `/refresh` stays the fast post-completion import path; the sweep is for reconciliation only (FR-NC-5).
2. **Unknown-content detection** (`06`, `02` §5.4): items on the mount not matchable to a known job are flagged `known=0`, best-effort categorized Movie/Series/Unknown by parsing folder/file names (reusing the Phase-2 parser), and raised as `unknown_content` notifications with name/size/category (FR-WD-2/3, FR-NC-2).
3. **Notification center** (`02` §3.3/§5.3, `04`, `07`): persist events — `download_completed`, `grab_failed`, `heal_triggered`/`heal_succeeded`/`heal_failed`, `deletion_completed`, `limit_reached`, `unknown_content` — with read/unread state, newest-first listing, and an unread badge (FR-NC-3/4). `GET /api/v1/notifications`, `GET /api/v1/notifications/unread_count`, `POST …/{id}/read`, `POST …/read_all`.
4. **Torrent-aware heal** (`06` heal, `01` §4 healer): extend the reused healer to **branch on protocol** — resubmit the stored magnet/`.torrent` (not just NZBs) — plus the **fresh-Prowlarr-re-search fallback** when the stored artifact is dead, with best-match repointing (`00` §19.4, FR-HEAL-1/2). Broken-symlink detection flips `has_file=0`, `status='expired_broken'`.
5. **Storage breakdown + limit surfacing** (`04`, `07`): storage overview broken down by category (movies vs series) using Boxarr's mapping (FR-ST-3); plan tier, active slots in use, and monthly usage/cooldown from `/user/me` surfaced in the overview and as `limit_reached` notifications (FR-LIM-4).

### Acceptance criteria (testable)

1. **Sweep cadence + upsert:** the reconciler runs on the configured interval; a WebDAV listing test asserts each release folder produces/updates a `webdav_item` row (size + `last_seen`), and a folder that disappears is marked `is_broken=1`.
2. **Unknown content raised:** a release folder present on the mount but absent from `jobs` produces an `unknown_content` notification with name/size/best-effort category; the WebDAV view flags it Unknown with adopt/ignore/delete actions.
3. **Notification center:** completing a download, a failed grab, a heal, a deletion, and a limit/cooldown each enqueue the corresponding notification; the unread badge equals `COUNT(*) WHERE read=0`; the feed is newest-first; marking read clears the badge and persists across restart.
4. **Torrent heal:** breaking a previously-imported **torrent** symlink (target removed) triggers a heal that **resubmits the stored magnet/`.torrent`**, and when that artifact is dead, a fresh Prowlarr re-search re-acquires and best-match-repoints the library symlink; `heal_succeeded` is recorded and `status` returns to `available`.
5. **Limit surfacing:** approaching the 60/hr or 300/min TorBox ceiling backs off and queues rather than failing grabs (FR-LIM-3), and `/api/v1/storage` shows plan tier + active slots + monthly cooldown from `/user/me`.

### Implements

`06` reconcile + unknown-content + torrent/research heal; `02` §3.3/§3.7/§5.3/§5.4 (notification + webdav); `04` notifications/storage routes; `07` Notification center + storage breakdown; FR-LIM surfacing.

---

## 7. Phase 5 — Automation (optional)

**Goal:** the optional layer deferred last by design (requirements §18/§19, `00` §8 lists availability back-sync out of scope). Scheduled monitoring of ongoing series and auto-search of still-missing wanted items. **Manual search remains the supported path; this is purely additive** and gated behind config so it can ship dark.

### Deliverables

1. **Metadata-refresh loop** (`01` §4, `06`): `BOXARR_METADATA_REFRESH_INTERVAL` (default `24h`) refreshing TMDB/TVDB to pick up new seasons/episodes and recomputing wanted-but-missing (FR-CAT-5), reusing `UpsertSeason`/`UpsertEpisode` (which preserve lifecycle columns).
2. **Scheduled auto-search** (`06` selection, `04`/`08`): `BOXARR_SEARCH_INTERVAL` loop running the configurable **selection score** (FR-SR-5: preferred resolution/quality, size min/max per quality, protocol preference incl. prefer-cached-torrents, seeders/grabs threshold, preferred/blocked group/keyword lists) over Prowlarr results for monitored, still-missing, aired items and auto-grabbing the best candidate; on a failed grab, pick the next candidate (FR-SR-4, FR-GP-7). All gated by a config flag, default off.

### Acceptance criteria (testable)

1. **Metadata refresh:** the daily loop adds a newly-aired episode to an existing monitored series and flips it to `wanted` without operator action; existing `status`/`has_file`/`job_id`/`library_path` are not clobbered.
2. **Auto-search scoring:** with auto-search enabled, a monitored still-missing aired item is searched, the selection score ranks candidates per the configured weights, and the best is grabbed automatically; a failed grab advances to the next candidate. With the flag off, **no automatic search runs** (manual remains the only path).

### Implements

`06` (selection score, scheduled search, metadata refresh); `04` (auto-search controls); `08` (interval/selection/limit config).

---

## 8. FR-traceability matrix

Every FR/NFR in `docs/boxarr-requirements.md` mapped to the **defining spec(s)** and the **delivering phase**. (Spec ownership per `00` document map; phase per §§2–7 above.)

### UI (FR-UI-*)

| FR | Requirement | Spec(s) | Phase |
|---|---|---|---|
| FR-UI-1 | Split Series/Movies poster libraries | `07`, `04` | 1 (Movies) / 2 (Series) |
| FR-UI-2 | Series detail: seasons/episodes per-episode status | `07`, `02` §2.2, `04` | 2 |
| FR-UI-3 | Movie detail: status | `07`, `02` §2.2, `04` | 1 |
| FR-UI-4 | Manual interactive + free-text search | `04`, `06`, `07` | 1 |
| FR-UI-5 | WebDAV view (category + size) | `04`, `02` §3.4, `07` | 1 (movies) / 4 (unknown) |
| FR-UI-6 | Storage overview | `04`, `02` §5.4, `07` | 1 |
| FR-UI-7 | Notification center | `04`, `02` §3.3, `07` | 4 |
| FR-UI-8 | Settings area for all connections | `04`, `07`, `08` | 0 |
| FR-UI-9 | Views read DB, not live mount scan | `02` §2.2, `04` | 0 (principle) / 1 (movies) |

### Catalog & metadata (FR-CAT-*)

| FR | Requirement | Spec(s) | Phase |
|---|---|---|---|
| FR-CAT-1 | Add series by TMDB/TVDB id, persist seasons/episodes | `02` §3.2, `03` TMDB/TVDB, `04` | 2 |
| FR-CAT-2 | Add movie by TMDB id, persist | `02` §3.2, `03` TMDB, `04` | 1 |
| FR-CAT-3 | Per-series/season/movie monitored flags | `02` §3.2/§5.2, `04` | 1 (movie) / 2 (series) |
| FR-CAT-4 | Air-date-aware wanted-but-missing computation | `02` §5.2 (`Wanted*`), `06` | 1 (movie) / 2 (episode) |
| FR-CAT-5 | Scheduled metadata refresh | `06`, `01` §4, `02` §5.2 | 5 |

### Search & selection (FR-SR-*)

| FR | Requirement | Spec(s) | Phase |
|---|---|---|---|
| FR-SR-1 | Manual Prowlarr search, usenet+torrent | `03` Prowlarr, `06`, `04` | 1 (movie) / 2 (episode/pack) |
| FR-SR-2 | Results w/ metadata + cached status | `03` (`checkcached`), `04`, `07` | 1 |
| FR-SR-3 | Pick → grab pipeline | `06`, `04` | 1 |
| FR-SR-4 | Automatic search on add / on schedule | `06`, `04` | 5 (3 for search-on-add via Seerr) |
| FR-SR-5 | Configurable selection score | `06` selection, `08` | 5 (defined) |

### Grab pipeline (FR-GP-*)

| FR | Requirement | Spec(s) | Phase |
|---|---|---|---|
| FR-GP-1 | Store artifact locally on job | `02` §3.1/§5.1, `06` | 1 |
| FR-GP-2 | Submit to TorBox (usenet + torrent) | `03` TorBox, `06`, `01` §4 | 1 |
| FR-GP-3 | Poll mylist to completion | `01` §4, `03`, `06` | 1 |
| FR-GP-4 | Resolve release folder on WebDAV | `06`, `03`, `01` §4 | 1 |
| FR-GP-5 | Dedup submissions | `02` §5.1, `06` | 1 |
| FR-GP-6 | Respect TorBox create caps | `03` TorBox, `06` (limits) | 1 (basic) / 4 (surfaced) |
| FR-GP-7 | Handle failure states + re-search | `02` §2.1, `06`, `04` | 1 (mark failed) / 4 (notify) / 5 (re-search) |

### Seerr (FR-SEERR-*)

| FR | Requirement | Spec(s) | Phase |
|---|---|---|---|
| FR-SEERR-1 | Auth via X-Api-Key + apikey query | `05`, `01` §5 | 3 |
| FR-SEERR-2 | Shared servarr test/config endpoints | `05`, `02` §3.6 | 3 |
| FR-SEERR-3 | series/movie lookups | `05`, `03` TMDB | 3 |
| FR-SEERR-4 | POST /series add | `05`, `06` | 3 |
| FR-SEERR-5 | POST /movie add | `05`, `06` | 3 |
| FR-SEERR-6 | Schema-compatible responses | `05` | 3 |
| FR-SEERR-7 | No availability back-sync (out of scope) | `00` §8, `05` | n/a |

### Import & library (FR-IMP-*)

| FR | Requirement | Spec(s) | Phase |
|---|---|---|---|
| FR-IMP-1 | Tool owns release→target mapping | `02` §3.1, `06` | 1 |
| FR-IMP-2 | Separate movie/TV roots, Plex layout | `06` namer, `08` | 1 (movie) / 2 (TV) |
| FR-IMP-3 | Symlink import into library | `06`, `01` §4 (symlink farm) | 1 |
| FR-IMP-4 | Season-pack enumerate + per-file map | `06` `importer/release` (`00` §5.2) | 2 |
| FR-IMP-5 | Plex partial scan after import | `03` Plex, `06` | 1 |
| FR-IMP-6 | Same-filesystem rename, never copy | `00` §5.1, `06` | 1 |

### Deletion & heal (FR-DEL-*, FR-HEAL-*)

| FR | Requirement | Spec(s) | Phase |
|---|---|---|---|
| FR-DEL-1 | Delete item → symlink + TorBox delete | `06` delete, `01` §4 deleter | 1 |
| FR-DEL-2 | Delete series/season cascades | `06`, `02` §5.2 | 2 |
| FR-HEAL-1 | Re-acquire broken symlink (extend to torrents) | `06` heal, `01` §4 healer | 4 |
| FR-HEAL-2 | Fresh-Prowlarr-re-search heal fallback | `00` §19.4, `06` | 4 |

### Limits (FR-LIM-*)

| FR | Requirement | Spec(s) | Phase |
|---|---|---|---|
| FR-LIM-1 | Reuse 429/Retry-After handling | `03` TorBox (reused helpers) | 0 (clients) / 1 (in pipeline) |
| FR-LIM-2 | Concurrency gate to active-slot allowance | `06` limits, `03` (`/user/me`) | 4 |
| FR-LIM-3 | Track 60/hr + 300/min ceilings, back off+queue | `03` TorBox, `06` | 1 (basic) / 4 (full) |
| FR-LIM-4 | Surface tier/slots/usage/cooldown | `03` (`/user/me`), `04`, `07` | 4 |

### WebDAV & storage (FR-WD-*, FR-ST-*)

| FR | Requirement | Spec(s) | Phase |
|---|---|---|---|
| FR-WD-1 | List every mount item w/ size | `02` §3.4/§5.4, `04` | 1 (own) / 4 (full sweep) |
| FR-WD-2 | Categorize Movie/Series/Unknown | `02` §3.4, `06` (parse) | 1 (own) / 4 (unknown parse) |
| FR-WD-3 | Flag unknown → notification | `06` reconcile, `02` §5.4 | 4 |
| FR-ST-1 | Total used = sum mylist sizes | `02` §5.4, `03`, `04` | 1 |
| FR-ST-2 | Used vs plan limits / monthly cooldown | `03` (`/user/me`), `04` | 1 (basic) / 4 (full) |
| FR-ST-3 | Breakdown by category | `02`, `04` | 4 |

### Reconcile & notifications (FR-NC-*)

| FR | Requirement | Spec(s) | Phase |
|---|---|---|---|
| FR-NC-1 | 15-min reconciliation poll | `06` reconcile, `01` §4 | 4 |
| FR-NC-2 | Unknown-content notification | `06`, `02` §5.4 | 4 |
| FR-NC-3 | Event notifications | `02` §3.3, `06`, `04` | 4 |
| FR-NC-4 | Persisted, read/unread, badge, newest-first | `02` §3.3/§5.3, `04`, `07` | 4 |
| FR-NC-5 | Keep forced /refresh as fast import path | `06`, `01` §4 | 1 (refresh kept) / 4 (sweep added) |

### Config (FR-CFG-1) & NFRs

| FR | Requirement | Spec(s) | Phase |
|---|---|---|---|
| FR-CFG-1 | Settings UI for all connections/options | `04`, `07`, `08`, `02` §3.5 | 0 |
| NFR-1 | Idempotency & retries; dedup | `02` §5.1, `03`, `06` | 0 (infra) / 1 (pipeline) |
| NFR-2 | SQLite WAL single-writer goose | `02` §1 | 0 |
| NFR-3 | Structured logging + /healthz + heal/broken surfaces | `01` §5, `04`, `03` | 0 (health) / 4 (heal/broken) |
| NFR-4 | Local-first, keys never logged | `00` §6, `08`, `02` §3.5 | 0 |
| NFR-5 | Env vars + settings UI + defaults | `08`, `04`, `02` §3.5 | 0 |
| NFR-6 | Single container; frontend served by backend | `08`, `01` §5 | 0 |
| NFR-7 | Configurable polls; DB-served views | `01` §4, `04`, `08` | 0 (intervals) / 1 (views) |

### CI/CD & supply-chain (FR-CI-*, FR-SEC-*)

| FR | Requirement | Spec(s) | Phase |
|---|---|---|---|
| FR-CI-1 | CI on every PR/push (Go + frontend + docker build no-push) | `08`, `04-sab-api-config-ci.md` §4 | 0 |
| FR-CI-2 | Release: multi-arch GHCR push, semver+latest tags | `08`, ground §5 | 0 |
| FR-CI-3 | Multi-stage Dockerfile w/ frontend stage + embed | `08`, ground §6 + Recs | 0 |
| FR-CI-4 | Single self-contained image | `08`, `01` §5 | 0 |
| FR-CI-5 | OCI labels + pinned action majors | `08` | 0 |
| FR-CI-6 | Supply-chain enforcement in CI (`--frozen-lockfile`, `ignore-scripts`, audit) | `08`, `07` | 0 |
| FR-SEC-1 | Exact pinning + committed lockfile | `07`, `08` | 0 |
| FR-SEC-2 | Vetted, not bleeding-edge | `07`, `08` | 0 |
| FR-SEC-3 | No install scripts by default | `07`, `08` | 0 |
| FR-SEC-4 | Minimal dependency surface | `07` | 0 |
| FR-SEC-5 | No runtime third-party fetch (embedded) | `07`, `01` §5/§6 | 0 |
| FR-SEC-6 | Pinned package manager (corepack) | `07`, `08` | 0 |
| FR-SEC-7 | Controlled, reviewed updates (min release age) | `08` | 0 |

**Coverage:** all **79** FR/NFR ids are mapped. The only requirement with no delivering phase is **FR-SEERR-7** (availability back-sync), explicitly **out of scope** (`00` §8) because Seerr derives availability from Plex.

---

## 9. Risk & sequencing notes

1. **Hardest parsing is deferred to Phase 2, not Phase 1.** Movies have one file and need no episode/season-pack mapping, so the whole grab→import→delete spine is proven on the easy case first. The genuinely risky code — season-pack→episode mapping across standard/daily/anime orderings, including the three in-house regexes Boxarr owns (`00` §5.2) — lands in Phase 2 behind golden tests, after the spine is known-good.
2. **Optional automation is deferred to Phase 5 and ships dark.** Manual search (FR-SR-1/2/3) is the supported path from Phase 1; the selection score (FR-SR-5) and scheduled auto-search (FR-SR-4) are additive, config-gated, default-off, so a parsing or scoring bug cannot regress the manual path. Availability back-sync is permanently out of scope (`00` §8).
3. **Runtime-verify items concentrate in Phases 1, 3, 4** (`00` §9). Phase 1 must verify the TorBox torrent field names, `checkcached` shape, `createtorrent` response key, and the torrent WebDAV subpath (`BOXARR_WEBDAV_TORRENT_SUBPATH` covers both layouts). Phase 3 must verify the Sonarr `3.x.x.x` version string against both Overseerr and Jellyseerr. Phase 4 must verify Plex partial-scan path encoding and the reconcile sweep against a live mount. Each is encoded as a chosen-default + fallback in the owning spec; none blocks an earlier phase.
4. **Phase 0 front-loads deployment risk.** Publishing a multi-arch distroless image with an embedded React build from the first commit (FR-CI-3/4) surfaces Docker/embed/CGO integration problems immediately rather than at the end. The `CGO_ENABLED=0` invariant holds because the only new Go deps (`torrentname`, `anitogo`) are pure-Go (`01` §6).
5. **No phase changes reused-package behavior.** Every phase's risk is confined to *new* packages and *new* migrations; `internal/torbox`/`store`/`job`/`worker` are only ever extended additively (`00` Assumption C, `01` §2), so a Phase-N regression cannot break the proven usenet spine.

## Definition of done

The phase plan is satisfied when Boxarr is delivered in the order **0 → 1 → 2 → 3 → 4 → (5)**, each phase merging to `main` green (Go format/vet/lint/`-race -cover` + `pnpm lint`/`build` + no-push docker build) and — from Phase 0 onward — **publishing a multi-arch `ghcr.io/radaiko/boxarr` image with the React SPA embedded**; when Phase 0 has renamed the module, dropped the entire SAB surface, applied migrations `004`–`009`, stood up the TorBox(+torrents/checkcached/user-me)/Prowlarr/TMDB/TVDB/Plex clients, and served a React shell with a working Settings page over an authed `/api/v1`; when Phase 1 can take a movie from TMDB search through grab (usenet **and** torrent) → poll → import as a symlink at `<Title> (Year)/<Title> (Year).ext` → Plex scan → delete with TorBox propagation, with storage and WebDAV views served from the DB; when Phase 2 adds the series catalog and the season-pack→episode mapping under golden tests with **no Python/JS sidecar**; when Phase 3's Sonarr v3 and Radarr v3 surfaces pass both Overseerr and Jellyseerr "Test" and ingest add-requests; when Phase 4's 15-minute reconciler, unknown-content detection, persistent notification center, and torrent-aware healer are live; and when **every one of the 79 FR/NFR ids** in `docs/boxarr-requirements.md` traces to its defining spec(s) and a delivering phase in §8 — the sole exception being the out-of-scope FR-SEERR-7. Cross-refs: `00-decisions-and-assumptions.md` (decisions/register), `01-architecture-and-packages.md` (packages/workers/router), `02-data-model.md` (migrations/state), `03-external-contracts.md` (clients), `04-internal-api.md` (`/api/v1`), `05-seerr-emulation.md` (Sonarr/Radarr v3), `06-pipelines.md` (grab/import/delete/heal/reconcile/selection/parsing), `07-frontend.md` (SPA), `08-config-deploy-ci.md` (config/Docker/CI).
