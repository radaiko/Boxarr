# Boxarr — Config, Deploy & CI (Spec 08)

**Date:** 2026-06-20
**Status:** Approved for planning

This spec owns the **`BOXARR_*` environment reference**, the **multi-stage Dockerfile**, the **`docker-compose`** deployment, and the **CI / release GitHub Actions** plus `.golangci.yml`. It is grounded in the verbatim sab2torbox config struct, Dockerfile, compose, and workflows (`/tmp/boxarr-grounding/04-sab-api-config-ci.md` §1, §4–§9) and evolves them per the decisions locked in `00-decisions-and-assumptions.md`. It implements requirements §17.1 (CI/CD & Release), §17.2 (frontend supply-chain), NFR-5 (config), NFR-6 (deployment), and the FR-CI-*/FR-SEC-* family.

The **mechanism** is reused verbatim from sab2torbox: `envconfig.Process` against a struct with `envconfig:`/`default:`/`required:` tags, a `Load()` that runs `os.Stat` fail-fast validation of filesystem roots, comma-separated slice fields, and `*Enabled()` predicates for optional integrations (verified `/tmp/sab2torbox/internal/config/config.go:14-130`). What changes is the **prefix** (`sab2torbox` → `boxarr`, `00` §2/§6), the **dropped vars** (`SAB_API_KEY`, `SYMLINK_ROOT`), and a large **additive surface** (Prowlarr, TMDB, TVDB, Plex, Seerr, library roots, intervals, selection score, limits).

---

## 1. Configuration mechanism (locked)

- **Load path:** `config.Load()` calls **`envconfig.Process("boxarr", &c)`** (was `"sab2torbox"`, verified `04` §1) so every variable is prefixed **`BOXARR_`**. Renaming the literal prefix string is the *only* change to the parse call (`00` §6; `01` step 4).
- **Struct tags:** mirror the existing style exactly — `envconfig:"<NAME>"`, `default:"<value>"`, `required:"true"` (verified `04` §1). Durations use Go `time.Duration` strings (`1m`, `15m`, `24h`); booleans `true`/`false`; **slice fields are comma-separated** (proven by `Categories`, verified `04` Key Facts).
- **Fail-fast validation in `Load()`** (carried from sab2torbox `validateMount()` + `os.Stat` pattern, verified `04` §1): after `envconfig.Process`, `Load()` validates that every required filesystem root exists **and is a directory** via `os.Stat`, and enforces "feature X requires Y" combos. It **fails fast** (returns an error from `run()` → exit 1) rather than starting in a half-configured state. Specifically:
  1. `BOXARR_WEBDAV_MOUNT_ROOT` must exist + be a dir (reused `validateMount()`).
  2. `BOXARR_MOVIE_LIBRARY_ROOT` and `BOXARR_TV_LIBRARY_ROOT` must each exist + be a dir (replaces the old `SYMLINK_ROOT` `os.Stat`; the importer writes library symlinks **directly at their final Plex path** under these roots — there is **no `_incoming` symlink farm**, `00` §5.1).
  3. If `BOXARR_HEAL_ENABLED`, then `BOXARR_HEAL_LIBRARY_ROOTS` must be non-empty and each entry must exist + be a dir, and `BOXARR_HEAL_MAX_ATTEMPTS > 0` (reused verbatim, verified `04` §1).
  4. **Same-filesystem guard (new, `00` §5.1 / Assumption D):** `Load()` asserts that each library root and the WebDAV mount root resolve to the **same device** (`syscall.Stat_t.Dev` via `os.Stat`), logging a warning (not a hard error) if they differ, because a cross-device library symlink would dangle in Plex. This makes the historically-implicit deployment contract explicit.
- **Helper methods** (carried + extended): `SlogLevel()` maps the `LOG_LEVEL` string → `slog.Level` (debug/warn/error/else info, verified `04` §1); `WebDAVRefreshEnabled()` = both TorBox WebDAV creds set (verified `04` §1, reused unchanged). **New `*Enabled()` predicates** gate optional integrations, mirroring `WebDAVRefreshEnabled()`:
  - `TorrentsEnabled()` = `TorBoxAPIToken != ""` (always true in practice; gates torrent loops — `01` §wiring rule).
  - `PlexEnabled()` = `PlexURL != "" && PlexToken != ""` (verified `03` Plex "Optional integration — `Enabled()` predicate gates it").
  - `TVDBEnabled()` = `TVDBAPIKey != ""` (TVDB supplements TMDB for ordering, `00` §19.3; absent → Boxarr falls back to TMDB-only episode ordering).
  - `SeerrEnabled()` = `len(SeerrAPIKeys) > 0` (gates the `/sonarr` + `/radarr` emulation surfaces, `05`).
  - `PathPrefixMapEnabled()` = `HostToPlexPathPrefix != ""` (escape hatch for divergent mounts, `03` Plex).
- **Helper paths** (carried + extended): `UsenetPath()` = `filepath.Join(WebDAVMountRoot, WebDAVUsenetSubpath)` (reused verbatim, verified `04` §1); **new** `TorrentPath()` = `filepath.Join(WebDAVMountRoot, WebDAVTorrentSubpath)` (`01` torrent-poller; `00` §19.5 — subpath default empty so it collapses to the same flat mount root as usenet until the live mount is verified, `00` §9 register).
- **Secrets never logged (NFR-4):** the startup log line records `version`, `listen_addr`, `usenet_path`, `torrent_path`, `poll_interval`, and which optional integrations are enabled — **never** any token/key/password (matches the sab2torbox startup-log discipline, verified `04` §3). The runtime-mutable `settings` table (`02` §3.5) may hold UI-set secrets but is likewise never logged.

**Runtime-mutable overlay (locked, `02` §3.5):** env vars are the **source of defaults**; the `settings` KV table holds operator overrides written from the Settings UI (FR-CFG-1 / NFR-5). Resolution order is **DB setting → env var → struct `default:`**. This spec owns the env defaults; `04-internal-api.md`/`07-frontend.md` own the settings-UI surface.

---

## 2. `BOXARR_*` environment reference

The full struct. **KEPT (renamed prefix only)** is carried verbatim from sab2torbox; **NEW** is additive; **DROPPED** is removed with the SAB surface (`00` Assumption B).

### 2.1 Go config struct (target)

```go
type Config struct {
	// ── Core (KEPT, renamed prefix only) ────────────────────────────────
	TorBoxAPIToken      string        `envconfig:"TORBOX_API_TOKEN" required:"true"`
	WebDAVMountRoot     string        `envconfig:"WEBDAV_MOUNT_ROOT" required:"true"`
	WebDAVUsenetSubpath string        `envconfig:"WEBDAV_USENET_SUBPATH"`
	ListenAddr          string        `envconfig:"LISTEN_ADDR" default:":8080"`
	DatabasePath        string        `envconfig:"DATABASE_PATH" default:"/config/boxarr.db"`
	PollInterval        time.Duration `envconfig:"POLL_INTERVAL" default:"1m"`
	LogLevel            string        `envconfig:"LOG_LEVEL" default:"info"`
	APIKey              string        `envconfig:"API_KEY"` // SPA /api/v1 auth (X-Api-Key); empty + loopback request = allowed (04 §1)

	// ── TorBox WebDAV force-refresh (KEPT) ──────────────────────────────
	TorBoxWebDAVUser       string        `envconfig:"TORBOX_WEBDAV_USER"`
	TorBoxWebDAVPass       string        `envconfig:"TORBOX_WEBDAV_PASS"`
	TorBoxWebDAVRefreshURL string        `envconfig:"TORBOX_WEBDAV_REFRESH_URL" default:"https://webdav.torbox.app/refresh"`
	WebDAVRefreshCooldown  time.Duration `envconfig:"TORBOX_WEBDAV_REFRESH_COOLDOWN" default:"2m"`

	// ── Torrent WebDAV path (NEW — 00 §19.5) ────────────────────────────
	WebDAVTorrentSubpath string `envconfig:"WEBDAV_TORRENT_SUBPATH"` // default empty → same flat root as usenet

	// ── Indexers / metadata / playback (NEW) ────────────────────────────
	ProwlarrURL    string `envconfig:"PROWLARR_URL" required:"true"`
	ProwlarrAPIKey string `envconfig:"PROWLARR_API_KEY" required:"true"`
	TMDBAPIKey     string `envconfig:"TMDB_API_KEY" required:"true"`
	TVDBAPIKey     string `envconfig:"TVDB_API_KEY"`          // optional; supplements TMDB ordering (00 §19.3)
	TVDBPin        string `envconfig:"TVDB_PIN"`              // only for user-supported keys (03 TVDB)

	PlexURL          string `envconfig:"PLEX_URL"`
	PlexToken        string `envconfig:"PLEX_TOKEN"`
	PlexMovieSection string `envconfig:"PLEX_MOVIE_SECTION"`  // optional override; else auto-detect (03 Plex)
	PlexTVSection    string `envconfig:"PLEX_TV_SECTION"`     // optional override; else auto-detect

	// ── Seerr emulation inbound keys (NEW — 05) ─────────────────────────
	SeerrAPIKeys []string `envconfig:"SEERR_API_KEYS"`       // comma-separated; gates /sonarr + /radarr

	// ── Library roots (NEW — replace SYMLINK_ROOT, 00 §5.1) ─────────────
	MovieLibraryRoot string `envconfig:"MOVIE_LIBRARY_ROOT" default:"/data/movies"`
	TVLibraryRoot    string `envconfig:"TV_LIBRARY_ROOT"    default:"/data/tv"`

	// ── Same-path escape hatch (NEW — 03 Plex) ──────────────────────────
	HostToPlexPathPrefix string `envconfig:"HOST_TO_PLEX_PATH_PREFIX"` // empty = identical mounts (Assumption D)

	// ── Intervals (NEW — 01 worker topology) ────────────────────────────
	ReconcileInterval time.Duration `envconfig:"RECONCILE_INTERVAL" default:"15m"`
	MetadataInterval  time.Duration `envconfig:"METADATA_REFRESH_INTERVAL" default:"24h"`
	SearchInterval    time.Duration `envconfig:"SEARCH_INTERVAL" default:"6h"`

	// ── Selection score knobs (NEW — FR-SR-5; the authoritative algorithm + this exact knob set live in 06 §3) ──
	// Reject conditions:
	SelectAllowedResolutions []string `envconfig:"SELECT_ALLOWED_RESOLUTIONS"`            // empty = all
	SelectMinSize            int64    `envconfig:"SELECT_MIN_SIZE" default:"0"`           // bytes; global min
	SelectMaxSize            int64    `envconfig:"SELECT_MAX_SIZE" default:"0"`           // bytes; 0 = ∞
	SelectSizeLimits         string   `envconfig:"SELECT_SIZE_LIMITS" default:"{}"`       // JSON per-quality {min,max}
	SelectMinSeeders         int      `envconfig:"SELECT_MIN_SEEDERS" default:"1"`        // torrent reject threshold
	SelectMinGrabs           int      `envconfig:"SELECT_MIN_GRABS" default:"0"`          // usenet reject threshold
	SelectRequireCached      bool     `envconfig:"SELECT_REQUIRE_CACHED" default:"false"` // reject uncached torrents
	SelectBlockedGroups      []string `envconfig:"SELECT_BLOCKED_GROUPS"`
	SelectBlockedKeywords    []string `envconfig:"SELECT_BLOCKED_KEYWORDS"`
	SelectMinScore           int      `envconfig:"SELECT_MIN_SCORE" default:"0"`          // reject if final score < this
	// Scoring preferences (ordered best→worst):
	SelectPreferredResolutions []string `envconfig:"SELECT_PREFERRED_RESOLUTIONS" default:"2160p,1080p,720p"`
	SelectPreferredQualities   []string `envconfig:"SELECT_PREFERRED_QUALITIES" default:"WEB-DL,BluRay,WEBRip,HDTV"`
	SelectPreferredGroups      []string `envconfig:"SELECT_PREFERRED_GROUPS"`
	SelectPreferredKeywords    []string `envconfig:"SELECT_PREFERRED_KEYWORDS"`
	// Scoring weights (defaults make cached-torrent > usenet > uncached-torrent):
	SelectWeightResolution              int `envconfig:"SELECT_WEIGHT_RESOLUTION" default:"400"`
	SelectWeightQuality                 int `envconfig:"SELECT_WEIGHT_QUALITY" default:"200"`
	SelectWeightProtocolCachedTorrent   int `envconfig:"SELECT_WEIGHT_PROTOCOL_CACHED_TORRENT" default:"300"`
	SelectWeightProtocolUsenet          int `envconfig:"SELECT_WEIGHT_PROTOCOL_USENET" default:"200"`
	SelectWeightProtocolUncachedTorrent int `envconfig:"SELECT_WEIGHT_PROTOCOL_UNCACHED_TORRENT" default:"100"`
	SelectWeightHealth                  int `envconfig:"SELECT_WEIGHT_HEALTH" default:"100"`
	SelectSeedSaturation                int `envconfig:"SELECT_SEED_SATURATION" default:"100"`
	SelectWeightPreferredGroup          int `envconfig:"SELECT_WEIGHT_PREFERRED_GROUP" default:"150"`
	SelectWeightPreferredKeyword        int `envconfig:"SELECT_WEIGHT_PREFERRED_KEYWORD" default:"50"`
	SelectWeightFreeleech               int `envconfig:"SELECT_WEIGHT_FREELEECH" default:"40"`
	SelectWeightProper                  int `envconfig:"SELECT_WEIGHT_PROPER" default:"25"`

	// ── Limit knobs (NEW — FR-LIM-2/3, 06/13) ───────────────────────────
	MaxActiveDownloads int `envconfig:"MAX_ACTIVE_DOWNLOADS" default:"0"`   // FR-LIM-2; 0 = derive from TorBox plan (06 §7.1); >0 = hard cap (min with plan-derived)
	MaxCreatePerHour   int `envconfig:"MAX_CREATE_PER_HOUR" default:"60"`   // FR-LIM-3 60/hour create* cap
	MaxTorrentPerMin   int `envconfig:"MAX_TORRENT_PER_MIN" default:"300"`  // FR-LIM-3 300/min torrent ceiling
	SearchConcurrency  int `envconfig:"SEARCH_CONCURRENCY" default:"3"`     // parallel Prowlarr searches

	// ── Auto-heal (KEPT) ────────────────────────────────────────────────
	HealEnabled        bool          `envconfig:"HEAL_ENABLED" default:"false"`
	HealInterval       time.Duration `envconfig:"HEAL_INTERVAL" default:"1h"`
	HealLibraryRoots   []string      `envconfig:"HEAL_LIBRARY_ROOTS"`
	HealDryRun         bool          `envconfig:"HEAL_DRY_RUN" default:"false"`
	HealMaxAttempts    int           `envconfig:"HEAL_MAX_ATTEMPTS" default:"3"`
	HealBackoffInitial time.Duration `envconfig:"HEAL_BACKOFF_INITIAL" default:"5m"`
	HealProwlarrFallback bool        `envconfig:"HEAL_PROWLARR_FALLBACK" default:"true"` // NEW: Prowlarr re-search fallback (00 §19.4)
	HealWebhookURL     string        `envconfig:"HEAL_WEBHOOK_URL"`
	HealWebhookEvents  []string      `envconfig:"HEAL_WEBHOOK_EVENTS" default:"failed"`
}
```

**DROPPED with the SAB surface (`00` Assumption B):** `SABAPIKey string \`envconfig:"SAB_API_KEY" required:"true"\``, `SymlinkRoot string \`envconfig:"SYMLINK_ROOT" required:"true"\``, and `Categories []string \`envconfig:"CATEGORIES"\`` (SAB download-client categories have no meaning once SAB emulation is gone; the catalog drives media routing, `01`). `SymlinkRoot` is functionally **replaced** by `MovieLibraryRoot` + `TVLibraryRoot` because Boxarr writes the library symlink at its final Plex path (`00` §5.1).

### 2.2 Full env var table

| Env var | Required | Default | Description |
|---|---|---|---|
| **Core (KEPT)** ||||
| `BOXARR_TORBOX_API_TOKEN` | **yes** | — | TorBox Bearer token; all `/usenet`,`/torrents`,`/user/me` calls. |
| `BOXARR_WEBDAV_MOUNT_ROOT` | **yes** | — | Absolute path of the rclone TorBox WebDAV mount; validated `os.Stat` is-dir at startup. Must be the **same absolute path in every container** (Plex incl.) or symlinks dangle (Assumption D). |
| `BOXARR_WEBDAV_USENET_SUBPATH` | no | (empty) | Subdir under the mount where usenet release folders surface; empty = flat root. `UsenetPath()=Join(root,subpath)`. |
| `BOXARR_LISTEN_ADDR` | no | `:8080` | HTTP bind address. Healthcheck reads this (`§3`). |
| `BOXARR_DATABASE_PATH` | no | `/config/boxarr.db` | SQLite file path (WAL; single writer). |
| `BOXARR_POLL_INTERVAL` | no | `1m` | TorBox `mylist` poll cadence for submit/poll loops (usenet **and** torrent). |
| `BOXARR_LOG_LEVEL` | no | `info` | `debug`/`info`/`warn`/`error`; `SlogLevel()` maps unknown→info. |
| `BOXARR_API_KEY` | no | (empty) | Auth key for Boxarr's own `/api/v1` SPA surface (`X-Api-Key` header). **Empty + loopback request ⇒ allowed** (single-operator local-first); once set, every client incl. loopback must present it. Never applied to `/sonarr`,`/radarr` (those use `BOXARR_SEERR_API_KEYS`). See `04-internal-api.md` §1. |
| **TorBox WebDAV refresh (KEPT)** ||||
| `BOXARR_TORBOX_WEBDAV_USER` | no | — | WebDAV user; both creds set → `WebDAVRefreshEnabled()` forces a `/refresh` post-completion. |
| `BOXARR_TORBOX_WEBDAV_PASS` | no | — | WebDAV password (paired with user). |
| `BOXARR_TORBOX_WEBDAV_REFRESH_URL` | no | `https://webdav.torbox.app/refresh` | Endpoint hit to force a WebDAV listing refresh. |
| `BOXARR_TORBOX_WEBDAV_REFRESH_COOLDOWN` | no | `2m` | Min interval between forced refreshes. |
| **Torrent WebDAV path (NEW)** ||||
| `BOXARR_WEBDAV_TORRENT_SUBPATH` | no | (empty) | Subdir where **torrent** release folders surface; empty = same flat root as usenet. `TorrentPath()=Join(root,subpath)`. **Runtime-verify** (`00` §19.5 / §9). |
| **Indexers / metadata / playback (NEW)** ||||
| `BOXARR_PROWLARR_URL` | **yes** | — | Prowlarr base URL (`http(s)://host:9696`). |
| `BOXARR_PROWLARR_API_KEY` | **yes** | — | Prowlarr `X-Api-Key` (sent as header, never query — `03` Prowlarr). |
| `BOXARR_TMDB_API_KEY` | **yes** | — | TMDB API Read Access Token; `Authorization: Bearer` (`03` TMDB). |
| `BOXARR_TVDB_API_KEY` | no | — | TVDB v4 key; enables scene/absolute ordering. `TVDBEnabled()` gate. Absent → TMDB-only ordering. |
| `BOXARR_TVDB_PIN` | no | — | TVDB subscriber PIN; only for user-supported keys (project keys need none — `03` TVDB). |
| `BOXARR_PLEX_URL` | no | — | Plex Media Server base URL (`http(s)://host:32400`). With token → `PlexEnabled()`. |
| `BOXARR_PLEX_TOKEN` | no | — | `X-Plex-Token` (header). |
| `BOXARR_PLEX_MOVIE_SECTION` | no | (auto) | Override movie section id; else auto-detected from `/library/sections` `type=movie` (`03` Plex). |
| `BOXARR_PLEX_TV_SECTION` | no | (auto) | Override TV section id; else auto-detected `type=show`. |
| **Seerr emulation (NEW)** ||||
| `BOXARR_SEERR_API_KEYS` | no | — | Comma-separated API keys accepted by the `/sonarr` + `/radarr` v3 surfaces (header `X-Api-Key` or `?apikey=`). Non-empty → `SeerrEnabled()` (`05`). |
| **Library roots (NEW — replace `SYMLINK_ROOT`)** ||||
| `BOXARR_MOVIE_LIBRARY_ROOT` | no | `/data/movies` | Movie library root; validated is-dir; seeds `root_folder` id 2 (`02` §3.6). Plex-standard `Title (Year)/…` layout (`00` §6). |
| `BOXARR_TV_LIBRARY_ROOT` | no | `/data/tv` | TV library root; validated is-dir; seeds `root_folder` id 1. `Series (Year)/Season NN/…` layout. |
| **Path mapping escape hatch (NEW)** ||||
| `BOXARR_HOST_TO_PLEX_PATH_PREFIX` | no | (empty) | `host=plex` prefix rewrite for partial-scan paths if Boxarr's and Plex's mounts ever diverge; empty = identical (Assumption D). `PathPrefixMapEnabled()` gate (`03` Plex). |
| **Intervals (NEW)** ||||
| `BOXARR_RECONCILE_INTERVAL` | no | `15m` | 15-min reconciliation sweep cadence (FR-NC-1; matches TorBox WebDAV refresh cadence). |
| `BOXARR_METADATA_REFRESH_INTERVAL` | no | `24h` | TMDB/TVDB catalog refresh to pick up new seasons/episodes (FR-CAT-5). |
| `BOXARR_SEARCH_INTERVAL` | no | `6h` | Auto-search sweep for monitored, still-missing items (FR-SR-4; opt-in via monitored flags). |
| **Selection score (NEW — FR-SR-5; algorithm + per-knob semantics owned by `06-pipelines.md` §3)** ||||
| `BOXARR_SELECT_ALLOWED_RESOLUTIONS` | no | (empty=all) | csv hard allow-list; reject anything else. |
| `BOXARR_SELECT_PREFERRED_RESOLUTIONS` | no | `2160p,1080p,720p` | csv ordered best→worst (weighted). |
| `BOXARR_SELECT_PREFERRED_QUALITIES` | no | `WEB-DL,BluRay,WEBRip,HDTV` | csv ordered best→worst. |
| `BOXARR_SELECT_MIN_SIZE` / `_MAX_SIZE` | no | `0` / `0`(=∞) | global size band in **bytes**. |
| `BOXARR_SELECT_SIZE_LIMITS` | no | `{}` | JSON per-quality `{"2160p":{"min":..,"max":..}}`. |
| `BOXARR_SELECT_MIN_SEEDERS` | no | `1` | torrent reject threshold. |
| `BOXARR_SELECT_MIN_GRABS` | no | `0` | usenet reject threshold. |
| `BOXARR_SELECT_REQUIRE_CACHED` | no | `false` | reject uncached torrents. |
| `BOXARR_SELECT_PREFERRED_GROUPS` / `_BLOCKED_GROUPS` | no | (empty) | csv release-group bonus / reject lists. |
| `BOXARR_SELECT_PREFERRED_KEYWORDS` / `_BLOCKED_KEYWORDS` | no | (empty) | csv title-substring bonus / reject lists. |
| `BOXARR_SELECT_WEIGHT_RESOLUTION` | no | `400` | weight: resolution preference. |
| `BOXARR_SELECT_WEIGHT_QUALITY` | no | `200` | weight: quality/source preference. |
| `BOXARR_SELECT_WEIGHT_PROTOCOL_CACHED_TORRENT` | no | `300` | weight: cached torrent. |
| `BOXARR_SELECT_WEIGHT_PROTOCOL_USENET` | no | `200` | weight: usenet. |
| `BOXARR_SELECT_WEIGHT_PROTOCOL_UNCACHED_TORRENT` | no | `100` | weight: uncached torrent. |
| `BOXARR_SELECT_WEIGHT_HEALTH` | no | `100` | weight: seeders(torrent)/grabs(usenet), saturating. |
| `BOXARR_SELECT_SEED_SATURATION` | no | `100` | health saturation cap. |
| `BOXARR_SELECT_WEIGHT_PREFERRED_GROUP` | no | `150` | bonus: preferred group. |
| `BOXARR_SELECT_WEIGHT_PREFERRED_KEYWORD` | no | `50` | bonus: preferred keyword. |
| `BOXARR_SELECT_WEIGHT_FREELEECH` | no | `40` | bonus: torrent freeleech flag. |
| `BOXARR_SELECT_WEIGHT_PROPER` | no | `25` | bonus: Proper/Repack. |
| `BOXARR_SELECT_MIN_SCORE` | no | `0` | reject if final score `< MIN_SCORE`. |
| **Limits (NEW — FR-LIM-2/3)** ||||
| `BOXARR_MAX_ACTIVE_DOWNLOADS` | no | `0` | Concurrency gate keeping active TorBox slots within the plan allowance; rest queue (FR-LIM-2). **`0` = derive from the TorBox plan slot map (`06` §7.1); `>0` = hard cap applied as `min(plan-derived, this)`.** |
| `BOXARR_MAX_CREATE_PER_HOUR` | no | `60` | `create*` hourly cap (both usenet + torrent) — back off + queue near it (FR-LIM-3). |
| `BOXARR_MAX_TORRENT_PER_MIN` | no | `300` | Torrent 300/min shared ceiling (FR-LIM-3). |
| `BOXARR_SEARCH_CONCURRENCY` | no | `3` | Parallel Prowlarr searches during multi-item sweeps. |
| **Auto-heal (KEPT)** ||||
| `BOXARR_HEAL_ENABLED` | no | `false` | Enable the broken-symlink healer; when true, `HEAL_LIBRARY_ROOTS` required. |
| `BOXARR_HEAL_INTERVAL` | no | `1h` | Heal sweep cadence. |
| `BOXARR_HEAL_LIBRARY_ROOTS` | required **iff** `HEAL_ENABLED` | — | Comma-separated library roots scanned for broken symlinks; each validated is-dir. |
| `BOXARR_HEAL_DRY_RUN` | no | `false` | Detect + log only; no resubmit/repoint. |
| `BOXARR_HEAL_MAX_ATTEMPTS` | no | `3` | Max heal attempts before give-up; must be `>0`. |
| `BOXARR_HEAL_BACKOFF_INITIAL` | no | `5m` | Initial heal backoff (grows per attempt). |
| `BOXARR_HEAL_PROWLARR_FALLBACK` | no | `true` | **NEW:** on dead stored artifact, fall back to a fresh Prowlarr re-search + best-match repoint (`00` §19.4 / FR-HEAL-2). |
| `BOXARR_HEAL_WEBHOOK_URL` | no | — | POST a JSON heal notification; empty disables. |
| `BOXARR_HEAL_WEBHOOK_EVENTS` | no | `failed` | Comma-separated subset of `detected,healing,healed,failed`. |
| **DROPPED (Assumption B)** ||||
| ~~`BOXARR_SAB_API_KEY`~~ | — | — | **Removed** — SAB emulation gone; Seerr auth uses `BOXARR_SEERR_API_KEYS`. |
| ~~`BOXARR_SYMLINK_ROOT`~~ | — | — | **Removed** — replaced by `MOVIE_LIBRARY_ROOT` + `TV_LIBRARY_ROOT` (`00` §5.1). |
| ~~`BOXARR_CATEGORIES`~~ | — | — | **Removed** — SAB download-client categories have no meaning without SAB. |

**Quirks to bake in:**

1. **Prefix string is the single source of the `BOXARR_` prefix** — `envconfig.Process("boxarr", &c)`; never hardcode `BOXARR_` elsewhere (verified `04` §1; `01` step 4).
2. **Required vars fail fast at parse** (`required:"true"` on `TORBOX_API_TOKEN`, `WEBDAV_MOUNT_ROOT`, `PROWLARR_URL`, `PROWLARR_API_KEY`, `TMDB_API_KEY`); a missing one aborts startup with a clear envconfig error before any I/O.
3. **Library roots default to `/data/movies` + `/data/tv`** to match the `root_folder` seed in `009_servarr.sql` (ids 2 + 1, verified `02` §3.6) — the path Boxarr returns from `/rootfolder` **must equal** what it accepts in a Seerr `POST` `rootFolderPath` (`00` §9 Seerr). Changing the env updates the seeded row via `UpsertRootFolder` (`02` §3.6 note 2), not a re-migration.
4. **`MAX_SIZE_MB=0` means no cap** — the only knob using `0` as a sentinel; min-size still applies.
5. **Slice fields are comma-separated, no spaces trimmed by envconfig** — document this in `.env.example` so operators don't write `a, b` (the proven `Categories` behavior, `04` Key Facts; `SEERR_API_KEYS`, `HEAL_LIBRARY_ROOTS`, and all `SELECT_*` csv lists follow it).
6. **Optional integrations degrade, not crash:** absent Plex → no partial scan (relies on Plex's own schedule); absent TVDB → TMDB-only ordering; absent Seerr keys → `/sonarr` + `/radarr` return 401. Each is gated by an `*Enabled()` predicate, never a hard requirement.
7. **Secrets never logged** (NFR-4) — the startup log enumerates enabled integrations, not their credentials.

---

## 3. Healthcheck subcommand (carried + repointed)

The single `healthcheck` CLI subcommand is **kept verbatim in shape** (verified `04` §3) — it self-`GET`s `/healthz` so the distroless image needs no shell. **One change:** it reads **`BOXARR_LISTEN_ADDR`** (was `SAB2TORBOX_LISTEN_ADDR`) via `os.Getenv` (**not** `config.Load`), default `:8080`, rewriting a leading `:` → `127.0.0.1:` (verified `04` §3; `01` step 4).

```go
func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(runHealthcheck())
	}
	if err := run(); err != nil { /* log + os.Exit(1) */ }
}

func runHealthcheck() int {
	addr := os.Getenv("BOXARR_LISTEN_ADDR")   // CHANGED from SAB2TORBOX_LISTEN_ADDR
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
```

The `/healthz` handler and its `Health`/`Checker`/`Pinger` shape are kept; the `Pinger` is repointed at Boxarr's real dependencies (DB + TorBox token; optionally Prowlarr/TMDB reachability), keeping the 5-minute TorBox ping cache (`00` §5.6; verified `04` §2).

---

## 4. Multi-stage Dockerfile (`deploy/Dockerfile`)

Three stages: **frontend (Node/pnpm)** → **build (Go)** → **runtime (distroless)**. The Go build + runtime stages are the verified sab2torbox Dockerfile (`04` §6) with the binary renamed `boxarr`; the frontend stage and the `COPY --from=frontend … internal/web/dist` are net-new (`04` Recommendations §Dockerfile; `01` §10). **Verbatim target:**

```dockerfile
# syntax=docker/dockerfile:1

# ── Stage 1: frontend (React + TS + Vite, pnpm) ─────────────────────────
FROM node:22-alpine AS frontend
WORKDIR /web
# Corepack pins pnpm from web/package.json "packageManager" (FR-SEC-6).
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/.npmrc ./
# --frozen-lockfile fails on lockfile drift (FR-SEC-1); --ignore-scripts
# blocks lifecycle/postinstall (FR-SEC-3); only onlyBuiltDependencies run.
RUN pnpm install --frozen-lockfile --ignore-scripts
COPY web/ ./
RUN pnpm build            # vite build -> /web/dist (static, embedded — FR-SEC-5)

# ── Stage 2: Go build (embeds the built frontend) ───────────────────────
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Built SPA lands where //go:embed all:dist expects it (01 §embedded SPA).
COPY --from=frontend /web/dist ./internal/web/dist
ARG TARGETOS TARGETARCH
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/boxarr ./cmd/boxarr

# ── Stage 3: runtime (distroless, nonroot) ──────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/boxarr /boxarr
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=60s --timeout=5s --retries=3 \
    CMD ["/boxarr", "healthcheck"]
ENTRYPOINT ["/boxarr"]
```

**Locked invariants (carried, verified `04` §6):**

1. **`CGO_ENABLED=0`** — the binary stays cross-compilable into `gcr.io/distroless/static-debian12:nonroot`; `modernc.org/sqlite` + the pure-Go `torrentname`/`anitogo` parsers preserve this (`01` CGO invariant).
2. **`GOOS=${TARGETOS} GOARCH=${TARGETARCH}`** — buildx cross-compiles per platform from the release matrix (`§6`).
3. **`-ldflags "-s -w -X main.version=${VERSION}"` + `-trimpath`** — strips symbols and stamps the version (CI run number, `§6`).
4. **`HEALTHCHECK CMD ["/boxarr", "healthcheck"]`** — the subcommand from `§3`; no shell in distroless.
5. **`USER nonroot:nonroot` (uid 65532)** — `/config` must be writable by uid 65532 (compose, `§5`).
6. **Frontend isolation (FR-SEC-1/3/5/6):** pnpm via corepack (pinned `packageManager`), `--frozen-lockfile`, `--ignore-scripts`; the runtime image carries **zero** Node — only the static binary with the SPA embedded.

A committed placeholder `internal/web/dist/index.html` keeps `go build ./...` green during local dev before the frontend is built (`01` §embedded SPA; `07`/`08` own the placeholder).

---

## 5. docker-compose (`deploy/docker-compose.yml`)

Carried from the verified sab2torbox compose (`04` §7) with: image renamed `ghcr.io/radaiko/boxarr`; all env `BOXARR_*`; `SAB_API_KEY` dropped; `SYMLINK_ROOT` replaced by the movie/TV library binds; the new Prowlarr/TMDB/Plex/Seerr env wired through. The **rclone WebDAV bind** and the **media-library bind** keep `propagation: rslave`; `./config:/config` stays writable by uid 65532.

```yaml
services:
  boxarr:
    image: ghcr.io/radaiko/boxarr:latest
    container_name: boxarr
    restart: unless-stopped
    environment:
      # ── Core ──
      - BOXARR_TORBOX_API_TOKEN=${TORBOX_API_TOKEN}
      - BOXARR_WEBDAV_MOUNT_ROOT=/mnt/torbox
      - BOXARR_WEBDAV_USENET_SUBPATH=
      - BOXARR_WEBDAV_TORRENT_SUBPATH=
      - BOXARR_MOVIE_LIBRARY_ROOT=/data/movies
      - BOXARR_TV_LIBRARY_ROOT=/data/tv
      - BOXARR_DATABASE_PATH=/config/boxarr.db
      - BOXARR_POLL_INTERVAL=1m
      - BOXARR_RECONCILE_INTERVAL=15m
      - BOXARR_METADATA_REFRESH_INTERVAL=24h
      - BOXARR_LOG_LEVEL=info
      # ── Indexers / metadata / playback ──
      - BOXARR_PROWLARR_URL=${PROWLARR_URL}
      - BOXARR_PROWLARR_API_KEY=${PROWLARR_API_KEY}
      - BOXARR_TMDB_API_KEY=${TMDB_API_KEY}
      - BOXARR_TVDB_API_KEY=${TVDB_API_KEY:-}
      - BOXARR_TVDB_PIN=${TVDB_PIN:-}
      - BOXARR_PLEX_URL=${PLEX_URL:-}
      - BOXARR_PLEX_TOKEN=${PLEX_TOKEN:-}
      # ── Seerr inbound emulation ──
      - BOXARR_SEERR_API_KEYS=${SEERR_API_KEYS:-}
      # ── TorBox WebDAV force-refresh (optional) ──
      - BOXARR_TORBOX_WEBDAV_USER=${TORBOX_WEBDAV_USER:-}
      - BOXARR_TORBOX_WEBDAV_PASS=${TORBOX_WEBDAV_PASS:-}
      - TZ=Europe/Vienna
    ports:
      - "8181:8080"
    volumes:
      # /config must be writable by uid 65532 (the distroless nonroot user).
      - ./config:/config
      # The rclone WebDAV mount. rslave lets the container see the host mount
      # without propagating its own mounts back to the host. Must be the SAME
      # absolute path here and in the Plex container (Assumption D).
      - type: bind
        source: /mnt/torbox
        target: /mnt/torbox
        bind:
          propagation: rslave
      # The filesystem holding the movie + TV library roots. Same filesystem
      # as the WebDAV mount so the library symlink is a rename, not a copy.
      - type: bind
        source: /data
        target: /data
        bind:
          propagation: rslave
    healthcheck:
      test: ["CMD", "/boxarr", "healthcheck"]
      interval: 60s
      timeout: 5s
      retries: 3
```

**Quirks to bake in (deployment contract):**

1. **`/config` writable by uid 65532** (distroless nonroot) — else SQLite open fails. Document `chown -R 65532:65532 ./config`.
2. **`/mnt/torbox` and `/data` mounted at the *same absolute path* in the Plex container** (Assumption D) — library symlinks point into `/mnt/torbox/...` by absolute path; if Plex sees a different path, links dangle. `Load()` warns on cross-device library/mount (`§1`).
3. **`propagation: rslave`** on both binds — the container sees the host's rclone mount without propagating its own mounts back (verified `04` §7).
4. **`SAB_API_KEY` removed; `SYMLINK_ROOT` removed** — replaced by the two library roots (`00` §5.1 / Assumption B).
5. **Host port 8181 → container 8080** (carried; `EXPOSE 8080`).

---

## 6. CI workflow (`.github/workflows/ci.yml`)

The Go `test` job and the no-push `docker-build` job are **carried verbatim** from sab2torbox (`04` §4); a **new `frontend` job** is added (`04` Recommendations §CI) for FR-CI-1/FR-CI-6. The `docker-build` job now also exercises the frontend Dockerfile stage (FR-CI-1 "image still builds").

```yaml
name: ci
on:
  push:
    branches: [main]
  pull_request:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
      - name: gofmt
        run: |
          unformatted=$(gofmt -s -l .)
          if [ -n "$unformatted" ]; then
            echo "Unformatted files:"; echo "$unformatted"; exit 1
          fi
      - name: vet
        run: go vet ./...
      - name: golangci-lint
        uses: golangci/golangci-lint-action@v8
        with:
          version: latest
      - name: test
        run: go test ./... -race -cover
  frontend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: pnpm/action-setup@v4
      - uses: actions/setup-node@v4
        with:
          node-version: '22'
          cache: 'pnpm'
          cache-dependency-path: web/pnpm-lock.yaml
      - name: install (frozen, no scripts)
        run: pnpm install --frozen-lockfile --ignore-scripts
        working-directory: web
      - name: lint
        run: pnpm lint
        working-directory: web
      - name: build
        run: pnpm build
        working-directory: web
      - name: audit (advisory)
        run: pnpm audit --audit-level=high || true
        working-directory: web
  docker-build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: docker/setup-buildx-action@v3
      - name: build image (no push)
        uses: docker/build-push-action@v6
        with:
          context: .
          file: deploy/Dockerfile
          push: false
```

**Locked invariants:**

1. **Go gates carried verbatim** (FR-CI-1): `gofmt -s -l .` (fails if any output), `go vet ./...`, `golangci-lint-action@v8` `version: latest`, `go test ./... -race -cover` (verified `04` §4).
2. **Frontend gates** (FR-CI-1/6): `pnpm install --frozen-lockfile` fails on lockfile drift (FR-SEC-1); `--ignore-scripts` (FR-SEC-3); `pnpm lint`; `pnpm build` proves the production build; `pnpm audit --audit-level=high` is an **advisory** gate (`|| true`, FR-CI-6).
3. **`docker-build` push:false** proves the full multi-stage image (now incl. the frontend stage) still builds on every PR/push (FR-CI-1/4; verified `04` §4).
4. **pnpm pinned via corepack** through the `packageManager` field — `pnpm/action-setup@v4` honors it (FR-SEC-6); `cache: 'pnpm'` keyed on `web/pnpm-lock.yaml`.
5. **Action major versions pinned** (FR-CI-5): `checkout@v4`, `setup-go@v5`, `golangci-lint-action@v8`, `pnpm/action-setup@v4`, `setup-node@v4`, `setup-buildx-action@v3`, `build-push-action@v6`.

---

## 7. Release workflow (`.github/workflows/release.yml`)

**Carried verbatim** from sab2torbox (`04` §5) — multi-arch buildx + QEMU + GHCR login + `docker/metadata-action` tags + `VERSION` build-arg ldflags — with the **image renamed `ghcr.io/radaiko/boxarr`** (`00` §6). The frontend stage builds transparently inside the same multi-arch Dockerfile (FR-CI-3/4).

```yaml
name: release
on:
  push:
    branches: [main]
    tags: ['v*']
jobs:
  publish:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write
    steps:
      - uses: actions/checkout@v4
      - uses: docker/setup-qemu-action@v3
      - uses: docker/setup-buildx-action@v3
      - name: log in to GHCR
        uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - name: docker metadata
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: ghcr.io/radaiko/boxarr
          tags: |
            type=raw,value=latest,enable={{is_default_branch}}
            type=semver,pattern=v{{version}}
            type=semver,pattern=v{{major}}.{{minor}}
      - name: build and push
        uses: docker/build-push-action@v6
        with:
          context: .
          file: deploy/Dockerfile
          platforms: linux/amd64,linux/arm64
          push: true
          build-args: |
            VERSION=${{ github.run_number }}
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
```

**Locked invariants (verified `04` §5):**

1. **Triggers on push to `main` AND tags `v*`** (FR-CI-2).
2. **`permissions: contents: read, packages: write`** with the built-in `GITHUB_TOKEN` (FR-CI-2).
3. **`platforms: linux/amd64,linux/arm64`** via QEMU + buildx (FR-CI-2 multi-arch).
4. **`metadata-action` tags:** `latest` on the default branch, `v{{version}}` + `v{{major}}.{{minor}}` from semver tags (FR-CI-2); its `labels` apply OCI provenance (FR-CI-5).
5. **`VERSION=${{ github.run_number }}` build-arg → ldflags `-X main.version`** stamps the binary (FR-CI-2).

---

## 8. `.golangci.yml` (carried verbatim)

Carried unchanged from sab2torbox (verified `04` §9) — schema v2, the five linters, `errcheck` excluded on test files:

```yaml
version: "2"
linters:
  enable:
    - errcheck
    - govet
    - ineffassign
    - staticcheck
    - unused
  exclusions:
    rules:
      - path: _test\.go
        linters:
          - errcheck
```

This is the lint config the CI `golangci-lint-action@v8 version: latest` step (`§6`) enforces. The coding standards it backs (`gofmt -s` + lint clean, exported symbols documented, no `panic` outside `main`, wrapped errors, structured `slog`) are owned by `00` §6 and apply repo-wide.

---

## 9. `.env.example` (full, commented)

Committed at the repo root for local `docker-compose` (`§5` reads these via `${...}` substitution). Required vars carry real placeholders; optional vars are commented with their default.

```bash
# ── Boxarr environment (BOXARR_* prefix via envconfig.Process("boxarr")) ──
# Required vars abort startup if unset. Slice vars are COMMA-SEPARATED with
# NO spaces (e.g. a,b,c — not "a, b, c"). Secrets are never logged.

# ── Core (required) ──
BOXARR_TORBOX_API_TOKEN=your-torbox-token
BOXARR_PROWLARR_URL=http://prowlarr:9696
BOXARR_PROWLARR_API_KEY=your-prowlarr-api-key
BOXARR_TMDB_API_KEY=your-tmdb-read-access-token

# ── WebDAV mount (required) ──
# Absolute path of the rclone TorBox WebDAV mount. MUST be the same absolute
# path in every container (incl. Plex) or library symlinks dangle.
BOXARR_WEBDAV_MOUNT_ROOT=/mnt/torbox
# Leave empty unless usenet release folders nest under a subdir.
BOXARR_WEBDAV_USENET_SUBPATH=
# Leave empty unless torrent release folders surface under a SEPARATE subpath.
# (Runtime-verify against the live mount — 00 §19.5.)
BOXARR_WEBDAV_TORRENT_SUBPATH=

# ── Library roots (default /data/movies, /data/tv) ──
# Plex-standard layout. Must share the WebDAV mount's filesystem (rename, not
# copy). These seed the Seerr root_folder rows; the path returned from
# /rootfolder must equal what Seerr POSTs back.
BOXARR_MOVIE_LIBRARY_ROOT=/data/movies
BOXARR_TV_LIBRARY_ROOT=/data/tv

# ── Storage / runtime ──
BOXARR_DATABASE_PATH=/config/boxarr.db
BOXARR_LISTEN_ADDR=:8080
BOXARR_LOG_LEVEL=info
# Auth for Boxarr's own /api/v1 SPA surface (X-Api-Key header). Empty + loopback
# request = allowed (single-operator local-first). Does NOT affect /sonarr,/radarr.
BOXARR_API_KEY=
BOXARR_POLL_INTERVAL=1m
BOXARR_RECONCILE_INTERVAL=15m
BOXARR_METADATA_REFRESH_INTERVAL=24h
BOXARR_SEARCH_INTERVAL=6h

# ── TVDB (optional — supplements TMDB for scene/absolute episode ordering) ──
BOXARR_TVDB_API_KEY=
# Subscriber PIN — only for user-supported keys (project keys need none).
BOXARR_TVDB_PIN=

# ── Plex (optional — enables fast partial scans after import) ──
BOXARR_PLEX_URL=
BOXARR_PLEX_TOKEN=
# Section ids are auto-detected from /library/sections; override only if needed.
# BOXARR_PLEX_MOVIE_SECTION=
# BOXARR_PLEX_TV_SECTION=
# Set only if Boxarr's and Plex's mounts diverge (host=plex prefix rewrite).
# BOXARR_HOST_TO_PLEX_PATH_PREFIX=

# ── Seerr inbound emulation (optional — Overseerr/Jellyseerr add) ──
# Comma-separated keys accepted by /sonarr/api/v3 and /radarr/api/v3.
BOXARR_SEERR_API_KEYS=

# ── TorBox WebDAV force-refresh (optional — set BOTH to enable) ──
BOXARR_TORBOX_WEBDAV_USER=
BOXARR_TORBOX_WEBDAV_PASS=
BOXARR_TORBOX_WEBDAV_REFRESH_URL=https://webdav.torbox.app/refresh
BOXARR_TORBOX_WEBDAV_REFRESH_COOLDOWN=2m

# ── Selection score (FR-SR-5; full knob list + algorithm in docs/specs/06-pipelines.md §3) ──
# Reject conditions:
BOXARR_SELECT_ALLOWED_RESOLUTIONS=
BOXARR_SELECT_MIN_SIZE=0
BOXARR_SELECT_MAX_SIZE=0
BOXARR_SELECT_SIZE_LIMITS={}
BOXARR_SELECT_MIN_SEEDERS=1
BOXARR_SELECT_MIN_GRABS=0
BOXARR_SELECT_REQUIRE_CACHED=false
BOXARR_SELECT_BLOCKED_GROUPS=
BOXARR_SELECT_BLOCKED_KEYWORDS=
BOXARR_SELECT_MIN_SCORE=0
# Scoring preferences (ordered best→worst):
BOXARR_SELECT_PREFERRED_RESOLUTIONS=2160p,1080p,720p
BOXARR_SELECT_PREFERRED_QUALITIES=WEB-DL,BluRay,WEBRip,HDTV
BOXARR_SELECT_PREFERRED_GROUPS=
BOXARR_SELECT_PREFERRED_KEYWORDS=
# Scoring weights (defaults make cached-torrent > usenet > uncached-torrent):
BOXARR_SELECT_WEIGHT_RESOLUTION=400
BOXARR_SELECT_WEIGHT_QUALITY=200
BOXARR_SELECT_WEIGHT_PROTOCOL_CACHED_TORRENT=300
BOXARR_SELECT_WEIGHT_PROTOCOL_USENET=200
BOXARR_SELECT_WEIGHT_PROTOCOL_UNCACHED_TORRENT=100
BOXARR_SELECT_WEIGHT_HEALTH=100
BOXARR_SELECT_SEED_SATURATION=100
BOXARR_SELECT_WEIGHT_PREFERRED_GROUP=150
BOXARR_SELECT_WEIGHT_PREFERRED_KEYWORD=50
BOXARR_SELECT_WEIGHT_FREELEECH=40
BOXARR_SELECT_WEIGHT_PROPER=25

# ── TorBox limits (FR-LIM-2/3) ──
# 0 = derive active-slot limit from the TorBox plan (06 §7.1); >0 = hard cap.
BOXARR_MAX_ACTIVE_DOWNLOADS=0
BOXARR_MAX_CREATE_PER_HOUR=60
BOXARR_MAX_TORRENT_PER_MIN=300
BOXARR_SEARCH_CONCURRENCY=3

# ── Auto-heal (optional — HEAL_LIBRARY_ROOTS required when enabled) ──
BOXARR_HEAL_ENABLED=false
BOXARR_HEAL_INTERVAL=1h
BOXARR_HEAL_LIBRARY_ROOTS=/data/tv,/data/movies
BOXARR_HEAL_DRY_RUN=false
BOXARR_HEAL_MAX_ATTEMPTS=3
BOXARR_HEAL_BACKOFF_INITIAL=5m
BOXARR_HEAL_PROWLARR_FALLBACK=true
BOXARR_HEAL_WEBHOOK_URL=
BOXARR_HEAL_WEBHOOK_EVENTS=failed
```

---

## 10. Frontend supply-chain policy (`web/.npmrc`, `package.json`, Renovate)

Enforces requirements §17.2 / FR-SEC-1..7 for the React+TS+Vite/pnpm toolchain (the full SPA structure is owned by `07-frontend.md`; this section owns only the **config artifacts that CI/Docker enforce**).

**`web/.npmrc` (committed):**

```ini
# FR-SEC-1: exact versions only (no ^/~).
save-exact=true
# FR-SEC-3: no lifecycle/postinstall scripts — the top npm-malware vector.
ignore-scripts=true
# Fail any install on lockfile drift (belt-and-braces with --frozen-lockfile).
frozen-lockfile=true
```

**`web/package.json` excerpt (committed):**

```json
{
  "packageManager": "pnpm@9.15.4",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "lint": "eslint . && tsc --noEmit"
  },
  "pnpm": {
    "onlyBuiltDependencies": ["esbuild"]
  }
}
```

**Quirks to bake in (FR-SEC mapping):**

1. **FR-SEC-1** — `save-exact=true` + committed authoritative `pnpm-lock.yaml`; every install (`§4`, `§6`) uses `--frozen-lockfile` and fails on drift.
2. **FR-SEC-2/7** — bumps go through reviewed PRs with a minimum release age; a committed `renovate.json` sets `"minimumReleaseAge": "21 days"` so freshly published (possibly compromised) versions are never auto-pulled. Security patches applied deliberately after review.
3. **FR-SEC-3** — `ignore-scripts=true` globally; build scripts permitted only for the explicit `onlyBuiltDependencies` allowlist (`esbuild`, which Vite needs).
4. **FR-SEC-4** — minimal dependency surface: React + Vite + TypeScript built-ins; no heavy UI/component libraries with deep transitive trees (`07`).
5. **FR-SEC-5** — all JS/CSS built at build time and embedded via `go:embed` (`§4`, `01`); the running app loads **no** third-party CDN scripts/styles.
6. **FR-SEC-6** — pnpm pinned via `packageManager` (corepack) so CI/Docker/local use an identical toolchain.

---

## 11. Requirement traceability (FR-CI-* / FR-SEC-*)

| Requirement | Mechanism (this spec) |
|---|---|
| **FR-CI-1** CI on every PR/push | `ci.yml`: `test` (gofmt/vet/lint/`go test -race -cover`) + `frontend` (`--frozen-lockfile`, lint, build) + `docker-build` no-push (`§6`). |
| **FR-CI-2** release/publish | `release.yml`: push-main + `v*` tags → multi-arch buildx → GHCR `ghcr.io/radaiko/boxarr`, `metadata-action` tags, `GITHUB_TOKEN` perms, `VERSION` ldflags (`§7`). |
| **FR-CI-3** multi-stage Dockerfile | frontend (pnpm) → Go (embed) → distroless runtime (`§4`). |
| **FR-CI-4** single self-contained image | SPA embedded via `go:embed`; one image, one container + WebDAV mount (`§4`, `§5`). |
| **FR-CI-5** provenance / pinned actions | `metadata-action` OCI labels; all actions pinned to major versions (`§6`, `§7`). |
| **FR-CI-6** supply-chain in CI | `pnpm install --frozen-lockfile --ignore-scripts` fails on drift; `pnpm audit` advisory gate (`§6`, `§10`). |
| **FR-SEC-1** exact pin + lockfile | `.npmrc save-exact`; committed `pnpm-lock.yaml`; `--frozen-lockfile` everywhere (`§10`). |
| **FR-SEC-2** vetted, not bleeding-edge | Renovate `minimumReleaseAge: 21 days`; reviewed bumps (`§10`). |
| **FR-SEC-3** no install scripts | `.npmrc ignore-scripts=true`; `--ignore-scripts`; `onlyBuiltDependencies` allowlist (`§4`, `§6`, `§10`). |
| **FR-SEC-4** minimal surface | React/Vite/TS built-ins only (`§10`; `07`). |
| **FR-SEC-5** no runtime third-party fetch | build-time bundle embedded via `go:embed` (`§4`). |
| **FR-SEC-6** pinned package manager | `packageManager` field + corepack (`§4`, `§6`, `§10`). |
| **FR-SEC-7** controlled updates | Renovate `minimumReleaseAge` (`§10`). |
| **NFR-4** secrets never logged | startup log enumerates enabled integrations, not credentials (`§1`, `§2` quirk 7). |
| **NFR-5** twelve-factor config | `BOXARR_*` env + `settings` overlay; defaults here, UI overrides in `04`/`07` (`§1`). |
| **NFR-6** single-container deploy | distroless image + compose with WebDAV/library binds, uid 65532 (`§4`, `§5`). |

---

## Definition of done

`config.Load()` parses `envconfig.Process("boxarr", &c)` into the §2.1 struct — required `TORBOX_API_TOKEN`/`WEBDAV_MOUNT_ROOT`/`PROWLARR_URL`/`PROWLARR_API_KEY`/`TMDB_API_KEY` abort startup when unset, every other var resolves to its tabulated default, slice vars split on commas, and `Load()` fail-fast `os.Stat`-validates the WebDAV mount + both library roots (plus `HEAL_LIBRARY_ROOTS` when `HEAL_ENABLED`) and warns on cross-device library/mount; the `*Enabled()` predicates (`PlexEnabled`/`TVDBEnabled`/`SeerrEnabled`/`WebDAVRefreshEnabled`/`TorrentsEnabled`) gate every optional integration; `runHealthcheck()` reads `BOXARR_LISTEN_ADDR`; `SAB_API_KEY`/`SYMLINK_ROOT`/`CATEGORIES` are gone; the three-stage `deploy/Dockerfile` builds the pnpm frontend (`--frozen-lockfile --ignore-scripts`) into `internal/web/dist`, cross-compiles the `CGO_ENABLED=0` Go binary with version ldflags, and ships `gcr.io/distroless/static-debian12:nonroot` with `EXPOSE 8080`, `USER nonroot`, and `HEALTHCHECK ["/boxarr","healthcheck"]`; `docker-compose.yml` runs `ghcr.io/radaiko/boxarr` with `BOXARR_*` env, the `propagation: rslave` WebDAV + `/data` library binds, and `./config` writable by uid 65532; `ci.yml` runs the verbatim Go gates + the new `frontend` job + the no-push image build; `release.yml` publishes multi-arch `linux/amd64,linux/arm64` to `ghcr.io/radaiko/boxarr` with `latest`/`v{version}`/`v{major}.{minor}` tags and `VERSION` ldflags; `.golangci.yml` and `.env.example` are committed; and every FR-CI-*/FR-SEC-* row in §11 maps to a concrete, checkable mechanism. See `01-architecture-and-packages.md` (package map, embed, worker intervals), `02-data-model.md` (`settings` overlay, `root_folder` seed), `03-external-contracts.md` (Prowlarr/TMDB/TVDB/Plex auth + `Enabled()` gating), and `07-frontend.md` (SPA + supply-chain detail).
