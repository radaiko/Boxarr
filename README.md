<div align="center">

<img src="docs/boxarr.svg" width="96" height="96" alt="Boxarr" />

# Boxarr

**A single, self-hosted media manager backed by [TorBox](https://torbox.app).**
Replaces sab2torbox + Sonarr + Radarr + a Seerr bridge with one small Go binary.

</div>

---

## What is Boxarr?

Boxarr manages your movies, series and anime end-to-end on top of TorBox: it
**searches** (via Prowlarr), **grabs** (TorBox usenet + torrents), **imports** by
writing a symlink straight to your Plex library, and **emulates Sonarr/Radarr v3**
so request apps like Seerr/Overseerr/Jellyseerr can request straight into it.

```
 Seerr ──requests──▶ Boxarr  (/sonarr, /radarr emulation)
                        │  search via Prowlarr → grab via TorBox
                        ▼
                     TorBox  ──WebDAV──▶ rclone FUSE mount (/mnt/torbox)
                        │
                        ▼  Boxarr writes a SYMLINK at the final Plex path
                     /mnt/library/{movies,tv,anime}/…  ──▶ Plex
```

Because import is a symlink to the file on the rclone mount, there's **no byte
copy, no rename, and no same-filesystem requirement** — the library can be a tiny
local directory. The only hard rule: **Plex must see `/mnt/torbox` at the same
absolute path** Boxarr does (the symlink targets are absolute).

## Highlights

- **Movies, Series & Anime** as first-class libraries. Anime is a series subtype
  with its own library root and Plex section; convert a series to/from anime and
  Boxarr relocates the files.
- **Seerr emulation** — exposes `/sonarr/api/v3` and `/radarr/api/v3` so request
  apps add straight into Boxarr (auto-generate the API key in Settings).
- **Sign in with Plex** (PIN OAuth) — pick your server and map libraries from a
  dropdown instead of pasting URLs, tokens and section IDs.
- **TorBox view** — account/usage stats plus a WebDAV mount browser grouped by
  title (Movies / Series / Anime / Unknown), with covers and tracked/unknown
  status. **Adopt** existing mount content into the library (search-and-pick the
  exact TMDB match) or **delete** it (single, multi-select, or a whole show).
- **Release selection** — a transparent weighted score with per-content-type
  **language rules** (e.g. German required + English preferred for movies/series;
  German *or* English for anime, preferring English subs).
- **Filterable search overlay** — per-item release search with language/subtitle
  columns and resolution/cached/subs filters; the currently-grabbed release is
  flagged.
- **Activity** — live download queue (with release details + ETA) and background
  adopt/delete tasks with progress and per-file detail.
- **Self-healing** — detects broken library symlinks and re-acquires them.
- **Everything is UI-settable** and applies live (no restart); env vars are an
  optional pre-seed.

## Quick start (Docker Compose)

A full reference stack (rclone + Boxarr + Seerr) lives in
[`deploy/docker-compose.yml`](deploy/docker-compose.yml). Minimal Boxarr service:

```yaml
services:
  boxarr:
    image: ghcr.io/radaiko/boxarr:latest   # or pin a tag, e.g. :0.0.29-dev
    container_name: boxarr
    restart: unless-stopped
    user: "1000:1000"                       # must own /config and the library dir
    environment:
      - BOXARR_DATABASE_PATH=/config/boxarr.db
      - BOXARR_LISTEN_ADDR=:8080
      - BOXARR_WEBDAV_MOUNT_ROOT=/mnt/torbox
      - BOXARR_MOVIE_LIBRARY_ROOT=/mnt/library/movies
      - BOXARR_TV_LIBRARY_ROOT=/mnt/library/tv
      - BOXARR_ANIME_LIBRARY_ROOT=/mnt/library/anime
    ports:
      - "8080:8080"
    volumes:
      - /mnt/appdata/boxarr:/config          # SQLite db (writable by the user above)
      - /mnt/library:/mnt/library            # symlink library (Plex mounts this too)
      - type: bind                           # the rclone TorBox mount, SAME path in Plex
        source: /mnt/torbox
        target: /mnt/torbox
        bind: { propagation: rslave }
```

Then open `http://<host>:8080` and finish setup in **Settings**.

### Configuration (all in the UI, or pre-seed via env)

| What | Required? | Notes |
|------|-----------|-------|
| **TorBox API token** | yes | torbox.app → Settings → API |
| **Prowlarr URL + API key** | yes | indexer search |
| **TMDB API Read Access Token (v4)** | yes | themoviedb.org → Settings → API. Boxarr queries TMDB directly (Sonarr/Radarr use a proxy; here you bring your own free token). |
| **TVDB** | optional | scene/absolute ordering (anime) only |
| **Plex** | optional | Sign in with Plex, then pick server + map Movies/Series/Anime libraries |
| **Seerr API key** | optional | auto-generate in Settings → Requests; point Seerr at `http://boxarr:8080/sonarr` and `…/radarr` |
| **Web UI API key** | optional | no key = open instance (LAN). Set one (and/or a reverse proxy) before exposing it. |

### Seerr setup

In Seerr → Settings → Services, add both with the Seerr API key from Boxarr:
- Sonarr → Server URL `http://boxarr:8080/sonarr`
- Radarr → Server URL `http://boxarr:8080/radarr`

## Architecture

- **Backend:** Go 1.26, [chi](https://github.com/go-chi/chi) router, modernc
  CGo-free SQLite (WAL), [goose](https://github.com/pressly/goose) migrations.
  Background workers: poller, importer, reconciler, healer, deleter, plus an
  in-process task runner for adopt/delete.
- **Frontend:** React + TypeScript + Vite SPA, embedded into the binary via
  `go:embed`.
- **Image:** multi-arch (amd64/arm64) distroless **nonroot** — `ghcr.io/radaiko/boxarr`.

## Development

```bash
# backend
go test ./...           # unit tests (race in CI)
go run ./cmd/boxarr     # serves :8080 (configure via env or the UI)

# frontend
cd web
pnpm install
pnpm dev                # Vite dev server, proxies /api to :8080
pnpm lint               # tsc --noEmit
pnpm build              # emits web/dist (embedded by the Go build)
```

CI runs tests + frontend build + a multi-arch docker build; pushing a `vX.Y.Z`
tag publishes the image to GHCR and cuts a GitHub Release.

## Status

Boxarr is in active development (`0.0.x-dev`). It's usable but pre-1.0 — expect
rough edges and breaking changes.

## License

See [LICENSE](LICENSE) if present; otherwise all rights reserved by the author.
