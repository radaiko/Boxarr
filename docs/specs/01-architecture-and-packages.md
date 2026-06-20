# Boxarr — Architecture & Packages (Spec 01)

**Date:** 2026-06-20
**Status:** Approved for planning

This spec owns the **Go package layout**, the **worker topology**, the **HTTP serving / namespace map**, the **dependency inventory**, and the concrete **repo-evolution mechanics** that turn `sab2torbox` into Boxarr. It is grounded verbatim in the real `radaiko/sab2torbox` source (cloned at review time). Cross-cutting decisions, naming, and the runtime-verify register live in `00-decisions-and-assumptions.md` and are **not** re-decided here. See `02-data-model.md` for DDL, `03-external-contracts.md` for client contracts, `04-internal-api.md` / `05-seerr-emulation.md` for HTTP surfaces, `06-pipelines.md` for the state machines this package map hosts.

---

## 1. Overview — one tool replacing three

Boxarr is a single Go binary that **replaces sab2torbox + Sonarr + Radarr** (`00` §1). It does not import sab2torbox as a dependency — **this repository *is* the evolved sab2torbox** (`00` Assumption A). The thesis is **evolve, not rewrite**:

- **Reuse the proven pipeline.** The entire usenet spine — TorBox client (`{success,error,detail,data}` envelope, `FlexInt`, `RateLimit`/`Retryable`/`parseRetryAfter`), the single-writer SQLite store (WAL + `busy_timeout(5000)` + `foreign_keys(1)` + `_txlock=immediate`, `SetMaxOpenConns(1)`), goose `//go:embed` migrations, the Go-side job state machine, the submit/poll/symlink-farm/heal/delete/reaper workers, the distroless image + `healthcheck` subcommand — is **kept and extended in place** (`00` Assumption C). This is the riskiest, best-tested code; rewriting it would discard the project's whole reason for choosing Go.
- **Add a torrent path that mirrors usenet.** TorBox torrents (`/torrents/*`), `/user/me`, and `checkcached` are net-new TorBox methods, but they route through the same `do()`/`Envelope`/`FlexInt`/rate-limit infrastructure verbatim (verified `/tmp/sab2torbox/internal/torbox/client.go:133-211`); the torrent submit/poll loops mirror the usenet loops without touching them.
- **Add the catalog/orchestration layer Sonarr/Radarr provide.** Prowlarr search, TMDB/TVDB metadata, Plex scan, selection scoring, importer + release-name parsing, a 15-minute reconciler, a notification center, Boxarr's own `/api/v1` REST surface, the Sonarr/Radarr v3 Seerr-emulation surfaces, and an embedded React SPA are all **new packages** layered on top.
- **Drop the SAB emulation.** The SABnzbd download-client surface is the one thing Boxarr *removes* (`00` Assumption B); nothing pushes NZBs to Boxarr anymore — it *is* the *arr.

Boxarr's structural advantage over Sonarr/Radarr is a **lifecycle model**: content lives on TorBox WebDAV and can expire or vanish. The reused healer/reaper plus a new reconciler make that a first-class concern.

---

## 2. Repo evolution mechanics

Concrete, ordered steps. These are mechanical refactors plus deletions; nothing in the reused packages changes behavior.

1. **Copy the packages in.** `internal/torbox`, `internal/store`, `internal/job`, `internal/config`, `internal/api`, `internal/worker`, and `cmd/...` already live in this repo (it is the fork). Carry `go.sum` with them — it exists in source (verified `/tmp/sab2torbox/go.sum`, 6040 bytes) and the `Dockerfile` `COPY go.mod go.sum ./` step depends on it (verified `04` §6).
2. **Rename the module.** `github.com/radaiko/sab2torbox` → **`github.com/radaiko/boxarr`** in `go.mod` (verified module path `/tmp/sab2torbox/go.mod:1`), then rewrite **every import path** across the tree:
   ```bash
   go mod edit -module github.com/radaiko/boxarr
   grep -rl 'github.com/radaiko/sab2torbox' --include='*.go' . \
     | xargs sed -i '' 's#github.com/radaiko/sab2torbox#github.com/radaiko/boxarr#g'
   go mod tidy && gofmt -s -w . && go build ./... && go test ./...
   ```
3. **Rename the binary/cmd.** `cmd/sab2torbox` → **`cmd/boxarr`** (binary `/boxarr`, locked `00` §6). Update the `Dockerfile` build target `./cmd/boxarr`, the output path `/out/boxarr`, the `COPY --from=build /out/boxarr /boxarr`, the `EXPOSE`, the `HEALTHCHECK CMD ["/boxarr","healthcheck"]`, and `ENTRYPOINT ["/boxarr"]` (skeleton verified `04` §6).
4. **Rename the env prefix.** `envconfig.Process("sab2torbox", &c)` → `envconfig.Process("boxarr", &c)` so all vars become `BOXARR_*` (verified `04` §1; reference table owned by `08`). `runHealthcheck()` must read **`BOXARR_LISTEN_ADDR`** via `os.Getenv` (default `:8080`, rewrite leading `:`→`127.0.0.1:`), not `SAB2TORBOX_LISTEN_ADDR` (verified `04` §3).
5. **Promote `chi` to a direct require.** It is currently **indirect** at `github.com/go-chi/chi/v5 v5.2.5` even though `internal/api` imports it directly (verified `/tmp/sab2torbox/go.mod:14`, `00` §9 register). `go get github.com/go-chi/chi/v5@v5.2.5` after the import rewrite, then `go mod tidy`, moves it into the direct `require` block.
6. **Standardize Go 1.25.** Keep `go 1.25.7` in `go.mod`, `golang:1.25-alpine` builder, `setup-go` `go-version: '1.25'` — all already on 1.25 (verified `/tmp/sab2torbox/go.mod:3`, `04` §4/§6; `00` §2 locked).
7. **DELETE the SAB emulation** (`00` Assumption B). Remove precisely:
   - **`internal/api/handlers.go`** — `handleAPI` and the `for _, base := range []string{"/api","/sabnzbd/api"}{ r.Get(base,...); r.Post(base,...) }` router loop; the mode dispatch (`version`/`get_config`/`fullstatus`/`addurl`/`addfile`/`queue`/`history`/`delete`) and helpers `handleAddURL`, `handleAddFile`, `handleQueue`, `handleHistory`, `handleDelete`, `parseNzoID`, `validAPIKey` callers tied to SAB, `formatTimeLeft`, `queueStatusLabel`, `maxNZBSize` (verified `04` §2). The route loop is **the only thing serving `/api` and `/sabnzbd/api`** — both endpoints disappear with it.
   - **`internal/api/responses.go` (entire file)** — `VersionResponse`, `AddResponse`, `ErrorResponse`, `QueueSlot`, `Queue`, `QueueResponse`, `HistorySlot`, `History`, `HistoryResponse`, `Category`, `ConfigResponse`, `StatusResponse`, `DeleteResponse` and the `formatTimeLeft`/`queueStatusLabel` helpers (verified `04` §2, structs listed `/tmp/sab2torbox/internal/api/responses.go:12-184`). Plus `responses_test.go`.
   - **`SAB_API_KEY`** — the `SABAPIKey string \`envconfig:"SAB_API_KEY" required:"true"\`` field (verified `04` §1) and every reference. The constant-time `subtle.ConstantTimeCompare` api-key check helper is **kept** (reused for `/api/v1` and Seerr auth, `00` §7) but re-pointed at Boxarr/Seerr keys.
   - **Keep** `/healthz` and the `Server`/`writeJSON`/`param()` machinery. The `/health/*` heal endpoints are **reshaped** into `/api/v1` (`04`), not served at their old paths.
8. **Rename the DB default.** `DatabasePath` default `/config/sab2torbox.db` → **`/config/boxarr.db`** (verified `04` §1; locked `00` §6).
9. **Repoint `/healthz`'s `Pinger`.** Keep the `Health`/`Checker`/`Pinger` shape and the **5-minute TorBox ping cache** (`api.NewHealth(st, tb, 5*time.Minute)`, verified `04` §2/§3) but check the dependencies Boxarr needs — DB + TorBox token, optionally Prowlarr/TMDB reachability (`00` §5.6).
10. **Add the frontend + embed.** New `web/` (React+TS+Vite, `07`) and `internal/web` (`go:embed`), plus the Dockerfile frontend stage and CI step (`08`). These have no sab2torbox precedent — sab2torbox ships **no web UI** (verified `04` Key Facts).

**Net deletions are confined to the SAB surface.** Everything under `internal/torbox`, `internal/store`, `internal/job`, `internal/worker` survives the rename untouched in behavior.

---

## 3. Project layout

Target `internal/` tree. Each package is tagged **[reused]** (copied, behavior unchanged bar import path), **[extended]** (kept + new methods/fields), or **[new]**.

```
boxarr/
├── cmd/
│   └── boxarr/                  [extended] main.go: healthcheck subcommand (reads BOXARR_LISTEN_ADDR)
│                                           + run() wiring; drops SAB, adds new clients+workers+SPA
├── internal/
│   ├── torbox/                  [extended] TorBox client: usenet (kept) + torrents/checkcached/user-me
│   │   ├── client.go                        do()/Envelope/APIError/rate-limit reused verbatim
│   │   └── types.go                         FlexInt + UsenetDownload kept; +TorrentDownload/User/CachedCheck
│   ├── store/                   [extended] SQLite open/DSN/goose reused; +catalog/notify/webdav/settings methods
│   │   └── migrations/                       004+ goose files (002/003 pattern); 001-003 kept verbatim
│   ├── job/                     [extended] 11-state machine kept; +protocol/media_ref/torrent fields; +states if any
│   ├── config/                  [extended] envconfig BOXARR_*; drops SAB/SYMLINK-only vars, adds Prowlarr/TMDB/…
│   ├── prowlarr/                [new]      Prowlarr client: /api/v1/search + /api/v1/indexer  (-> 03)
│   ├── metadata/               [new]      catalog/metadata service facade over tmdb+tvdb (-> 03)
│   │   ├── tmdb/                [new]        TMDB client: series/seasons/episodes/movies/artwork/configuration
│   │   └── tvdb/                [new]        TVDB v4 client: scene/absolute ordering, season types, JWT token
│   ├── plex/                    [new]      Plex client: partial-scan trigger (X-Plex-Token)  (-> 03)
│   ├── catalog/                 [new]      what the user wants: series/season/episode/movie persistence + monitored set
│   ├── selection/              [new]      release scoring: pick best Prowlarr result per configurable score (-> 06)
│   ├── importer/               [new]      organize completed releases into Plex library symlinks; reuses symlink farm
│   │   ├── namer/              [new]        Plex-standard path builder (movie/TV layout, padding, sanitize) (-> 06)
│   │   └── release/            [new]        release-name parser: torrentname+anitogo+3 in-house regexes (-> 06)
│   ├── reconcile/              [new]      15-min sweep: WebDAV+mylist vs known jobs; unknown-content detection (-> 06)
│   ├── notify/                 [new]      notification center: persist events, read/unread, badge (-> 02,04)
│   ├── worker/                  [extended] submit/poll/symlink/heal/delete/reaper kept; +torrent loops, +reconciler
│   │   ├── worker.go                        worker.New wiring + Run + HealRunInfo reused; +new loops
│   │   ├── submitter.go / poller.go         usenet kept; torrent_submitter.go / torrent_poller.go mirror them
│   │   ├── symlink.go                        buildSymlinkFarm/atomicReplaceSymlink/findBestMatch reused verbatim
│   │   ├── healer.go / deleter.go / reaper.go  kept; healer+deleter branch on protocol; +Prowlarr-research heal
│   │   └── clock.go                          timeNow seam reused; route new time reads through it
│   ├── api/                     [extended] chi router + Server + /healthz + writeJSON kept; SAB surface DELETED
│   │   ├── v1/                  [new]        Boxarr's own REST surface (-> 04)
│   │   └── seerr/              [new]        Sonarr v3 + Radarr v3 emulation (-> 05)
│   │       ├── sonarr.go        [new]        /sonarr/api/v3/* handlers
│   │       └── radarr.go        [new]        /radarr/api/v3/* handlers
│   └── web/                     [new]      go:embed of web/dist + SPA index.html fallback handler
├── web/                         [new]      React+TS+Vite source (pnpm) -> web/dist (-> 07)
└── deploy/                      [extended] Dockerfile (+frontend stage), docker-compose (BOXARR_*) (-> 08)
```

**One-line responsibilities:**

| Package | Responsibility |
|---|---|
| `cmd/boxarr` | Process entrypoint: `healthcheck` subcommand + `run()` wiring of config, store, clients, workers, HTTP server, SPA. |
| `internal/torbox` | TorBox HTTP client: usenet + torrents + `checkcached` + `/user/me`, all via shared `do()`/`FlexInt`/rate-limit. |
| `internal/store` | Single-writer SQLite: open/DSN/goose migrations + all query methods for jobs, catalog, notifications, webdav, settings. |
| `internal/store/migrations` | Sequential goose `.sql` files; 001-003 verbatim, 004+ add torrent/media/catalog/notify/webdav/settings. |
| `internal/job` | Job domain type + the Go-only state machine (`transitions` map, `CanTransitionTo`, `IsTerminal`). |
| `internal/config` | `envconfig.Process("boxarr", …)` struct + `Load()` validation + helper predicates. |
| `internal/prowlarr` | Prowlarr client: interactive search + indexer listing (`X-Api-Key`). |
| `internal/metadata` | Facade unifying TMDB (primary) + TVDB (ordering) into one catalog/metadata service. |
| `internal/metadata/tmdb` | TMDB client: catalog, posters, movie+TV, `/configuration` image base. |
| `internal/metadata/tvdb` | TVDB v4 client: scene/absolute ordering, season types, JWT-token lifecycle. |
| `internal/plex` | Plex client: targeted partial-scan trigger after import. |
| `internal/catalog` | Persisted catalog: series/season/episode/movie + monitored flags + wanted-but-missing computation. |
| `internal/selection` | Configurable release-selection score over Prowlarr results. |
| `internal/importer` | Build the library symlink at its final Plex path; drives namer + release parser. |
| `internal/importer/namer` | Plex-standard folder/file path builder (zero-padding, char sanitize, multi-ep/anime/daily). |
| `internal/importer/release` | Release-name → episode/movie parser (torrentname + anitogo + 3 in-house regexes). |
| `internal/reconcile` | 15-minute reconciliation sweep + unknown-content detection feeding `notify`. |
| `internal/notify` | Notification center: persist events, read/unread state, newest-first listing, unread badge. |
| `internal/worker` | All background loops: submit/poll/symlink/heal/delete/reaper (usenet, kept) + torrent variants + reconciler + metadata-refresh. |
| `internal/api` | chi router + `Server` (injected deps) + `/healthz` + JSON helpers; hosts `v1` and `seerr` subrouters. |
| `internal/api/v1` | Boxarr's own `/api/v1` REST surface for the SPA. |
| `internal/api/seerr` | Sonarr v3 (`/sonarr`) + Radarr v3 (`/radarr`) inbound emulation. |
| `internal/web` | `go:embed` the built SPA and serve it with index.html fallback for client routes. |

---

## 4. Worker topology

Reuse `worker.New(st, tb, cfg, logger) *Workers` and `Run(ctx)` verbatim (verified `/tmp/sab2torbox/internal/worker/worker.go:56,77`). `Run` blocks on a `sync.WaitGroup`, launching one **goroutine per loop**; each loop runs its `Once` fn immediately, then on a `time.NewTicker(interval)`, with shutdown a `select` on `ctx.Done()` vs. the ticker, and errors logged only while `ctx.Err()==nil` (verified `02` Concrete Artifacts, `worker.go:78-127`). **All loops share the single SQLite writer** (`SetMaxOpenConns(1)`, verified `01` store grounding); because each loop is its own goroutine, per-loop state maps and backoff timers (`missingPolls`, `deleteAttempts`, `submitBackoffUntil`, `webdavBackoffUntil`, `lastWebDAVRefresh`) **need no locks** and the **torrent path must get its own** `torrentSubmitBackoffUntil` / `torrentMissingPolls` so a usenet 429 never pauses torrents (verified `02` Recommendations).

`worker.New` also implements `HealReporter.HealRunInfo() (last, next)` (verified `worker.go:103`), kept for the health/notify surface. Graceful shutdown is reused unchanged: `run()` runs `workers.Run(ctx)` in one goroutine and `httpServer.Shutdown(10s)` on `<-ctx.Done()` in another (verified `04` §3).

| Goroutine | Status | Interval | Notes |
|---|---|---|---|
| **usenet submitter** | [reused] | `BOXARR_POLL_INTERVAL` (default `1m`) | `JobsByState(pending)` → `CreateUsenetDownload`; 429 reverts to `pending` + `submitBackoffUntil` (verified `02` §submitter). |
| **usenet poller** | [reused] | `1m` | `ListUsenet` each tick; updates progress/eta/state; resolves WebDAV folder; builds symlink farm (verified `02` §poller). |
| **torrent submitter** | [new] | `1m` | Mirrors usenet: `JobsByState(pending)` filtered to `protocol=torrent` → `CreateTorrent`; **own** `torrentSubmitBackoffUntil`; separate 300/min + 60/hr budget (`00` §13, `02` Recs 3-4). |
| **torrent poller** | [new] | `1m` | Mirrors usenet: `ListTorrents`; resolves `TorrentPath()=Join(WebDAVMountRoot, BOXARR_WEBDAV_TORRENT_SUBPATH)` then `rec.Name`; **own** `torrentMissingPolls`; reuses `maybeRefreshWebDAV` (one mount) (`02` Recs 5; subpath default empty, `00` §19.5 / register). |
| **deleter** | [extended] | `1m` | Propagates deletes to TorBox; branch on `protocol`: `ControlUsenet(id,"delete")` vs `ControlTorrent(id,"delete")`; reuses `deleteGiveUpAttempts=60` retry + guarded `removeSymlinkDir` + `DeleteJob` (verified `03` §deleter; `02` Recs (b)). |
| **reaper** | [reused] | `5m` | Detects imports (empty release dir), TTL-reaps imported jobs (24h), sweeps empty/all-broken farm dirs; never removes a category dir or root (verified `03` §reaper). |
| **healer** | [extended] | `BOXARR_HEAL_INTERVAL` (default `1h`), opt-in | Discover→detect-broken→trigger; resubmits the **stored artifact** (NZB or magnet/.torrent, branch on `protocol`), atomic repoint on completion; **+ fresh-Prowlarr-re-search fallback** when the artifact is dead (`00` §19.4; `03` §healer + Recs (a)). |
| **heal-reconciler** | [reused] | `1m`, opt-in | Reconciles jobs already in `healing` against the list; reuses `finishHeal` repoint (protocol-agnostic, verified `03` §healer). |
| **grab/search orchestrator** | [new] | event-driven + `BOXARR_SEARCH_INTERVAL` | Turns a chosen/auto-selected release into a `pending` job (artifact stored, dedup-before-insert); on add-with-search runs `selection` over Prowlarr results. Hands off to the submitters; details `06`. |
| **reconciler** | [new] | `BOXARR_RECONCILE_INTERVAL` (default `15m`) | Lists WebDAV mount + both `mylist`s vs known jobs; flags unknown content; feeds `notify` (`00` §1; FR-NC-1, `06`). |
| **metadata-refresh** | [new] | `BOXARR_METADATA_REFRESH_INTERVAL` (default `24h`) | Refreshes TMDB/TVDB catalog to pick up new seasons/episodes; recomputes wanted-but-missing (FR-CAT-5, `03`/`06`). |

**Wiring rule (verified `02` Recommendations 3):** add torrent/reconcile/metadata loops as **parallel `Run` goroutines gated on config flags** (`cfg.TorrentsEnabled`, reconcile/metadata always-on), **not** as branches inside the usenet loops. They touch disjoint rows (filtered by `protocol`/state) and share only the store. Route every new `time.Now` read through `worker.timeNow` (verified `clock.go`, `02` Recs 7) for test control.

---

## 5. HTTP serving & namespaces

Keep the `Server`-with-injected-deps shape and the chi router. `Server` holds `*store.Store`, `*config.Config`, `*slog.Logger`, plus optional `Checker` (health) and `HealReporter`, attached post-construction via `SetHealth`/`SetHealReporter` (verified `04` §2). `writeJSON`, the `param()` (query-then-form) helper, and the constant-time `subtle.ConstantTimeCompare` api-key check are **kept** and reused for the new authed surfaces. The `for _, base := range []string{"/api","/sabnzbd/api"}` loop and all `mode`-dispatched handlers are **deleted** (§2 step 7).

Target router (`func (s *Server) Router() http.Handler`):

```go
r := chi.NewRouter()
r.Get("/healthz", s.handleHealthz)                      // [reused] kept verbatim (Pinger repointed, §2 step 9)
r.Mount("/api/v1", s.v1.Router())                       // [new] Boxarr's own REST API  -> 04-internal-api.md
r.Mount("/sonarr/api/v3", s.seerr.SonarrRouter())       // [new] Sonarr v3 emulation    -> 05-seerr-emulation.md
r.Mount("/radarr/api/v3", s.seerr.RadarrRouter())       // [new] Radarr v3 emulation    -> 05-seerr-emulation.md
r.Handle("/*", s.web.SPAHandler())                       // [new] embedded SPA, last (catch-all)
return r
```

| Namespace | Owner | Auth | Notes |
|---|---|---|---|
| `/healthz` | `internal/api` | none | 200 `"ok"` or 503 `"unhealthy: <err>"`; `Health.Check` pings DB then TorBox (5-min cache) — kept verbatim (verified `04` §2). |
| `/api/v1/...` | `internal/api/v1` | Boxarr key | JSON/REST consumed by the SPA; full surface in `04-internal-api.md`. |
| `/sonarr/api/v3/...` | `internal/api/seerr` | `X-Api-Key` **and** `apikey` query (Servarr convention) | Sonarr-flavored; advertises a Sonarr **3.x.x.x** version (`00` §5.3); `05-seerr-emulation.md`. |
| `/radarr/api/v3/...` | `internal/api/seerr` | `X-Api-Key` **and** `apikey` query | Radarr-flavored; advertises a Radarr version; `05-seerr-emulation.md`. |
| `/` (catch-all) | `internal/web` | none | Embedded React SPA. |

**Embedded SPA (locked `00` §2):** `internal/web` embeds the Vite build with `//go:embed dist` and serves it. Because it is a client-routed SPA, the handler **falls back to `index.html`** for any path that is not a real asset (so deep links like `/movies/123` load the SPA shell, not a 404). It is mounted as the **last** route so the API/Seerr/health namespaces win first.

```go
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// SPAHandler serves the embedded Vite build, falling back to index.html so
// client-side routes resolve. Mounted last in the chi router.
func SPAHandler() http.Handler {
	sub, _ := fs.Sub(distFS, "dist")
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(sub, trimLeadingSlash(r.URL.Path)); err != nil {
			r.URL.Path = "/" // unknown path -> index.html (SPA shell)
		}
		fileServer.ServeHTTP(w, r)
	})
}
```

**The Dockerfile frontend stage `COPY --from=frontend /web/dist ./internal/web/dist` (verified `04` Recommendations §Dockerfile) puts `dist/` where `//go:embed all:dist` expects it.** During local dev a committed placeholder `internal/web/dist/index.html` keeps `go build` green when the frontend has not been built (`07`/`08` own the exact placeholder + build wiring). The `http.Server` keeps `ReadHeaderTimeout: 10*time.Second` (verified `04` §3).

---

## 6. Dependency inventory

**Reused Go libraries** (already in `go.mod`, verified `/tmp/sab2torbox/go.mod`):

| Module | Version | Role | Direct? |
|---|---|---|---|
| `github.com/kelseyhightower/envconfig` | `v1.4.0` | `BOXARR_*` config parsing. | direct |
| `github.com/pressly/goose/v3` | `v3.27.1` | Embedded SQLite migrations (dialect `sqlite3`). | direct |
| `modernc.org/sqlite` | `v1.50.1` | **CGo-free** SQLite driver (registers driver name `sqlite`). | direct |
| `github.com/go-chi/chi/v5` | `v5.2.5` | HTTP router. **Promote indirect → direct** (§2 step 5). | **→ direct** |
| `github.com/cenkalti/backoff/v4` | `v4.3.0` | Capped exponential retry in the submitters (verified `submitter.go:7`). | indirect (used via worker) |
| `github.com/sethvargo/go-retry` | `v0.3.0` | Retry helper available in the foundation. | indirect |
| `github.com/google/uuid` | `v1.6.0` | IDs where needed. | indirect |
| `golang.org/x/sync` | `v0.20.0` | Sync primitives. | indirect |

**New Go libraries** (parsing, `00` §5.2 — pure-Go, no sidecar):

| Module | Version | License | Role |
|---|---|---|---|
| `github.com/chill-institute/torrentname` | `v1.4.0` | MIT | Primary release-name parser; provides `EpisodeEnd`/`Complete`/`Season` used by `importer/release` (`06`). |
| `github.com/nssteinbrenner/anitogo` | `v1.0.0` | MPL-2.0 | Anime absolute-numbering parser supplement. |

Three **in-house regexes** Boxarr owns (no new dep): adjacent multi-episode `S01E01E02`, daily-show `YYYY-MM-DD`, bare season packs (`S01` without a `COMPLETE` keyword) — owned by `importer/release` (`00` §5.2; `06`).

**CGO invariant (locked, `00` §2):** `CGO_ENABLED=0` must hold so the binary stays cross-compilable into `gcr.io/distroless/static-debian12:nonroot`. `modernc.org/sqlite` is the CGo-free driver precisely for this. **Neither new dependency pulls cgo** — `torrentname` and `anitogo` are zero-/pure-Go (`00` §5.2). The Python/JS `guessit`/`parsett` route is **rejected** because it would break the single-binary distroless deploy (`00` §5.2). Any future dependency must be vetted against this invariant before adding.

**Frontend dependencies** (React + TS + Vite + pnpm, hardened per requirements §17.2) are owned by `07-frontend.md` / `08-config-deploy-ci.md`; they live in `web/` and never enter `go.mod`. They are embedded at build time, so the running app loads **no third-party scripts/styles at runtime** (FR-SEC-5).

---

## 7. Cross-links

- `00-decisions-and-assumptions.md` — canonical decisions, assumptions A–D, conventions, runtime-verify register.
- `02-data-model.md` — full SQLite DDL: migrations `004+`, jobs torrent/media columns, catalog/notify/webdav/settings tables, extended state machine, store methods.
- `03-external-contracts.md` — TorBox (torrents/`checkcached`/`/user/me`), Prowlarr, TMDB, TVDB, Plex request/response contracts the new clients implement.
- `04-internal-api.md` — the `/api/v1` REST surface that `internal/api/v1` serves.
- `05-seerr-emulation.md` — the Sonarr v3 / Radarr v3 payloads that `internal/api/seerr` serves.
- `06-pipelines.md` — grab/import/delete/heal/reconcile state machines, selection score, namer, release parser, TorBox limits — the behavior hosted by `worker`, `selection`, `importer`, `reconcile`.
- `07-frontend.md` — React+TS+Vite SPA built into `web/dist` and embedded by `internal/web`.
- `08-config-deploy-ci.md` — `BOXARR_*` env reference, multi-stage Dockerfile (+frontend stage), CI/release workflows, compose.

## Definition of done

The repository builds as `github.com/radaiko/boxarr` with binary `/boxarr`: the module path and every import are renamed, `cmd/boxarr` produces the binary, `chi` is a direct require, Go is pinned to 1.25, and the DB default is `/config/boxarr.db`; the entire SABnzbd surface — `handleAPI`, the `/api` + `/sabnzbd/api` router loop, all SAB modes, `internal/api/responses.go`, and `SAB_API_KEY` — is deleted while `internal/torbox`, `internal/store`, `internal/job`, and `internal/worker` are carried over with behavior unchanged; the new packages (`prowlarr`, `metadata/{tmdb,tvdb}`, `plex`, `catalog`, `selection`, `importer/{namer,release}`, `reconcile`, `notify`, `api/v1`, `api/seerr`, `web`) exist with the responsibilities tabulated above; `worker.Run` launches the reused usenet loops plus parallel torrent submitter/poller, protocol-branching deleter/healer, a 15-minute reconciler, and a daily metadata-refresh — all sharing the single SQLite writer with per-loop backoff state; the chi router serves `/api/v1`, `/sonarr/api/v3`, `/radarr/api/v3`, `/healthz` (Pinger repointed to Boxarr's deps, 5-minute TorBox cache kept), and the `go:embed` SPA with index.html fallback as the catch-all; and `CGO_ENABLED=0` still holds because the only new Go deps (`torrentname`, `anitogo`) are pure-Go — `gofmt -s` and `golangci-lint` clean, `go build ./...` and `go test ./... -race` green.
