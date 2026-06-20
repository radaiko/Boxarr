# Boxarr — Frontend (Spec 07)

**Date:** 2026-06-20
**Status:** Approved for planning

This spec owns the **React + TypeScript + Vite single-page app** (`web/`), its **routes/views**, the **typed client** over `/api/v1`, the **build → embed** pipeline, and the **npm/pnpm supply-chain hardening** (requirements §6, §17.2). Cross-cutting decisions, naming, namespaces, and the runtime-verify register live in `00-decisions-and-assumptions.md` and are **not** re-decided here. The SPA is consumed by the REST surface in `04-internal-api.md`; it is embedded and served by `internal/web` (`01-architecture-and-packages.md` §5); the build stage and CI step are owned by `08-config-deploy-ci.md`. Status/category vocabularies the views render are defined in `02-data-model.md` §2.

---

## 1. Stack & principles (locked)

| Concern | Decision | Rationale |
|---|---|---|
| Framework | **React 18 + TypeScript**, function components + hooks only | The stack the implementer is most fluent in (`00` §2); supersedes the Svelte choice in requirements §4.1/§17.2 (`00` §3 19.2). |
| Build tool | **Vite 5** (`@vitejs/plugin-react`) | Fast static build; `vite build` → `web/dist`; no Node at runtime (`00` §2 locked). |
| Package manager | **pnpm**, pinned via `packageManager` / corepack (FR-SEC-6) | Strict isolation, content-addressable store, integrity-checked lockfile (requirements §17.2). |
| Runtime delivery | **Static assets embedded in the Go binary via `embed.FS`** | One process serves API + UI; **no Node, no CDN at runtime** (FR-CI-4, FR-SEC-5). |
| Routing | **Client-side only**, tiny hand-rolled hash-free router over the History API (≈80 LOC, no dep) | Avoids `react-router`'s transitive tree; SPA index.html fallback in `internal/web` makes deep links work (`01` §5). |
| Data fetching | **`fetch` + a thin typed client** over `/api/v1`; no Axios, no React Query, no Redux | Keeps the dependency surface minimal (FR-SEC-4). Server state held in component-level hooks + a tiny SWR-style cache we own. |
| Styling | **Plain CSS + CSS variables** (one `theme.css`), co-located `*.module.css` | No Tailwind/component-library transitive trees (FR-SEC-4). |
| Icons | **Inline SVG** components in `src/components/icons/` | Zero icon-font/library dependency. |

**Invariants:**

1. **No heavy UI/component library.** No MUI/Chakra/Ant/Tailwind/Bootstrap. Each runtime dependency is a deliberate, reviewed decision (FR-SEC-4). The runtime dependency list is **exactly** `react` + `react-dom` (§4).
2. **No runtime third-party fetch.** Every JS/CSS/font/icon is built at build time and embedded (FR-SEC-5). Posters/backdrops are the *only* external images, and they are loaded from TMDB's image CDN **via the backend** (the SPA requests `/api/v1/.../poster` which proxies/redirects — see `04-internal-api.md`); the SPA itself loads no third-party `<script>`/`<link>`.
3. **Client-side routing only.** No SSR, no SSG, no Node server. The Go binary is the only server (`00` §2).
4. **The SPA never scans the WebDAV mount.** All library/status/storage data is read from `/api/v1` (which reads Boxarr's DB) — **FR-UI-9** enforced at the client by never having a filesystem code path.
5. **TypeScript `strict: true`.** No `any` in committed code; the API types are a single hand-maintained `src/api/types.ts` mirroring `04-internal-api.md`.

---

## 2. App structure

### 2.1 Directory layout (`web/`)

```
web/
├── package.json                 pinned deps, packageManager pin, scripts (§4)
├── pnpm-lock.yaml               committed, authoritative (--frozen-lockfile) (§4)
├── .npmrc                       save-exact + ignore-scripts (§4)
├── tsconfig.json                strict: true
├── vite.config.ts               base '/', build.outDir 'dist', /api+/healthz dev proxy
├── eslint.config.js             flat config; pnpm lint
├── index.html                   single mount point <div id="app">
├── public/
│   └── favicon.svg              inline SVG favicon (no external)
└── src/
    ├── main.tsx                 createRoot(...).render(<App/>)
    ├── App.tsx                  <Router> + <Shell> (sidebar + notification badge + <Outlet>)
    ├── router.tsx               hand-rolled History-API router (§2.3)
    ├── theme.css                CSS variables (color, spacing, radius); dark default
    ├── api/
    │   ├── client.ts            typed fetch wrapper over /api/v1 (§2.4)
    │   ├── types.ts             TS mirror of /api/v1 DTOs (-> 04-internal-api.md)
    │   └── hooks.ts             useQuery/useMutation (tiny SWR-style cache, §2.4)
    ├── components/
    │   ├── Shell.tsx            app frame: nav, header, NotificationBadge
    │   ├── PosterGrid.tsx       reusable poster grid (series + movies)
    │   ├── PosterCard.tsx       poster + title + status pill
    │   ├── StatusPill.tsx       MediaStatus -> color/label (§3.7)
    │   ├── ReleaseTable.tsx     ranked/filterable release list (manual search)
    │   ├── Toolbar.tsx          search box + sort/filter controls
    │   ├── EmptyState.tsx       reusable empty/error/loading states
    │   ├── ConfirmDialog.tsx    delete confirmation
    │   └── icons/               inline SVG icon components
    └── views/
        ├── SeriesLibrary.tsx    /series                (FR-UI-1)
        ├── SeriesDetail.tsx     /series/:id            (FR-UI-2)
        ├── MoviesLibrary.tsx    /movies                (FR-UI-1)
        ├── MovieDetail.tsx      /movies/:id            (FR-UI-3)
        ├── ManualSearch.tsx     /search                (FR-UI-4)
        ├── WebDAVView.tsx       /webdav                (FR-UI-5)
        ├── Storage.tsx          /storage               (FR-UI-6)
        ├── Notifications.tsx    /notifications         (FR-UI-7)
        └── Settings.tsx         /settings              (FR-UI-8)
```

**The Vite build emits to `web/dist`; the Dockerfile copies `web/dist → internal/web/dist`, which `//go:embed all:dist` consumes** (verified `01` §5; `04-sab-api-config-ci.md` Recommendations §Dockerfile). A committed placeholder `internal/web/dist/index.html` keeps `go build` green before the first frontend build (`01` §5; wiring owned by `08`).

### 2.2 Routes → views (locked)

| Path | View | Requirement | Data source (`/api/v1`) |
|---|---|---|---|
| `/` | redirect → `/series` | — | — |
| `/series` | `SeriesLibrary` | FR-UI-1 | `GET /series` |
| `/series/:id` | `SeriesDetail` | FR-UI-2 | `GET /series/{id}` (seasons+episodes) |
| `/movies` | `MoviesLibrary` | FR-UI-1 | `GET /movies` |
| `/movies/:id` | `MovieDetail` | FR-UI-3 | `GET /movies/{id}` |
| `/search` | `ManualSearch` | FR-UI-4 | `GET /search?...`; `POST /grab` |
| `/webdav` | `WebDAVView` | FR-UI-5 | `GET /webdav` |
| `/storage` | `Storage` | FR-UI-6 | `GET /storage` |
| `/notifications` | `Notifications` | FR-UI-7 | `GET /notifications`; `POST /notifications/{id}/read` |
| `/settings` | `Settings` | FR-UI-8 | `GET/PUT /settings`; `POST /settings/test/{conn}` |

Deep links (`/movies/123`, `/series/45`) resolve because `internal/web`'s `SPAHandler` falls back to `index.html` for non-asset paths (verified `01` §5). The router then reads `window.location.pathname` and renders the matching view.

### 2.3 Routing approach (hand-rolled, no dependency)

A ≈80-LOC `router.tsx` over the **History API** — chosen over `react-router` to keep the transitive tree at zero (FR-SEC-4). Contract:

```ts
// router.tsx (shape)
type Route = { pattern: string; view: React.ComponentType<{ params: Params }> };
const routes: Route[] = [
  { pattern: "/series", view: SeriesLibrary },
  { pattern: "/series/:id", view: SeriesDetail },
  { pattern: "/movies", view: MoviesLibrary },
  { pattern: "/movies/:id", view: MovieDetail },
  { pattern: "/search", view: ManualSearch },
  { pattern: "/webdav", view: WebDAVView },
  { pattern: "/storage", view: Storage },
  { pattern: "/notifications", view: Notifications },
  { pattern: "/settings", view: Settings },
];

export function useRoute(): { path: string };          // subscribes to popstate
export function navigate(to: string): void;            // history.pushState + dispatch
export function Link(props: { to: string; ... }): JSX.Element; // <a> that calls navigate, preventDefault
```

1. `navigate(to)` calls `history.pushState({}, "", to)` then dispatches a synthetic `popstate`-like event a `useSyncExternalStore` subscribes to.
2. `<Link>` renders a real `<a href>` (so middle-click / open-in-new-tab work) but `preventDefault`s left-clicks and calls `navigate`.
3. Path matching is exact-segment with `:param` capture; first match wins; no match → an in-app **404 view** (not a server 404, since the server already served `index.html`).
4. Base path is `/` (locked `00` §6 — the SPA owns the catch-all). `vite.config.ts` sets `base: "/"`.

### 2.4 Data fetching (`fetch` + thin typed client)

No state-management library and no data-fetching library. `api/client.ts` is a typed wrapper; `api/hooks.ts` is a tiny SWR-style cache (~120 LOC we own).

```ts
// api/client.ts (shape)
const BASE = "/api/v1";
async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(BASE + path, {
    method,
    headers: { "Content-Type": "application/json", "X-Api-Key": apiKey() },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.status === 204 ? (undefined as T) : (res.json() as Promise<T>);
}
export const api = {
  series:        () => req<Series[]>("GET", "/series"),
  seriesDetail:  (id: number) => req<SeriesDetail>("GET", `/series/${id}`),
  movies:        () => req<Movie[]>("GET", "/movies"),
  movieDetail:   (id: number) => req<MovieDetail>("GET", `/movies/${id}`),
  search:        (q: SearchQuery) => req<Release[]>("GET", `/search?${qs(q)}`),
  grab:          (b: GrabRequest) => req<GrabResult>("POST", "/grab", b),
  webdav:        () => req<WebDAVItem[]>("GET", "/webdav"),
  storage:       () => req<Storage>("GET", "/storage"),
  notifications: () => req<Notification[]>("GET", "/notifications"),
  markRead:      (id: number) => req<void>("POST", `/notifications/${id}/read`),
  settings:      () => req<Settings>("GET", "/settings"),
  saveSettings:  (s: Settings) => req<Settings>("PUT", "/settings", s),
  testConn:      (c: ConnName) => req<TestResult>("POST", `/settings/test/${c}`),
};
```

```ts
// api/hooks.ts (shape) — minimal owned SWR
function useQuery<T>(key: string, fn: () => Promise<T>): {
  data?: T; error?: Error; loading: boolean; refetch: () => void;
};
function useMutation<A, R>(fn: (a: A) => Promise<R>): {
  mutate: (a: A) => Promise<R>; pending: boolean; error?: Error;
};
```

**Quirks to bake in (data layer):**

1. **Auth key.** `X-Api-Key` carries Boxarr's `/api/v1` key on every call (matches `01` §5 — `/api/v1` is "Boxarr key" authed). The key is **injected at build/serve time**, not hardcoded in source: `apiKey()` reads a `<meta name="boxarr-api-key">` value the backend stamps into the served `index.html`, OR a same-origin session — exact mechanism is owned by `04-internal-api.md`; default to the meta tag, fallback to no header for a same-origin cookie session. (Runtime-verify: confirm `04`'s chosen `/api/v1` auth mode.)
2. **204 No Content** on mutations (mark-read, delete) → resolve `undefined`, not `.json()`.
3. **Cache invalidation is explicit:** mutations call `invalidate(key)` for affected queries (e.g. `grab` invalidates the relevant detail + `/storage`); no global magic.
4. **Polling for in-flight state.** Detail views with a `downloading`/`searching` item poll their `GET` every **5s** (component-level `setInterval` cleared on unmount); libraries do **not** auto-poll (FR-NC-9 cadence is server-side; the SPA refetches on focus). The notification badge polls `GET /notifications?unread_count=1` every **30s**.
5. **Errors surface as toasts + an inline `EmptyState`**; never a blank screen.

### 2.5 Shared components

| Component | Used by | Behavior |
|---|---|---|
| `Shell` | all | Sidebar nav (Series, Movies, Search, WebDAV, Storage, Notifications, Settings), header, `NotificationBadge`. |
| `PosterGrid` / `PosterCard` | Series/Movies libraries | Responsive CSS-grid of posters; lazy `loading="lazy"` images; click → detail. Status pill overlay. |
| `StatusPill` | libraries + details + webdav | Maps `MediaStatus` (`02` §2.2) / job `state` / webdav `category` to a colored label (§3.7). |
| `ReleaseTable` | Manual search | Sortable/filterable table of `Release`; grab button per row; cached/seeders/quality badges. |
| `Toolbar` | libraries + search + webdav | Free-text filter box + sort dropdown + protocol/status filter chips. |
| `EmptyState` | all | Unified loading / empty / error visuals. |
| `ConfirmDialog` | details, webdav, notifications | Confirms destructive actions (delete from TorBox). |

---

## 3. Views (FR-UI mapping, exhaustive)

### 3.0 FR-UI → view traceability

| FR | View(s) | What it satisfies |
|---|---|---|
| **FR-UI-1** | `SeriesLibrary`, `MoviesLibrary` | Two primary sections, poster-based libraries backed by TMDB artwork. |
| **FR-UI-2** | `SeriesDetail` | Seasons + episodes with per-episode status (`02` §2.2). |
| **FR-UI-3** | `MovieDetail` | Movie status (wanted/searching/downloading/available/expired-broken). |
| **FR-UI-4** | `ManualSearch` (+ entry points in detail views) | Ranked, filterable release list → grab; Sonarr/Radarr-style interactive search. |
| **FR-UI-5** | `WebDAVView` | Every mount item with category + size. |
| **FR-UI-6** | `Storage` | Used size vs plan limits + monthly usage/cooldown. |
| **FR-UI-7** | `Notifications` | Notification center, newest-first, unread badge. |
| **FR-UI-8** | `Settings` | All connections + options + test buttons. |
| **FR-UI-9** | *all libraries/details* | Data read from `/api/v1` (DB-backed), never a WebDAV scan (§1 invariant 4). |

### 3.1 Series library — `SeriesLibrary` (`/series`)

Poster grid (`PosterGrid`) of monitored series from `GET /series`. Each `PosterCard` shows poster, title `(year)`, a roll-up status pill, and a small "have/total episodes" badge (e.g. `18/24`). `Toolbar` gives free-text title filter + sort (title / recently added / most-wanted) + a status filter chip set. A "+ Add series" button opens an inline TMDB/TVDB lookup (`GET /search?type=series-lookup`) → add (`POST /series`) — add+lookup details in `04`. Empty state when no series. Click → `/series/:id`.

### 3.2 Series detail — `SeriesDetail` (`/series/:id`)

Header: backdrop, poster, title, year, TMDB `status` string (e.g. `Returning Series` / `Ended`, `02` line 244), monitored toggle, overflow menu (refresh metadata, delete series). Below, **seasons as collapsible sections**; each season has a monitored toggle and a roll-up; within it an **episode table** with columns: `S×E`, title, air date, **per-episode status pill** (`wanted`/`searching`/`downloading`/`available`/`expired_broken`/`missing` — `02` §2.2), size (if available), and per-episode actions: **Search** (opens `ManualSearch` scoped to that episode), **Delete** (`ConfirmDialog` → `DELETE /episode/{id}` propagates to TorBox, `02` deleter), and **Heal** when `expired_broken`. A season header offers "Search season" (season-pack scope) and "Monitor/Unmonitor season". Air-date-aware: unaired episodes render `missing` and grey, never offering Search (FR-CAT-4). Polls every 5s while any episode is `searching`/`downloading` (§2.4 quirk 4).

### 3.3 Movies library — `MoviesLibrary` (`/movies`)

Identical `PosterGrid` machinery to series, from `GET /movies`. `PosterCard` shows poster, title `(year)`, and a single movie status pill. Same `Toolbar` (filter/sort/status chips). "+ Add movie" → TMDB lookup (`GET /search?type=movie-lookup`) → `POST /movie`. Click → `/movies/:id`.

### 3.4 Movie detail — `MovieDetail` (`/movies/:id`)

Header (backdrop/poster/title/year), monitored toggle, status pill (FR-UI-3 vocabulary), TMDB `tmdb_status` string (`Released`/`Post Production`/…, `02` line 322), runtime, overflow menu. Primary actions: **Search** (→ `ManualSearch` scoped to this movie title+year), **Delete** (`ConfirmDialog` → `DELETE /movie/{id}`, propagates to TorBox), **Heal** when `expired_broken`. If a grab is in flight, show progress (percent + ETA from the linked job, `02` `progress_pct`/`eta_seconds`) and poll every 5s. Shows the resolved `library_path` when `available`.

### 3.5 Manual search panel — `ManualSearch` (`/search`)

The interactive search (FR-SR-1/2/3, FR-UI-4). Reachable as a top-level view (free-text) **and** scoped from any title/season/episode (deep-linked query params). Flow:

1. **Query input.** Free-text box, or pre-filled when arriving scoped (movie `title+year`; series `title + SxxEyy` or season pack). A protocol toggle (both / usenet / torrent) and category are derived from context (`type=movie` → cat `2000`; `type=tvsearch` → cat `5000`, per Prowlarr category space, `06-ext-prowlarr-api.md`).
2. **Results.** `GET /search` returns a `Release[]` ranked by the backend's selection score (`06-pipelines.md`); rendered in `ReleaseTable`. Columns (verified Prowlarr `ReleaseResource`, `06-ext-prowlarr-api.md` §ReleaseResource): **title**, **size**, **protocol** (`usenet`/`torrent`), **indexer**, **seeders/leechers** (torrent) or **grabs** (usenet), **detected quality**, **age** (from `publishDate`), and **cached** badge (torrents only, from `checkcached`, `00` §5.5 / TorBox `03`). A score column shows the backend rank.
3. **Sort/filter** client-side: sort by score / size / seeders / age; filter by protocol, cached-only, indexer, min-seeders.
4. **Grab.** Per-row "Grab" button → `POST /grab` with the release identity (the backend stores the artifact + dedups, FR-GP-1/5). On success: toast, invalidate the scoped item's detail + `/storage`, and the linked item flips to `searching`/`downloading`.
5. Loading shows a spinner with the indexer-aggregation note (Prowlarr returns partial results when some indexers fail, `06-ext-prowlarr-api.md`); empty state when no releases.

### 3.6 WebDAV view — `WebDAVView` (`/webdav`)

A table (one row per release folder) from `GET /webdav` (FR-WD-1): **name**, **size**, **category** pill (`movie`/`series`/`unknown`, `02` §3.4 `webdav_item.category`), **known** indicator (mapped to a Boxarr job vs not), **last seen**, **broken** flag. `Toolbar` filters by category (incl. an "Unknown only" chip) and free-text name; sort by size/name/last-seen. Unknown rows expose actions (FR-NC-2): **Adopt/categorize**, **Ignore**, **Delete from TorBox** (`ConfirmDialog`). This view reads the reconciler-maintained `webdav_item` table — never scans the mount itself (FR-UI-9, FR-NF-7).

### 3.7 Storage overview — `Storage` (`/storage`)

From `GET /storage`: **total used** = sum of `size` across usenet + torrent `mylist`s (FR-ST-1), shown against **plan limits** and **monthly usage/cooldown** from `/user/me` (FR-ST-2; field names runtime-verified per `00` §9 TorBox `/user/me`). A category breakdown (movies vs series, FR-ST-3) using Boxarr's mapping. **Plan tier + active slots in use + monthly usage/cooldown** also surface here (FR-LIM-4). Renders defensively: any field the backend could not populate (because `/user/me` shapes are runtime-verify, `00` §9) shows "—", never an error.

### 3.8 Notification center — `Notifications` (`/notifications`)

From `GET /notifications`, **newest-first** (`ORDER BY created_at DESC, id DESC`, `02` §3.3). Each row shows type (download completed / grab failed / TorBox failure / heal triggered/succeeded/failed / deletion completed / limit-cooldown reached / **unknown content** — FR-NC-2/3), a human summary rendered from the `payload` JSON (`02` line 374), timestamp, read/unread state, and per-type actions (unknown-content → adopt/ignore/delete; failed grab → re-search). Mark-as-read (`POST /notifications/{id}/read`) and "mark all read". The `Shell`'s `NotificationBadge` shows the unread count (`COUNT(*) WHERE read=0`, `02` §3.3) polled every 30s (§2.4 quirk 4); badge clears as items are read (FR-NC-4).

### 3.9 Settings — `Settings` (`/settings`)

From `GET /settings` (the `settings` key/value table, `02` §3.5) with a `PUT /settings` save. Grouped sections, each with a **Test** button (`POST /settings/test/{conn}` → `TestResult{ok,detail}` rendered as a green/red inline result):

| Group | Fields | Test |
|---|---|---|
| TorBox | API token | `test/torbox` (token + `/user/me`) |
| Prowlarr | URL, API key | `test/prowlarr` (`/api/v1/indexer`) |
| TMDB | API key | `test/tmdb` (`/configuration`) |
| TVDB | API key | `test/tvdb` (token exchange) |
| Plex | URL, token | `test/plex` (sections list) |
| Seerr (Sonarr/Radarr emul.) | shown API key(s) + base URLs to paste into Seerr | — (read-only display) |
| Library | movie root, TV root, symlink root, WebDAV mount root, torrent subpath | filesystem reachability |
| Options | poll/reconcile/metadata/search intervals, selection-score weights, TorBox limits, heal toggles | — |

Secrets render masked with a "reveal" toggle and are **never logged client-side** (NFR-4). Boolean/duration/int inputs map to the `BOXARR_*` config types (`08`). Note: env-provided config is read-only-with-override per `04`/`08`'s precedence rule; the UI flags which values are env-pinned. (Runtime-verify: confirm `04`'s settings read/write precedence vs env.)

### 3.10 Status → visual mapping (`StatusPill`, locked)

| Domain value | Source | Pill color | Label |
|---|---|---|---|
| `wanted` | `MediaStatus` (`02` §2.2) | amber | Wanted |
| `searching` | `MediaStatus` | blue (pulsing) | Searching |
| `downloading` | `MediaStatus` | blue | Downloading |
| `available` | `MediaStatus` | green | Available |
| `missing` | `MediaStatus` | grey | Missing |
| `expired_broken` | `MediaStatus` | red | Expired |
| `movie` / `series` / `unknown` | `webdav_item.category` (`02` §3.4) | teal / violet / grey | Movie / Series / Unknown |

---

## 4. Build + embed

### 4.1 Build

`vite build` compiles `web/src` → **`web/dist`** (`base: "/"`, hashed asset filenames). `pnpm build` is the canonical command (matches the proposed CI step, `04-sab-api-config-ci.md` Recommendations §CI). No Node is present at runtime — the output is plain static files.

```ts
// vite.config.ts (shape)
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
export default defineConfig({
  base: "/",
  plugins: [react()],
  build: { outDir: "dist", emptyOutDir: true, sourcemap: false },
  server: {
    // dev only — proxy backend so the SPA talks to a running boxarr
    proxy: { "/api": "http://localhost:8080", "/healthz": "http://localhost:8080" },
  },
});
```

`package.json` scripts: `dev` (`vite`), `build` (`tsc -b && vite build`), `lint` (`eslint .`), `preview` (`vite preview`).

### 4.2 Embed (cross-ref `01` §5 + `08`)

1. The Dockerfile **frontend stage** runs `pnpm install --frozen-lockfile` then `pnpm build`, producing `/web/dist` (verified `04-sab-api-config-ci.md` Recommendations §Dockerfile).
2. The Go build stage copies it into the embed path: `COPY --from=frontend /web/dist ./internal/web/dist` (verified `01` §5).
3. `internal/web` embeds it: `//go:embed all:dist` + `fs.Sub(distFS, "dist")`, served by `SPAHandler()` with **index.html fallback** for client routes, mounted as the **last** chi route so `/api/v1`, `/sonarr`, `/radarr`, `/healthz` win first (verified `01` §5).
4. **Local-dev placeholder.** A committed `internal/web/dist/index.html` keeps `go build`/`go test` green before any frontend build (verified `01` §5); the real build overwrites it. (Owned by `08`.)

**Result: one binary, one container — the SPA loads no third-party scripts/styles at runtime** (FR-CI-4, FR-SEC-5).

---

## 5. Supply-chain hardening (§17.2 re-expressed for React/pnpm)

The Svelte-specific wording in requirements §17.2 is **re-expressed for the React/pnpm toolchain** (`00` §3 19.2); every control carries over unchanged in intent.

| FR | Control (React/pnpm) |
|---|---|
| **FR-SEC-1** | Exact pinning — `.npmrc` `save-exact=true`, **no `^`/`~`** anywhere in `package.json`; `pnpm-lock.yaml` committed + authoritative; every install uses `--frozen-lockfile` and **fails on drift** (CI + image build, FR-CI-6). |
| **FR-SEC-2** | Vetted, not bleeding-edge — pin to versions published long enough to be vetted (cooldown), within maintained lines; never pin onto a known-vulnerable version; patches applied deliberately after review. |
| **FR-SEC-3** | No install scripts by default — `.npmrc` `ignore-scripts=true`; build scripts permitted **only** via an explicit pnpm `onlyBuiltDependencies` allowlist (esbuild, the one binary Vite needs). |
| **FR-SEC-4** | Minimal surface — runtime deps are **exactly** `react` + `react-dom`; routing, fetching, state, styling, icons are all in-house (§1). Every dep is a reviewed decision. |
| **FR-SEC-5** | No runtime third-party fetch — all JS/CSS embedded (§4); the running app loads no CDN scripts/styles. |
| **FR-SEC-6** | Pinned package manager — `packageManager` field pins the exact pnpm version (corepack), so CI and local use an identical toolchain. |
| **FR-SEC-7** | Controlled updates — Renovate with `minimumReleaseAge` cooldown; reviewed PRs only; advisory `pnpm audit` gate in CI (FR-CI-6). |

### 5.1 `.npmrc` (committed at `web/.npmrc`)

```ini
# Exact versions only (FR-SEC-1)
save-exact=true
# No lifecycle/postinstall scripts by default (FR-SEC-3) — closes the top npm malware vector
ignore-scripts=true
# Fail the install if the lockfile would change (defense in depth; CI also passes --frozen-lockfile)
frozen-lockfile=true
# Strict isolation — no phantom hoisting (pnpm default, stated explicitly)
node-linker=isolated
# Do not auto-pull a different pnpm; use the packageManager pin (FR-SEC-6)
manage-package-manager-versions=false
```

### 5.2 `package.json` skeleton (pinned; `web/package.json`)

Exact versions, no ranges (FR-SEC-1). Pinned to **vetted, currently-maintained** releases (FR-SEC-2); the versions below are the chosen defaults — bump only via the controlled-update path (§5.3). `onlyBuiltDependencies` is the **sole** build-script allowlist (FR-SEC-3): `esbuild` is the one dependency Vite needs to compile its native binary; nothing else may run a postinstall.

```json
{
  "name": "boxarr-web",
  "private": true,
  "type": "module",
  "packageManager": "pnpm@9.15.0",
  "engines": { "node": ">=20.11.0" },
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "lint": "eslint .",
    "preview": "vite preview"
  },
  "dependencies": {
    "react": "18.3.1",
    "react-dom": "18.3.1"
  },
  "devDependencies": {
    "@types/react": "18.3.12",
    "@types/react-dom": "18.3.1",
    "@vitejs/plugin-react": "4.3.4",
    "eslint": "9.17.0",
    "typescript": "5.7.2",
    "typescript-eslint": "8.18.1",
    "vite": "5.4.11"
  },
  "pnpm": {
    "onlyBuiltDependencies": ["esbuild"]
  }
}
```

**Runtime dependency tree is exactly two packages** (`react`, `react-dom`) — FR-SEC-4. Everything else is a `devDependency` that disappears at build time and never ships in the embedded bundle.

### 5.3 `renovate.json` (committed at repo root)

Controlled, cooldown-gated updates (FR-SEC-7). `minimumReleaseAge` enforces a cooldown so a freshly published — possibly compromised — version is never auto-pulled; updates land as reviewed PRs.

```json
{
  "$schema": "https://docs.renovatebot.com/renovate-schema.json",
  "extends": ["config:recommended"],
  "minimumReleaseAge": "21 days",
  "internalChecksFilter": "strict",
  "rangeStrategy": "pin",
  "lockFileMaintenance": { "enabled": true, "schedule": ["before 5am on monday"] },
  "packageRules": [
    {
      "matchManagers": ["npm"],
      "matchFileNames": ["web/**"],
      "rangeStrategy": "pin",
      "minimumReleaseAge": "21 days"
    },
    {
      "description": "Security fixes bypass the cooldown but still land as a reviewed PR",
      "matchManagers": ["npm"],
      "matchFileNames": ["web/**"],
      "vulnerabilityAlerts": { "minimumReleaseAge": "0 days" }
    },
    {
      "matchManagers": ["gomod"],
      "minimumReleaseAge": "21 days",
      "rangeStrategy": "pin"
    }
  ]
}
```

### 5.4 CI enforcement (owned by `08`, summarized)

The frontend CI job (`04-sab-api-config-ci.md` Recommendations §CI) runs, in `web/`: `pnpm install --frozen-lockfile` (fails on lockfile drift, FR-CI-6/FR-SEC-1), `pnpm lint`, `pnpm build`, and an **advisory** `pnpm audit` (non-blocking gate, FR-SEC-7). `corepack` honors the `packageManager` pin so the toolchain is identical to local (FR-SEC-6). The Docker `docker-build` (no-push) job exercises the frontend stage end-to-end. Exact YAML is owned by `08-config-deploy-ci.md`.

---

## 6. Cross-links

- `00-decisions-and-assumptions.md` — stack lock (React/Vite/embed, §2/§3 19.2), conventions, runtime-verify register (TorBox `/user/me` fields, settings auth mode).
- `01-architecture-and-packages.md` §5 — `internal/web` embed + `SPAHandler` index.html fallback + chi mount order.
- `02-data-model.md` §2/§3 — `MediaStatus` + job `state` vocabularies, `notification`/`webdav_item`/`settings` shapes the views render.
- `04-internal-api.md` — the `/api/v1` REST surface the typed client consumes (endpoint contracts, auth mode, lookup/add/grab/test payloads).
- `06-pipelines.md` — selection score (release ranking shown in `ManualSearch`), grab/delete/heal behaviors the action buttons trigger.
- `08-config-deploy-ci.md` — Dockerfile frontend stage, CI frontend job, `internal/web/dist` placeholder, `BOXARR_*` settings the Settings view edits.

## Definition of done

`web/` holds a React 18 + TypeScript + Vite SPA whose **only runtime dependencies are `react` and `react-dom`** (verified `package.json`), built with `pnpm build` to `web/dist` and embedded by `internal/web` via `//go:embed all:dist` with an index.html SPA fallback (`01` §5) — no Node, no CDN at runtime; the nine views (`SeriesLibrary`, `SeriesDetail`, `MoviesLibrary`, `MovieDetail`, `ManualSearch`, `WebDAVView`, `Storage`, `Notifications`, `Settings`) each satisfy their mapped FR-UI requirement (§3.0) reading data exclusively through the typed `/api/v1` client (FR-UI-9, never scanning WebDAV), with client-side routing via the hand-rolled History-API router; supply-chain controls hold — `.npmrc` `save-exact`+`ignore-scripts`, a committed `pnpm-lock.yaml` installed with `--frozen-lockfile`, a `packageManager` pnpm pin, a sole `onlyBuiltDependencies: ["esbuild"]` allowlist, and a `renovate.json` `minimumReleaseAge` cooldown — and CI runs `pnpm install --frozen-lockfile`, `pnpm lint`, `pnpm build`, and advisory `pnpm audit` green with `tsc` strict mode clean.
