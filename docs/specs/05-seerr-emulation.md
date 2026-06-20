# Boxarr — Seerr Emulation: Sonarr v3 + Radarr v3 Inbound Surfaces (Spec 05)

**Date:** 2026-06-20
**Status:** Approved for planning

This spec owns the **inbound** Servarr-compatible API surfaces that Overseerr and Jellyseerr (collectively "Seerr") call so they can add series and movies to Boxarr. Boxarr presents **two** v3 surfaces — a Sonarr-flavored one at `/sonarr/api/v3/...` and a Radarr-flavored one at `/radarr/api/v3/...` — that satisfy Seerr's connection "Test" and its add-media flow (requirements §10, FR-SEERR-1…7). This is the *only* place Boxarr **answers** Servarr requests; the *outbound* clients Boxarr calls (TorBox/Prowlarr/TMDB/TVDB/Plex) are owned by `03-external-contracts.md`. Boxarr's own SPA API lives at `/api/v1` (`04-internal-api.md`) and never collides with these surfaces (00 §6 — HTTP namespaces).

Everything below is grounded verbatim in `10-ext-seerr-servarr-emulation.md` (the verbatim Overseerr/Jellyseerr client payloads and the fields each Seerr validates) and in the real sab2torbox API server shape (`internal/api/handlers.go`). Cross-cutting decisions are fixed in `00-decisions-and-assumptions.md` (esp. §5.3 version-string rule, §6 namespaces, §9 Seerr runtime-verify register) and are **not** re-decided here. Catalog ingest targets and the seeded `quality_profile`/`root_folder`/`tag` rows come from `02-data-model.md`; lookup objects are built from TMDB/TVDB per `03-external-contracts.md`.

**The contract is asymmetric and lopsided in our favor: Seerr reads only a handful of fields.** Boxarr returns the **full, realistic shapes** anyway (extra fields are always ignored — verified `10-ext-seerr-servarr-emulation.md`), so a future Seerr version that starts reading another field does not break us.

---

## 1. Routing & auth (locked)

### 1.1 Two surfaces, one router factory

Boxarr mounts **two** sub-routers on the existing chi router (reuse `Server.Router()`, verified `handlers.go:56-65`) via `r.Mount`:

| Surface | Mount base | Serves | Backed by |
|---|---|---|---|
| **Sonarr v3** | `/sonarr/api/v3/...` | series lookup/add/update, the 5 config endpoints (incl. `languageprofile`), episode update-path | `series`/`season`/`episode` tables, TMDB+TVDB lookup |
| **Radarr v3** | `/radarr/api/v3/...` | movie lookup/add/update, the 4 config endpoints (no `languageprofile`) | `movie` table, TMDB lookup |

**Implement a single router factory parameterized by `kind ∈ {"sonarr","radarr"}`** (00 §6; `10` Recommendation 1) living in a new package `internal/servarr` (placement owned by `01-architecture-and-packages.md`; backing structs are the `servarr.QualityProfile`/`RootFolder`/`Tag` types in `02-data-model.md` §4.3). Both bases append `/api/v3` exactly — Seerr's `ServarrBase.buildUrl` always constructs `{scheme}://{host}:{port}{baseUrl}/api/v3` and appends nothing else (verified `10` Key Facts). **Every Boxarr endpoint lives under `/api/v3/`** (locked).

```go
// internal/servarr — mounted on the shared chi router (handlers.go:56)
r.Mount("/sonarr/api/v3", servarr.NewRouter(servarr.KindSonarr, deps))
r.Mount("/radarr/api/v3", servarr.NewRouter(servarr.KindRadarr, deps))
```

**Routing is case-insensitive-safe.** Seerr's client uses the path `/qualityProfile` internally but the wire path is lowercase `/qualityprofile` (verified `10` ENDPOINT 2). Boxarr registers the lowercase canonical paths and **lower-cases the request path before matching** (a tiny middleware) so `/qualityProfile` and `/qualityprofile` both resolve (`10` Key Facts — "Case-insensitive routing is safe").

### 1.2 Auth — accept the key via BOTH header and query param

**Boxarr accepts the api key via both `X-Api-Key` header and `apikey` query param, validated in constant time** (FR-SEERR-1; locked). This is broader than what either Seerr actually sends: **Overseerr and Jellyseerr send the key ONLY as the `?apikey=` query param — never as an `X-Api-Key` header** (verified `10` AUTH section: the axios `params` object in Seerr's `ExternalAPI` constructor). Boxarr nonetheless honors the header too, because (a) real Servarr accepts both and other Servarr clients (the `starr` Go lib, scripts) use the header, and (b) it costs nothing. **Precedence: `X-Api-Key` header first, then `apikey` query param** (mirrors the `param()` query-then-form helper at `handlers.go:84-88`, here extended to "header-then-query").

Reuse the **exact** constant-time comparator from sab2torbox (verified `handlers.go:78-80`):

```go
// internal/servarr — reused verbatim from internal/api (handlers.go:79)
func validAPIKey(got, want string) bool {
    return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// auth middleware (constant-time; header THEN query)
func (s *router) auth(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        got := r.Header.Get("X-Api-Key")
        if got == "" {
            got = r.URL.Query().Get("apikey") // Seerr always uses this one
        }
        if !s.keyMatches(got) { // constant-time vs EVERY configured key
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusUnauthorized) // 401
            _, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

`keyMatches` iterates **every** configured key (multiple Seerr instances allowed, §1.3) and `||`s the constant-time results **without early-return**, so timing does not leak which key (or how many) matched. **Missing/invalid key ⇒ HTTP 401** (`10` Recommendation 1).

### 1.3 Configuration — `BOXARR_SEERR_*` (chosen default + fallback)

Configured via `BOXARR_SEERR_*` (owned in full by `08-config-deploy-ci.md`; grounded in `04-sab-api-config-ci.md:399`). **08 is not yet authored**, so the chosen shape and fallback are pinned here and registered in 00 §9 (Seerr — "single vs multiple Seerr instances … must be defined during Boxarr design", `04-sab-api-config-ci.md:464`):

| Env var | Type | Default | Meaning |
|---|---|---|---|
| `BOXARR_SEERR_API_KEYS` | comma-separated slice | *(empty)* | Accepted api keys for **both** surfaces. Reuses envconfig slice support already proven by `Categories` (`04-sab-api-config-ci.md:399`). |
| `BOXARR_SEERR_API_KEY` | string | *(empty)* | **Fallback / single-instance** convenience alias; if set, appended to the key set. |

**Chosen default:** one shared key set spans both `/sonarr` and `/radarr` (single-operator product, 00 §1). **Fallback:** if richer per-surface keys are ever needed, add `BOXARR_SEERR_SONARR_API_KEY` / `BOXARR_SEERR_RADARR_API_KEY` (additive; the comparator already iterates a set). **If no key is configured, both surfaces refuse all requests with 401** (fail-closed) — Boxarr never exposes an unauthenticated add path. The operator gives the same key to Seerr's "API Key" field when adding the Boxarr Sonarr/Radarr server.

### 1.4 Body parsing & content negotiation

- **Request bodies are `application/json`.** Seerr's `ExternalAPI` constructor sets `'Content-Type': 'application/json'` as a default header on every POST/PUT (verified `10` Uncertainties — confirmed). Boxarr decodes the JSON body with `json.NewDecoder(r.Body).Decode(&v)`; **unknown fields are ignored** (no `DisallowUnknownFields`) so Overseerr-vs-Jellyseerr body differences (e.g. Jellyseerr's extra `monitorNewItems`/`monitorNewItems`) never 400.
- **Responses are `application/json`** via the reused `writeJSON` helper (verified `handlers.go:70-76`); Boxarr sets `Content-Type: application/json` explicitly.
- **Seerr sends `Accept: application/json`** on every request (verified `10` AUTH); Boxarr always answers JSON regardless.

---

## 2. Sonarr v3 surface

All paths below are under `/sonarr/api/v3/`. Each row gives the request Seerr issues and the **verbatim response JSON Boxarr returns**. Field shapes are lifted from `10-ext-seerr-servarr-emulation.md` (the Overseerr/Jellyseerr TypeScript interfaces). Fields Seerr actually reads are **bold** in prose; everything else is realistic padding that Seerr ignores.

### 2.1 `GET /sonarr/api/v3/system/status`

Seerr reads `urlBase` (both) and **`version`** (Jellyseerr only — `Number(version.split('.')[0])` to gate the `/languageprofile` call). **Boxarr advertises a Sonarr `3.x.x.x` version string, locked to `3.0.10.1567`** (00 §5.3 + 00 §9 Seerr register). **Why 3.x and not 4.x:** a `4.x` string makes Jellyseerr *skip* `/languageprofile`, but Overseerr *always* calls `/languageprofile` and a `4.x` Sonarr breaks Overseerr's v3 contract — Overseerr would 500 the test if it 404'd, and even with `languageprofile` served, advertising 4.x mismatches the v3 surface Boxarr emulates (verified `10` Key Facts + Uncertainties; cross-ref GitHub overseerr#3013, jellyseerr#602). Advertising 3.x means Jellyseerr *will* call `/languageprofile`, which Boxarr serves anyway (§2.6) — so 3.x satisfies **both** Seerrs. There is **no** `appName`/`instanceName` validation by either Seerr; Boxarr still returns `"appName": "Sonarr"` for fidelity (verified `10` — "Overseerr does NOT validate appName").

**Jellyseerr reads `system/status` synchronously and fails the Sonarr test if it errors** (no `.catch` around it, unlike Overseerr); Boxarr therefore **always returns a clean 200** (00 §9 Seerr register; `10` Key Facts).

```json
{
  "appName": "Sonarr",
  "instanceName": "Sonarr",
  "version": "3.0.10.1567",
  "buildTime": "2021-10-10T00:00:00Z",
  "isDebug": false,
  "isProduction": true,
  "isAdmin": false,
  "isUserInteractive": false,
  "startupPath": "/app/sonarr/bin",
  "appData": "/config",
  "osName": "ubuntu",
  "osVersion": "20.04",
  "isNetCore": true,
  "isMono": false,
  "isLinux": true,
  "isOsx": false,
  "isWindows": false,
  "isDocker": true,
  "mode": "console",
  "branch": "master",
  "authentication": "apikey",
  "sqliteVersion": "3.35.5",
  "migrationVersion": 0,
  "urlBase": "/sonarr",
  "runtimeVersion": "6.0.0",
  "runtimeName": "netcore",
  "startTime": "2021-10-10T00:00:00Z",
  "packageUpdateMechanism": "docker"
}
```

`urlBase` echoes the mount prefix (`/sonarr`); Overseerr reuses it only to display the configured base — it does not re-route on it (verified `10` ENDPOINT 1). `migrationVersion: 0` and the date constants are static — Seerr never inspects them.

### 2.2 `GET /sonarr/api/v3/qualityprofile`

Seerr reads only **`id`** and **`name`** (`QualityProfile = {id,name}`). Boxarr returns the seeded `quality_profile` rows (`02` §3.6 — id `1` "Any" is seeded and `is_default`), so the id Seerr later echoes in `POST /series` (`qualityProfileId`) resolves to a real row.

```json
[
  { "id": 1, "name": "Any" }
]
```

**Returning at least one profile is mandatory** — the dropdown must populate. Extra rows (e.g. operator-created profiles) appear automatically; Boxarr serves whatever `ListQualityProfiles` returns (`02` §5.6).

### 2.3 `GET /sonarr/api/v3/rootfolder`

Seerr's test maps each folder to `{id, path}` but the full shape carries **`accessible: true`** + **`freeSpace`** so it renders as usable. Boxarr returns the seeded TV root(s) (`02` §3.6 — id `1`, `media_kind='tv'`, default path `/data/tv`) and **computes `freeSpace`/`totalSpace`/`accessible` at request time**, not from the DB (`02` §3.6 note). `freeSpace`/`totalSpace` come from a `statfs` on the root path; `accessible` is `true` iff the path exists and is a writable directory.

```json
[
  {
    "id": 1,
    "path": "/data/tv",
    "accessible": true,
    "freeSpace": 107374182400,
    "totalSpace": 214748364800,
    "unmappedFolders": []
  }
]
```

**The `path` returned here MUST equal the value Boxarr accepts in `POST /series.rootFolderPath`** — Seerr sends back exactly what the operator picked from this dropdown (`10` Recommendation 11; `02` §3.6 quirk 2). `root_folder.path` is `UNIQUE` in the DB so the round-trip is exact. If the path is missing/unwritable, Boxarr still returns the row but with `accessible:false` and `freeSpace:0` so the operator sees the misconfiguration in Seerr's UI rather than getting a silent failure.

### 2.4 `GET /sonarr/api/v3/tag`

Seerr reads `{id,label}`. **An empty array is valid** (verified `10` ENDPOINT 4 + `02` §3.6 quirk 3 — `tag` seeded empty). Boxarr returns whatever `ListTags` yields, normally `[]`:

```json
[]
```

### 2.5 `GET /sonarr/api/v3/languageprofile` (Sonarr surface only)

**CRITICAL for Overseerr.** Overseerr **always** calls this during the Sonarr test; a non-2xx fails the whole test with HTTP 500 "Failed to connect to Sonarr". Jellyseerr calls it only when major version ≤ 3 — which Boxarr's `3.0.10.1567` triggers. **Boxarr therefore always returns 200 with a non-empty array** (verified `10` ENDPOINT 5; Recommendation 3). Seerr reads `{id,name}`:

```json
[
  { "id": 1, "name": "English" }
]
```

This is a static stub (Boxarr has no language-profile concept of its own — language filtering is out of scope, 00 §8). id `1` is what Seerr echoes back in `POST /series.languageProfileId`; Boxarr accepts and ignores it.

### 2.6 `GET /sonarr/api/v3/series/lookup?term=tvdb:{tvdbId}`

Issued by `SonarrAPI.getSeriesByTvdbId(id)` **before** both POST (add) and PUT (update). The query term is **always `tvdb:{id}`** for Boxarr's purposes — Seerr looks up by the TVDB id it holds for the request.

**Behavior gates (verified `10` LOOKUP series):**
1. Seerr checks `response.data[0]` — **an empty array throws "Series not found"** and the request fails. **Boxarr always returns at least one element** when the series resolves.
2. Seerr then checks `response.data[0].id` — **present & non-zero ⇒ series already exists ⇒ Seerr goes to the PUT update path; absent/0 ⇒ POST add path**. This is the integration switch: **Boxarr includes `id` only when the series is already in its catalog** (`GetSeriesByTVDB` hit), and **omits `id` entirely** otherwise (`10` Recommendation 4).

**How Boxarr builds the lookup object** (cross-ref `03` TMDB/TVDB): the inbound `tvdbId` is series-level. Boxarr resolves it to TMDB via `GET /find/{tvdbId}?external_source=tvdb_id` → `tv_results[0].id` (`03` TMDB "resolve tvdb→tmdb"; if `tv_results` is empty, fall back to TVDB `/search/remoteid/{tvdbId}` — `03` TVDB, 00 §9). Then `GET /tv/{id}?append_to_response=external_ids` supplies the header fields and `seasons[]` summary. **The `seasons[]` array is the load-bearing field**: Overseerr maps it (forcing every season `monitored:false`) to build the `POST /series` body, then flips the requested season numbers to `true` (verified `10` LOOKUP + POST series; Recommendation 6). Boxarr must therefore return **every** season number (including season 0/Specials) with accurate `seasonNumber`.

`remotePoster` is a fully-qualified TMDB image URL reconstructed from the cached `secure_base_url` + `w500` + `poster_path` (`03` TMDB Images). `seriesType` is `"anime"` when ordering is absolute (TVDB), else `"standard"`/`"daily"` (`03` TVDB quirk 8; `02` `series.series_type`).

Response for a **not-yet-added** series (drives the POST path — **no `id` field**):

```json
[
  {
    "title": "Breaking Bad",
    "sortTitle": "breaking bad",
    "seasonCount": 5,
    "status": "ended",
    "overview": "A chemistry teacher diagnosed with cancer...",
    "network": "AMC",
    "airTime": "21:00",
    "images": [
      { "coverType": "poster", "url": "https://image.tmdb.org/t/p/w500/poster.jpg" }
    ],
    "remotePoster": "https://image.tmdb.org/t/p/w500/poster.jpg",
    "seasons": [
      { "seasonNumber": 0, "monitored": false },
      { "seasonNumber": 1, "monitored": true },
      { "seasonNumber": 2, "monitored": true },
      { "seasonNumber": 3, "monitored": true },
      { "seasonNumber": 4, "monitored": true },
      { "seasonNumber": 5, "monitored": true }
    ],
    "year": 2008,
    "path": "",
    "profileId": 0,
    "languageProfileId": 1,
    "seasonFolder": true,
    "monitored": false,
    "useSceneNumbering": false,
    "runtime": 47,
    "tvdbId": 81189,
    "tvRageId": 0,
    "tvMazeId": 0,
    "firstAired": "2008-01-20T00:00:00Z",
    "seriesType": "standard",
    "cleanTitle": "breakingbad",
    "imdbId": "tt0903747",
    "titleSlug": "breaking-bad",
    "certification": "TV-MA",
    "genres": ["Crime", "Drama", "Thriller"],
    "tags": [],
    "added": "0001-01-01T00:00:00Z",
    "ratings": { "votes": 0, "value": 0 },
    "qualityProfileId": 0,
    "statistics": {
      "seasonCount": 5,
      "episodeFileCount": 0,
      "episodeCount": 0,
      "totalEpisodeCount": 62,
      "sizeOnDisk": 0,
      "releaseGroups": [],
      "percentOfEpisodes": 0
    }
  }
]
```

For an **already-tracked** series, the identical object but **with `"id": <series.id>`** (and `"path": series.library_path`, `"monitored": true`, real `qualityProfileId`) so Seerr takes the PUT path (§2.8). **No empty array on "found-in-TMDB"** — only a genuinely unresolvable `tvdbId` yields `[]` (and Seerr's "not found" is the correct user-facing result).

### 2.7 `POST /sonarr/api/v3/series` (add)

**Exact body Overseerr sends** (verbatim `10` POST series; Jellyseerr additionally sends `monitorNewItems`):

```json
{
  "tvdbId": 81189,
  "title": "Breaking Bad",
  "qualityProfileId": 1,
  "languageProfileId": 1,
  "seasons": [
    { "seasonNumber": 0, "monitored": false },
    { "seasonNumber": 1, "monitored": true },
    { "seasonNumber": 2, "monitored": false }
  ],
  "tags": [],
  "seasonFolder": true,
  "monitored": true,
  "rootFolderPath": "/data/tv",
  "seriesType": "standard",
  "addOptions": {
    "ignoreEpisodesWithFiles": true,
    "searchForMissingEpisodes": true
  }
}
```

Notes baked in (verified `10`): **Overseerr does NOT send `titleSlug`** in the series body; **`addOptions` is `{ ignoreEpisodesWithFiles, searchForMissingEpisodes }`** — *not* `searchForCutoffUnmetEpisodes`, *not* a `monitor` string; the `seasons[]` carries the requested-monitoring per season (the partial-season mechanism). Jellyseerr adds `"monitorNewItems": "all"|"none"`. Boxarr decodes all of these and **ignores unknown extras**.

Ingest behavior is owned by §4. **Response (HTTP 201) MUST echo a non-zero numeric `id`** — Overseerr checks `if (createdSeriesResponse.data.id)` and throws "Failed to add series to Sonarr" on a falsy id (verified `10` — CRITICAL). `id` is the real `series.id` assigned by `CreateSeries` (`02` §5.2):

```json
{
  "id": 42,
  "title": "Breaking Bad",
  "tvdbId": 81189,
  "qualityProfileId": 1,
  "languageProfileId": 1,
  "rootFolderPath": "/data/tv",
  "seasonFolder": true,
  "monitored": true,
  "seriesType": "standard",
  "titleSlug": "breaking-bad",
  "path": "/data/tv/Breaking Bad (2008)",
  "seasons": [
    { "seasonNumber": 0, "monitored": false },
    { "seasonNumber": 1, "monitored": true },
    { "seasonNumber": 2, "monitored": false }
  ],
  "tags": [],
  "added": "2026-06-20T00:00:00Z",
  "images": [],
  "statistics": {
    "seasonCount": 2,
    "episodeFileCount": 0,
    "episodeCount": 0,
    "totalEpisodeCount": 0,
    "sizeOnDisk": 0,
    "releaseGroups": [],
    "percentOfEpisodes": 0
  }
}
```

`added` is the real `series.added_at`; `path` is `series.library_path` (resolved by the namer, 00 §6 / `06`). The echoed `seasons[]` mirrors what was requested (monitored flags as received).

### 2.8 Episode update-path endpoints (series already exists)

These fire **only on the update path** — when the lookup returned an `id`, i.e. Boxarr's store already has the series (`10` COMPLETE ENDPOINT SURFACE; Uncertainties). Boxarr supports them when the series exists; they are otherwise never reached.

**`PUT /sonarr/api/v3/series`** — Overseerr/Jellyseerr PUTs the full series object back with `monitored`, merged `tags`, and a rebuilt `seasons[]` (requested seasons → `monitored:true`). Boxarr applies the monitoring deltas (`SetSeasonMonitored` per season, `02` §5.2; series-level `UpdateSeries`), then **returns the series object including a non-zero `id`** (same shape as §2.7 response). Success check is `response.data.id` truthy (verified `10` PUT series).

**`GET /sonarr/api/v3/episode?seriesId={id}`** (Jellyseerr only) — returns the series' episodes so Jellyseerr can re-monitor specific ones. Boxarr maps `ListEpisodes(seriesId)` (`02` §5.2) to the Sonarr `Episode` shape:

```json
[
  {
    "id": 1001,
    "seriesId": 42,
    "seasonNumber": 1,
    "episodeNumber": 1,
    "title": "Pilot",
    "airDate": "2008-01-20",
    "airDateUtc": "2008-01-20T02:00:00Z",
    "monitored": true,
    "hasFile": false
  }
]
```

`id` is the real `episode.id`; `hasFile` mirrors `episode.has_file`; `monitored` mirrors `episode.monitored` (`02` §3.2).

**`PUT /sonarr/api/v3/episode/monitor`** (Jellyseerr only) — body `{ "episodeIds": [1001, 1002], "monitored": true }`. Boxarr flips `episode.monitored` for the listed ids (a targeted update; `SetEpisode...` family per `02` §5.2) and returns **HTTP 200** (Seerr only needs a 2xx; the body is not inspected). Episodes set to `monitored:true` that are aired + file-less become `wanted` on the next reconcile (`02` §2.2; `06`).

---

## 3. Radarr v3 surface

All paths under `/radarr/api/v3/`. **No `languageprofile` for Radarr — ever** (verified `10` TEST-BUTTON RADARR).

### 3.1 `GET /radarr/api/v3/system/status`

Both Seerrs wrap Radarr's `system/status` in `.catch(() => req.body.baseUrl)`, so a failure does not abort the Radarr test (unlike Jellyseerr's Sonarr test) — but Boxarr returns a clean 200 regardless. **Boxarr advertises a Radarr v3 version string, locked to `3.0.0.0`** (Radarr's v3 line; 00 §5.3 — "Radarr emulation advertises a Radarr version"). Radarr has no `languageprofile` so the version's only effect is cosmetic for Seerr, but a v3 string keeps the surface internally consistent with the v3 contract. Same `SystemStatus` shape as §2.1 with `appName`/`instanceName` `"Radarr"`, `version: "3.0.0.0"`, `urlBase: "/radarr"`:

```json
{
  "appName": "Radarr",
  "instanceName": "Radarr",
  "version": "3.0.0.0",
  "buildTime": "2021-10-10T00:00:00Z",
  "isDebug": false,
  "isProduction": true,
  "isAdmin": false,
  "isUserInteractive": false,
  "startupPath": "/app/radarr/bin",
  "appData": "/config",
  "osName": "ubuntu",
  "osVersion": "20.04",
  "isNetCore": true,
  "isMono": false,
  "isLinux": true,
  "isOsx": false,
  "isWindows": false,
  "isDocker": true,
  "mode": "console",
  "branch": "master",
  "authentication": "apikey",
  "sqliteVersion": "3.35.5",
  "migrationVersion": 0,
  "urlBase": "/radarr",
  "runtimeVersion": "6.0.0",
  "runtimeName": "netcore",
  "startTime": "2021-10-10T00:00:00Z",
  "packageUpdateMechanism": "docker"
}
```

### 3.2 `GET /radarr/api/v3/qualityprofile`

Identical shape to §2.2; same seeded profile id `1`:

```json
[
  { "id": 1, "name": "Any" }
]
```

### 3.3 `GET /radarr/api/v3/rootfolder`

Same shape/semantics as §2.3, returning the seeded **movie** root (`02` §3.6 — id `2`, `media_kind='movie'`, default path `/data/movies`); `freeSpace`/`totalSpace`/`accessible` computed at request time:

```json
[
  {
    "id": 2,
    "path": "/data/movies",
    "accessible": true,
    "freeSpace": 107374182400,
    "totalSpace": 214748364800,
    "unmappedFolders": []
  }
]
```

The `path` returned here must equal `POST /movie.rootFolderPath` (round-trip exactness; `02` §3.6 quirk 2).

### 3.4 `GET /radarr/api/v3/tag`

`[]` (same as §2.4).

### 3.5 `GET /radarr/api/v3/movie/lookup?term=tmdb:{tmdbId}`

Issued by `RadarrAPI.getMovieByTmdbId(id)`. The term is **always `tmdb:{id}`** (Radarr keys on TMDB directly — no tvdb→tmdb bridge needed).

**Behavior gates (verified `10` LOOKUP movie):**
1. `response.data[0]` empty ⇒ throws "Movie not found". **Return at least one element** when the movie resolves.
2. `movie.hasFile === true` ⇒ Seerr returns early treating it as already-satisfied (no POST/PUT). Boxarr sets `hasFile` from `movie.has_file` so an already-imported movie short-circuits.
3. `movie.id && !movie.monitored` ⇒ PUT update path; afterward Seerr checks `response.data.monitored === true` for success.
4. `movie.id` (already monitored) ⇒ Seerr skips.
5. Otherwise (`id:0`/absent + `hasFile:false` + `monitored:false`) ⇒ **POST add path**.

**Boxarr builds the lookup from TMDB** (`03` TMDB "build a movie"): `GET /movie/{id}?append_to_response=release_dates` → title/year/imdbId/status. `titleSlug` in the lookup is a slug-style value (`the-matrix-603`) — note this differs from what Seerr *sends back* in the POST body (the numeric TMDB id as a string, §3.6). **Boxarr includes `id` only when the movie is already tracked** (`GetMovieByTMDB` hit), and sets it to the real `movie.id`; `hasFile`/`monitored` mirror the catalog row.

Response for a **not-yet-added** movie (drives the POST path — `id:0`):

```json
[
  {
    "id": 0,
    "title": "The Matrix",
    "isAvailable": false,
    "monitored": false,
    "tmdbId": 603,
    "imdbId": "tt0133093",
    "titleSlug": "the-matrix-603",
    "folderName": "",
    "path": "",
    "profileId": 0,
    "qualityProfileId": 0,
    "added": "0001-01-01T00:00:00Z",
    "hasFile": false,
    "tags": []
  }
]
```

### 3.6 `POST /radarr/api/v3/movie` (add)

**Exact body Overseerr sends** (verbatim `10` POST movie):

```json
{
  "title": "The Matrix",
  "qualityProfileId": 1,
  "profileId": 1,
  "titleSlug": "603",
  "minimumAvailability": "released",
  "tmdbId": 603,
  "year": 1999,
  "rootFolderPath": "/data/movies",
  "monitored": true,
  "tags": [],
  "addOptions": {
    "searchForMovie": true
  }
}
```

Quirks baked in (verified `10`): **Overseerr sends BOTH `qualityProfileId` AND the legacy `profileId`** (same value) — Boxarr reads `qualityProfileId` and **accepts-but-ignores `profileId`**. **`titleSlug` is the TMDB id as a string** (`"603"`), NOT a real slug — Boxarr **accepts-but-ignores** it (it generates its own slug for the response). **`minimumAvailability` is one of `announced`|`inCinemas`|`released`|`preDB`** — Boxarr stores it on `movie.minimum_availability` (`02` §3.2). `addOptions.searchForMovie` is the auto-search trigger (§4).

Ingest behavior owned by §4. **Response (HTTP 201) MUST echo a non-zero numeric `id`** — Overseerr checks `if (response.data.id)` and throws "Failed to add movie to Radarr" on falsy (verified `10` — CRITICAL). `id` is the real `movie.id` from `CreateMovie` (`02` §5.2):

```json
{
  "id": 17,
  "title": "The Matrix",
  "tmdbId": 603,
  "imdbId": "tt0133093",
  "titleSlug": "the-matrix-603",
  "qualityProfileId": 1,
  "profileId": 1,
  "rootFolderPath": "/data/movies",
  "minimumAvailability": "released",
  "monitored": true,
  "hasFile": false,
  "isAvailable": false,
  "folderName": "The Matrix (1999)",
  "path": "/data/movies/The Matrix (1999)",
  "added": "2026-06-20T00:00:00Z",
  "tags": []
}
```

`titleSlug` in the **response** is the real slug (`the-matrix-603`); `path`/`folderName` are `movie.library_path` and its basename (the namer, 00 §6 / `06`); `added` is `movie.added_at`.

### 3.7 Movie update path

**`PUT /radarr/api/v3/movie`** fires when the lookup returned `movie.id && !movie.monitored` (an unmonitored, already-tracked movie). Overseerr PUTs the full movie spread with `monitored`, `minimumAvailability`, merged `tags`, `addOptions.searchForMovie`. Boxarr flips `movie.monitored=true` (and updates `minimum_availability`, quality profile, root folder via `UpdateMovie`, `02` §5.2), honors `searchForMovie` (§4), and **returns the movie with `"monitored": true`** — Seerr's success check is `response.data.monitored === true` (verified `10` PUT movie; Recommendation 7).

---

## 4. Add → ingest mapping (into the catalog, §02)

Both add endpoints converge on a small `ingest` service (placement `01`; store calls `02` §5.2; metadata `03`). The flow is identical in spirit to a manual catalog add (`04`), with Seerr-specific id resolution at the front.

### 4.1 `POST /series` ingest (numbered)

1. **Resolve tvdb→tmdb.** The body carries `tvdbId` (series-level integer). Resolve to TMDB id via `GET /find/{tvdbId}?external_source=tvdb_id` → `tv_results[0].id` (`03` TMDB); if empty, fall back to TVDB `/search/remoteid/{tvdbId}` (`03` TVDB; 00 §9). **If neither resolves, return HTTP 404** (Seerr surfaces "not found") rather than creating a half-blank row.
2. **Idempotency.** `GetSeriesByTMDB(tmdbId)` — if a row exists, **do not double-insert**; treat the POST as the update path (apply monitoring deltas, return the existing `series.id`). This guards against Seerr POSTing when a race or stale cache made it skip the lookup's `id`.
3. **Build catalog rows.** `CreateSeries` with `tmdb_id`, resolved `tvdb_id`, `title`, `year`, `monitored` (from body, default `true`), `season_folder` (from body), `quality_profile_id` (from body, falls back to seeded `1`), `root_folder_path` (from body, must match a seeded `root_folder.path`), `series_type` (from body — `standard`/`daily`/`anime`). Fetch seasons/episodes from TMDB (`03` TMDB call sequence) and `UpsertSeason`/`UpsertEpisode` per season/episode. `library_path` is resolved by the namer.
4. **Apply per-season monitoring** from the body's `seasons[]`: each season's `monitored` flag → `season.monitored`; episodes inherit. This is how partial-season requests work — only the requested season numbers arrive as `monitored:true` (verified `10` — the season round-trip mechanism). Season 0/Specials arrives `monitored:false` by Overseerr's default.
5. **Status promotion.** The catalog promoter recomputes each monitored, aired, file-less episode to `MediaStatus = wanted` (`02` §2.2); unaired episodes stay `missing` (air-date-aware, `02` §5.2 `WantedEpisodes`).
6. **Honor `addOptions.searchForMissingEpisodes`.** When `true`, kick off a search for the wanted episodes (hand the wanted set to the grab pipeline, `06`). When `false`, the episodes sit in `wanted` until a scheduled/manual search picks them up (FR-SR-4). **This is fire-and-forget relative to the HTTP response** — the 201 returns immediately; the search runs async (matching real Sonarr, where the add returns before the search completes).
7. **Respond 201** with the series object echoing the assigned `id` (§2.7).

### 4.2 `POST /movie` ingest (numbered)

1. **TMDB id is direct** (`tmdbId` in body) — no bridge. **Idempotency:** `GetMovieByTMDB(tmdbId)` — existing row ⇒ update path, return existing `movie.id`.
2. **Build catalog row.** `CreateMovie` with `tmdb_id`, `title`, `year`, `monitored` (default `true`), `minimum_availability` (from body), `quality_profile_id` (body → seeded `1`), `root_folder_path` (body, must match seeded movie root). Fetch full TMDB details + `release_dates` (`03` TMDB) to populate `imdb_id`, `release_date`/`digital_release`/`physical_release`, `runtime`, `tmdb_status`, posters. `library_path` via the namer.
3. **Status promotion.** Movie is `wanted` iff monitored + released-per-`minimumAvailability` (the `released`/`inCinemas`/`announced`/`preDB` gate maps onto the TMDB release-date types, `03` TMDB build-a-movie; `02` `WantedMovies` uses `release_date <= date('now')`). A `minimumAvailability` not yet met keeps the movie `missing`.
4. **Honor `addOptions.searchForMovie`.** `true` ⇒ kick off a movie search async (`06`); `false` ⇒ wait for manual/scheduled search.
5. **Respond 201** echoing `id` (§3.6). On the PUT update path (§3.7), respond with `monitored:true`.

### 4.3 `POST /api/v3/command` (both surfaces — fire-and-forget no-op)

After add/update, Seerr POSTs a search command and **catches/logs any error without propagating** (verified `10` SEARCH COMMANDS): Sonarr/Overseerr `{ "name": "SeriesSearch", "seriesId": 42 }`; Sonarr/Jellyseerr `{ "name": "MissingEpisodeSearch", "seriesId": 42 }`; Radarr `{ "name": "MoviesSearch", "movieIds": [17] }`. Boxarr's actual search is already triggered by `addOptions` (§4.1/§4.2), so `command` is a **no-op that returns HTTP 201** with a stub body to avoid log noise (`10` Recommendation 8):

```json
{ "id": 1, "name": "SeriesSearch", "status": "queued" }
```

Boxarr **may** opportunistically (re)trigger the search from `command` too (idempotent — dedup at the grab layer prevents double-submit, `02`/`03`), but it is not required; the contract is satisfied by any 2xx.

### 4.4 `DELETE /series/{id}` and `DELETE /movie/{id}` (Jellyseerr admin only — not in normal flow)

These exist in Jellyseerr's client but are invoked only from specific admin routes, **not** the Test or add-media flows (verified `10` Uncertainties). Boxarr maps them to its own delete pipeline (`DeleteSeries`/`DeleteMovie`, `02` §5.2, which cascades + propagates to TorBox, FR-DEL-1) and returns **HTTP 200**. They are low-priority (a v2 nicety); the add flow does not depend on them.

---

## 5. Availability back-sync is NOT in scope (FR-SEERR-7)

**Boxarr never pushes availability state back to Seerr.** Seerr derives "available" from Plex, which scans Boxarr's library symlinks (`03` Plex; `06` import → Plex scan). The Servarr emulation here is **add + config only** — there is no `/api/v3/wanted`, `/api/v3/queue`, `/api/v3/history`, or webhook-out from Boxarr to Seerr (00 §8; requirements §10 FR-SEERR-7). The only endpoints Boxarr serves are those in §2/§3/§4; anything else returns 404.

---

## 6. Complete endpoint surface (what Boxarr serves)

| Surface | Method | Path | Response | Seerr reads |
|---|---|---|---|---|
| Sonarr | GET | `/sonarr/api/v3/system/status` | `SystemStatus` (§2.1) | `urlBase` (both), `version` (Jellyseerr) |
| Sonarr | GET | `/sonarr/api/v3/qualityprofile` | `[{id,name}]` (§2.2) | `id`,`name` |
| Sonarr | GET | `/sonarr/api/v3/rootfolder` | `[{id,path,accessible,freeSpace,totalSpace,unmappedFolders}]` (§2.3) | `id`,`path` |
| Sonarr | GET | `/sonarr/api/v3/tag` | `[]` (§2.4) | `id`,`label` |
| Sonarr | GET | `/sonarr/api/v3/languageprofile` | `[{id,name}]` (§2.5) — **always 200** | `id`,`name` |
| Sonarr | GET | `/sonarr/api/v3/series/lookup?term=tvdb:{id}` | `SonarrSeries[]` (§2.6) | `data[0]`, `data[0].id`, `seasons[]` |
| Sonarr | POST | `/sonarr/api/v3/series` | `SonarrSeries` w/ `id>0` (§2.7) — **201** | `data.id` |
| Sonarr | PUT | `/sonarr/api/v3/series` | `SonarrSeries` w/ `id>0` (§2.8) | `data.id` |
| Sonarr | GET | `/sonarr/api/v3/episode?seriesId={id}` | `Episode[]` (§2.8, Jellyseerr) | episode ids/monitored |
| Sonarr | PUT | `/sonarr/api/v3/episode/monitor` | **200** (§2.8, Jellyseerr) | — |
| Sonarr | POST | `/sonarr/api/v3/command` | **201** stub (§4.3) | — |
| Sonarr | DELETE | `/sonarr/api/v3/series/{id}` | **200** (§4.4, admin) | — |
| Radarr | GET | `/radarr/api/v3/system/status` | `SystemStatus` (§3.1) | `urlBase` |
| Radarr | GET | `/radarr/api/v3/qualityprofile` | `[{id,name}]` (§3.2) | `id`,`name` |
| Radarr | GET | `/radarr/api/v3/rootfolder` | `[{...}]` (§3.3) | `id`,`path` |
| Radarr | GET | `/radarr/api/v3/tag` | `[]` (§3.4) | `id`,`label` |
| Radarr | GET | `/radarr/api/v3/movie/lookup?term=tmdb:{id}` | `RadarrMovie[]` (§3.5) | `data[0]`, `id`, `hasFile`, `monitored` |
| Radarr | POST | `/radarr/api/v3/movie` | `RadarrMovie` w/ `id>0` (§3.6) — **201** | `data.id` |
| Radarr | PUT | `/radarr/api/v3/movie` | `RadarrMovie` w/ `monitored:true` (§3.7) | `data.monitored` |
| Radarr | POST | `/radarr/api/v3/command` | **201** stub (§4.3) | — |
| Radarr | DELETE | `/radarr/api/v3/movie/{id}` | **200** (§4.4, admin) | — |

Every endpoint requires the api key via `X-Api-Key` header or `?apikey=` query (§1.2). Anything not listed ⇒ 404 (§5).

---

## 7. Quirks to bake in

1. **Version-string rule (locked, 00 §5.3 + 00 §9 Seerr register).** Sonarr surface advertises **`3.0.10.1567`** (a `3.x.x.x`); Radarr surface advertises **`3.0.0.0`**. A `4.x` Sonarr string makes Jellyseerr skip `/languageprofile` but **breaks Overseerr** (which always calls it and would 500 on 404). 3.x satisfies both because Boxarr serves `/languageprofile` unconditionally.
2. **Jellyseerr reads `system/status` synchronously** on the Sonarr test (no `.catch`), so a non-200 there fails the entire Sonarr test in Jellyseerr (Overseerr tolerates it). **Always return a clean 200** (00 §9 Seerr register).
3. **Return 201 echoing a non-zero numeric `id`** from `POST /series` and `POST /movie`. Seerr checks `response.data.id` (truthy), **not** the HTTP status — but Axios throws on 4xx/5xx, so **never** return ≥400 on success; **201 (or 200) both work** (00 §9 Seerr register). Boxarr returns **201**.
4. **`PUT /movie` must return `monitored: true`** — Seerr's update success check is `response.data.monitored === true`, not the id. **`PUT /series` must return a truthy `id`.**
5. **Minimal `{id,name}` quality/language profiles and `{id,path,accessible,freeSpace}` root folders are sufficient** — extra fields are always ignored (00 §9 Seerr register). Boxarr returns the fuller realistic shape anyway.
6. **Both auth methods accepted** — `X-Api-Key` header *and* `apikey` query param, constant-time compared via the reused `subtle.ConstantTimeCompare` helper (`handlers.go:79`). Seerr in practice only ever sends `?apikey=`.
7. **Parse JSON request bodies** (`Content-Type: application/json`, confirmed sent by Seerr) and **ignore unknown fields** so Overseerr-vs-Jellyseerr body differences (`monitorNewItems`, legacy `profileId`, `titleSlug`) never 400.
8. **Lookup `id` presence is the add-vs-update switch.** Omit `id` (and `id:0` for Radarr) ⇒ POST add path; include real `id` ⇒ PUT update path. **Never return an empty `[]` when the title resolves in TMDB** — empty ⇒ Seerr throws "not found".
9. **Radarr lookup `hasFile:true` short-circuits Seerr** (already satisfied). Set it from `movie.has_file` so re-requests of owned movies do nothing.
10. **The `path` in `/rootfolder` must equal the `rootFolderPath` Boxarr accepts back** in the POST body — `root_folder.path` is `UNIQUE` in the DB so the round-trip is exact (`02` §3.6 quirk 2).
11. **`POST /command` is a fire-and-forget no-op returning 201** (search is already triggered by `addOptions`). **`searchForMissingEpisodes`/`searchForMovie` drive the actual search, async** — the add response returns before the search completes.
12. **Availability back-sync is NOT served** (FR-SEERR-7) — add + config endpoints only; Seerr derives availability from Plex.
13. **Case-insensitive path matching** — register lowercase canonical paths and lower-case the request path so `/qualityProfile` and `/qualityprofile` both resolve.

---

## Definition of done

The Seerr emulation is done when both Overseerr and Jellyseerr add a Sonarr server at `/sonarr` and a Radarr server at `/radarr` (api key in their "API Key" field), the "Test" button turns green on **both** Seerrs for **both** surfaces, and requesting a series or a movie from Seerr creates a monitored catalog row in Boxarr that begins searching when the request asked for it: Boxarr serves all endpoints in §6 under `/sonarr/api/v3` and `/radarr/api/v3` from a single `kind`-parameterized router factory, accepting the key via `X-Api-Key` header **or** `?apikey=` query in constant time (reusing `subtle.ConstantTimeCompare`, `handlers.go:79`) and 401-ing otherwise; `system/status` advertises **Sonarr `3.0.10.1567`** / **Radarr `3.0.0.0`** with a clean 200 (so Jellyseerr's synchronous Sonarr status read passes and Overseerr's unconditional `/languageprofile` call succeeds); `qualityprofile`/`rootfolder`/`tag`/`languageprofile` return the seeded `02` §3.6 rows (root folders with `accessible`+`freeSpace` computed at request time and a `path` that round-trips exactly into the POST body); the lookup endpoints build canonical objects from TMDB (tvdb→tmdb resolved via `/find` with a TVDB `/search/remoteid` fallback for Sonarr) with a complete `seasons[]` and an `id` present **only** when the item is already tracked; `POST /series` and `POST /movie` ingest into the `02` catalog (idempotent on `tmdb_id`, per-season monitoring from the body, status promoted to `wanted`/`missing` air-date-aware, `addOptions.searchForMissingEpisodes`/`searchForMovie` kicking off an async `06` search) and respond **201** echoing the real assigned numeric `id`; `PUT /movie` returns `monitored:true` and `PUT /series` a truthy `id`; the episode update-path endpoints work when the series already exists; `POST /command` is a 201 no-op; no availability back-sync is served (FR-SEERR-7); and `gofmt -s`/`golangci-lint` pass with every endpoint documented and every uncertain Seerr behavior resolved to its 00 §9-registered default. Cross-refs: `00-decisions-and-assumptions.md` (§5.3 version string, §6 namespaces, §9 Seerr register), `02-data-model.md` (catalog ingest target + seeded servarr tables), `03-external-contracts.md` (TMDB/TVDB lookup construction), `04-internal-api.md` (the distinct `/api/v1` SPA surface), `06-pipelines.md` (the async search the add flow triggers).
