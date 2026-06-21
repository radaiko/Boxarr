<div align="center">

<img src="docs/boxarr.svg" width="96" height="96" alt="Boxarr" />

# Boxarr

**A single, self-hosted media manager for movies, series and anime — backed by [TorBox](https://torbox.app).**

</div>

---

## What is Boxarr?

Boxarr manages your library end-to-end on top of TorBox: it **searches** (via
Prowlarr), **grabs** (TorBox usenet + torrents), **imports** by writing a symlink
straight to your Plex library, and **emulates Sonarr/Radarr v3** so request apps
like Seerr/Overseerr/Jellyseerr can request straight into it.

```
 Seerr ──requests──▶ Boxarr  (/sonarr, /radarr emulation)
                        │  search via Prowlarr → grab via TorBox
                        ▼
                     TorBox  ──WebDAV──▶ rclone FUSE mount (/mnt/torbox)
                        │
                        ▼  Boxarr writes a SYMLINK at the final Plex path
                     /mnt/library/{movies,tv,anime}/…  ──▶ Plex
```

Import is just a symlink to the file on the rclone mount — **no byte copy, no
rename, no same-filesystem requirement** — so the library can be a tiny local
directory. The one hard rule: **Plex must see `/mnt/torbox` at the same absolute
path** Boxarr does, because the symlink targets are absolute.

## Highlights

- **Movies, Series & Anime** as first-class libraries. Anime is a series subtype
  with its own library root and Plex section; convert a series to/from anime and
  Boxarr relocates the files.
- **Seerr emulation** — exposes `/sonarr/api/v3` and `/radarr/api/v3` so request
  apps add straight into Boxarr (auto-generate the API key in Settings).
- **Sign in with Plex** (PIN OAuth) — pick your server and map libraries from a
  dropdown instead of pasting URLs, tokens and section IDs.
- **TorBox view** — account/usage stats, a learned-limits panel, and a WebDAV
  mount browser grouped by title with covers and tracked/unknown status. **Adopt**
  existing mount content into the library (search-and-pick the exact TMDB match),
  or **delete** it (single, multi-select, or a whole show). Click a tracked item
  to jump to its library page.
- **Release selection** — a transparent weighted score with per-content-type
  **language rules** (e.g. German required + English preferred for movies/series;
  German *or* English for anime, preferring English subs).
- **Filterable search overlay** — per-item release search with language/subtitle
  columns and resolution/cached/subs filters; the currently-grabbed release is
  flagged.
- **Activity** — tabbed Queue / History / Tasks. Live download queue with release
  details + ETA, finished/failed download history, and background adopt/delete
  tasks with progress and per-file detail. Task + download history is persisted
  and survives a restart.
- **Adaptive limits** — learns and remembers TorBox throttling (429/cooldown),
  pauses submissions across restarts, and infers a daily-grab ceiling.
- **Self-healing** — detects broken library symlinks and re-acquires them.
- **Everything is UI-settable** and applies live (no restart); env vars below are
  an optional pre-seed.

## Quick start (Docker Compose)

The full stack — **Boxarr + rclone (TorBox WebDAV mount) + Seerr** — on one
network. This mirrors [`deploy/docker-compose.yml`](deploy/docker-compose.yml).

```yaml
services:
  # ── Boxarr ────────────────────────────────────────────────────────────────
  boxarr:
    image: ${BOXARR_IMAGE:-ghcr.io/radaiko/boxarr:latest}
    container_name: boxarr
    restart: unless-stopped
    user: "1000:1000"                 # owns /config + the library dir (writable)
    environment:
      - BOXARR_DATABASE_PATH=/config/boxarr.db
      - BOXARR_LISTEN_ADDR=:8080
      - TZ=Europe/Vienna
      # Optional pre-seed — or just set these in the UI → Settings (UI wins, live):
      - BOXARR_TORBOX_API_TOKEN=${TORBOX_API_KEY:-}
      - BOXARR_PROWLARR_URL=${PROWLARR_URL:-http://host.docker.internal:9696}
      - BOXARR_PROWLARR_API_KEY=${PROWLARR_API_KEY:-}
      - BOXARR_TMDB_API_KEY=${TMDB_API_KEY:-}
      - BOXARR_SEERR_API_KEYS=${BOXARR_SEERR_API_KEY:-}
      - BOXARR_WEBDAV_MOUNT_ROOT=/mnt/torbox
      - BOXARR_MOVIE_LIBRARY_ROOT=/mnt/library/movies
      - BOXARR_TV_LIBRARY_ROOT=/mnt/library/tv
      - BOXARR_ANIME_LIBRARY_ROOT=/mnt/library/anime
      - BOXARR_PLEX_URL=${PLEX_URL:-}
      - BOXARR_PLEX_TOKEN=${PLEX_TOKEN:-}
    ports:
      - "8181:8080"
    volumes:
      - /mnt/appdata/boxarr:/config            # SQLite db
      - /mnt/library:/mnt/library              # symlink library (Plex mounts this too)
      - type: bind                             # rclone TorBox mount — SAME path in Plex
        source: /mnt/torbox
        target: /mnt/torbox
        bind: { propagation: rslave }
    depends_on: [rclone]
    networks: [media]
    healthcheck:
      test: ["CMD", "/boxarr", "healthcheck"]
      interval: 60s
      timeout: 5s
      retries: 3

  # ── rclone: TorBox WebDAV → FUSE mount at /mnt/torbox ──────────────────────
  rclone:
    image: rclone/rclone:latest
    container_name: rclone
    restart: unless-stopped
    cap_add: [SYS_ADMIN]
    devices: ["/dev/fuse:/dev/fuse"]
    security_opt: ["apparmor:unconfined"]
    environment:
      - TZ=Europe/Vienna
    volumes:
      - /mnt/appdata/rclone/rclone.conf:/config/rclone/rclone.conf:ro
      - /mnt/appdata/rclone/cache:/cache
      - type: bind
        source: /mnt/torbox
        target: /data
        bind: { propagation: rshared }
    command: >
      mount torbox: /data
      --allow-other --allow-non-empty --dir-cache-time 1h
      --vfs-cache-mode full --vfs-cache-max-size 50G --vfs-cache-max-age 168h
      --vfs-read-ahead 256M --vfs-read-chunk-size 32M --vfs-read-chunk-size-limit 1G
      --buffer-size 64M --vfs-fast-fingerprint --no-checksum --no-modtime
      --transfers 4 --checkers 2 --tpslimit 5 --tpslimit-burst 5 --low-level-retries 3
      --attr-timeout 9999h --umask 002 --uid 1000 --gid 1000 --cache-dir /cache --log-level INFO
    networks: [media]

  # ── Seerr: request UI → points at Boxarr's Sonarr/Radarr emulation ─────────
  seerr:
    image: ghcr.io/seerr-team/seerr:latest
    container_name: seerr
    init: true
    restart: unless-stopped
    environment:
      - LOG_LEVEL=info
      - TZ=Europe/Vienna
      - PORT=5055
    volumes:
      - /mnt/appdata/seerr:/app/config
    ports:
      - "5055:5055"
    extra_hosts:
      - "host.docker.internal:host-gateway"
    networks: [media]

networks:
  media:
    name: media
    driver: bridge
```

### Host prep

```bash
mkdir -p /mnt/appdata/{boxarr,rclone,seerr} /mnt/torbox /mnt/library/{movies,tv,anime}
chown -R 1000:1000 /mnt/appdata/{boxarr,rclone,seerr} /mnt/torbox /mnt/library
# Configure the rclone TorBox WebDAV remote named "torbox:" in
#   /mnt/appdata/rclone/rclone.conf
# (rclone config → new remote → type "webdav" → TorBox WebDAV URL + credentials)
```

Bring it up with `docker compose up -d`, then open Boxarr at `http://<host>:8181`
and finish setup in **Settings**.

> **rclone `--dir-cache-time` matters.** TorBox is a WebDAV remote, and rclone's
> `--poll-interval` change-notification is **not supported on WebDAV** — so the
> directory cache only refreshes when `--dir-cache-time` expires. With a very long
> value (e.g. `9999h`) rclone never notices content **added or removed** on TorBox,
> so deleted folders linger in the mount (and freshly-grabbed ones can be slow to
> appear). Keep `--dir-cache-time` modest — `1h` is a good default; drop to `5m` if
> you want deletions/additions to reflect faster (at the cost of more listing
> requests). Boxarr tombstones a path it deletes so its **own** view is correct
> immediately regardless, but the mount itself (and Plex) follow `--dir-cache-time`.

### Seerr setup (one-time)

In Seerr → Settings → Services, add both with the Seerr API key from Boxarr
(Settings → Requests → generate):

- **Sonarr** → Server URL `http://boxarr:8080/sonarr`
- **Radarr** → Server URL `http://boxarr:8080/radarr`

Quality-profile / root-folder dropdowns are served by Boxarr's emulation.

### Plex (separate stack)

Mount **both** paths so symlinks resolve, and point Plex libraries at the library
roots:

```yaml
  plex:
    volumes:
      - type: bind        # the library (where Boxarr writes symlinks)
        source: /mnt/library
        target: /mnt/library
        bind: { propagation: rslave }
      - type: bind        # symlink targets — SAME absolute path as Boxarr
        source: /mnt/torbox
        target: /mnt/torbox
        bind: { propagation: rslave }
```

Plex libraries: Movies → `/mnt/library/movies`, Shows → `/mnt/library/tv`,
Anime → `/mnt/library/anime`. Or just **Sign in with Plex** in Settings and map
them from the dropdown.

## Configuration

Everything is settable in the UI (and applies live); the env vars above are an
optional pre-seed.

| What | Required? | Notes |
|------|-----------|-------|
| **TorBox API token** | yes | torbox.app → Settings → API |
| **Prowlarr URL + API key** | yes | indexer search |
| **TMDB API Read Access Token (v4)** | yes | themoviedb.org → Settings → API. Boxarr queries TMDB directly, so bring your own free token. |
| **TVDB** | optional | scene/absolute ordering (anime) only |
| **Plex** | optional | Sign in with Plex, then pick server + map Movies/Series/Anime libraries |
| **Seerr API key** | optional | auto-generate in Settings → Requests |
| **Web UI API key** | optional | no key = open instance (LAN). Set one (and/or a reverse proxy) before exposing it. |

## Architecture

- **Backend:** Go 1.26, [chi](https://github.com/go-chi/chi) router, modernc
  CGo-free SQLite (WAL), [goose](https://github.com/pressly/goose) migrations.
  Background workers: poller, importer, reconciler, healer, deleter, plus a
  persisted task runner for adopt/delete.
- **Frontend:** React + TypeScript + Vite SPA, embedded into the binary via
  `go:embed`.
- **Image:** multi-arch (amd64/arm64) distroless **nonroot** — `ghcr.io/radaiko/boxarr`.

## Development

```bash
go test ./...           # backend unit tests
go run ./cmd/boxarr     # serves :8080 (configure via env or the UI)

cd web
pnpm install
pnpm dev                # Vite dev server, proxies /api to :8080
pnpm lint               # tsc --noEmit
pnpm build              # emits web/dist (embedded by the Go build)
```

CI runs tests + frontend build + a multi-arch docker build; pushing a `vX.Y.Z`
tag publishes the image to GHCR and cuts a GitHub Release.

## Status

Active development (`0.0.x-dev`) — usable but pre-1.0; expect rough edges and
breaking changes.
