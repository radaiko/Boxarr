# Boxarr Phase 0a — Repo Evolution, Config & Schema Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn this repository (a copy of `sab2torbox`) into the Boxarr skeleton — module renamed, the SABnzbd emulation deleted, the `BOXARR_*` config superset in place, and the full SQLite schema (migrations `004`–`009`) plus its Go domain types and store methods landed — leaving a binary that builds, migrates a real DB, and passes `go test ./... -race` green.

**Architecture:** Evolve, don't rewrite (spec `00` Assumption A/C). The proven `internal/{torbox,store,job,worker}` packages are carried over with behavior unchanged (only import paths move); only the SAB HTTP surface is removed (Assumption B). All new persistence is additive goose migrations on top of the byte-for-byte `001`–`003`. This sub-plan does **no** networking and **no** HTTP routing changes beyond deleting SAB and keeping `/healthz` — clients (0b), the `/api/v1` chassis (0c), and the frontend/CI (0d) come next.

**Tech Stack:** Go 1.25, `modernc.org/sqlite` (CGo-free) + `pressly/goose/v3` embedded migrations, `kelseyhightower/envconfig`, `go-chi/chi/v5`. Tests use the stdlib `testing` package with a temp on-disk SQLite DB and `t.Setenv`.

**Authoritative specs (committed):** `docs/specs/00`–`09`. This plan implements `01 §2` (repo evolution), `02` (data model), `08 §1–§2` (config). Where a step shows abbreviated content, the cited spec section has the exhaustive verbatim version — but every step here contains enough to implement without guessing.

**Conventions for every task:**
- TDD: write the failing test first, watch it fail, implement minimally, watch it pass, commit.
- Go tests live beside the code as `*_test.go`. Run a single test with `go test ./internal/<pkg>/ -run <TestName> -v`; run everything with `go test ./... -race`.
- Commit after each task with the shown message. Never combine tasks in one commit.
- `gofmt -s -w .` before every commit; `go vet ./...` must be clean.

---

## Group 0 — Import the foundation

> This repo currently contains only `docs/`. It is **not** a git-fork of `sab2torbox` — the proven Go tree must be copied in before anything else. After this task the repo builds under the *old* `sab2torbox` module path; Task 1 renames it.

### Task 0: Import the `sab2torbox` source tree

**Files:**
- Create: `cmd/`, `internal/`, `go.mod`, `go.sum`, `.golangci.yml`, `.gitignore`, `LICENSE`, `.github/workflows/{ci,release}.yml`, `deploy/{Dockerfile,docker-compose.yml}` (copied from upstream)
- Do **not** copy: upstream `docs/` (Boxarr's own specs/plans live here already) or `README.md` (Boxarr's is written in 0d).

- [ ] **Step 1: Clone upstream to a temp dir**

```bash
tmp=$(mktemp -d)
gh repo clone radaiko/sab2torbox "$tmp/sab" -- --depth=1
```
Expected: clone succeeds; `$tmp/sab/go.mod` reads `module github.com/radaiko/sab2torbox`.

- [ ] **Step 2: Copy the source tree into this repo (excluding docs/ and README)**

```bash
cp -R "$tmp/sab/cmd" "$tmp/sab/internal" ./
cp "$tmp/sab/go.mod" "$tmp/sab/go.sum" "$tmp/sab/.golangci.yml" "$tmp/sab/.gitignore" "$tmp/sab/LICENSE" ./
mkdir -p .github/workflows deploy
cp "$tmp/sab/.github/workflows/"*.yml .github/workflows/
cp "$tmp/sab/deploy/"* deploy/
```

- [ ] **Step 3: Verify it builds and tests green as-is (old module path)**

Run: `go build ./... && go test ./... -race`
Expected: green — the imported tree is internally consistent under `github.com/radaiko/sab2torbox`. (`gofmt -s -l .` should already be clean.)

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "chore: import sab2torbox source tree as the Boxarr foundation"
```

---

## Group A — Repo evolution (carry-over behavior unchanged, SAB removed)

### Task 1: Rename the Go module and all imports

**Files:**
- Modify: `go.mod` (module path + promote `chi` to direct)
- Modify: every `*.go` import of `github.com/radaiko/sab2torbox`

- [ ] **Step 1: Confirm the starting point**

Run: `head -3 go.mod && grep -rl 'github.com/radaiko/sab2torbox' --include='*.go' . | wc -l`
Expected: `module github.com/radaiko/sab2torbox`, `go 1.25.7`, and a non-zero count of files importing the old path.

- [ ] **Step 2: Rewrite the module path and every import**

```bash
go mod edit -module github.com/radaiko/boxarr
grep -rl 'github.com/radaiko/sab2torbox' --include='*.go' . \
  | xargs sed -i '' 's#github.com/radaiko/sab2torbox#github.com/radaiko/boxarr#g'
```
(On GNU sed drop the `''` after `-i`.)

- [ ] **Step 3: Promote `chi` to a direct require and tidy**

Run:
```bash
go get github.com/go-chi/chi/v5@v5.2.5
go mod tidy
```
Expected: `go.mod` now lists `github.com/go-chi/chi/v5 v5.2.5` in the direct `require` block (no `// indirect`).

- [ ] **Step 4: Verify the tree still builds and tests pass**

Run: `gofmt -s -w . && go vet ./... && go build ./... && go test ./... -race`
Expected: build succeeds; all existing tests pass (they still cover the SAB surface — that's fine, it's removed in Task 4); `grep -r 'github.com/radaiko/sab2torbox' --include='*.go' .` returns nothing.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor: rename module github.com/radaiko/sab2torbox -> boxarr"
```

---

### Task 2: Rename the command, binary, and DB default

**Files:**
- Rename: `cmd/sab2torbox/` → `cmd/boxarr/`
- Modify: `cmd/boxarr/main.go` (healthcheck env var, startup log strings)
- Modify: `internal/config/config.go:22` (`DatabasePath` default)

- [ ] **Step 1: Move the command package**

```bash
git mv cmd/sab2torbox cmd/boxarr
```

- [ ] **Step 2: Repoint the healthcheck env var (write the failing expectation as a build check)**

In `cmd/boxarr/main.go`, change `runHealthcheck()` to read `BOXARR_LISTEN_ADDR` (was `SAB2TORBOX_LISTEN_ADDR`):

```go
func runHealthcheck() int {
	addr := os.Getenv("BOXARR_LISTEN_ADDR") // CHANGED from SAB2TORBOX_LISTEN_ADDR
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
	_ = resp.Body.Close()
	return 0
}
```

- [ ] **Step 3: Update startup log identity strings**

In `cmd/boxarr/main.go` `run()`, change the two log lines `"sab2torbox started"` / `"sab2torbox stopped"` to `"boxarr started"` / `"boxarr stopped"`.

- [ ] **Step 4: Change the DB default**

In `internal/config/config.go`, change the `DatabasePath` tag default `/config/sab2torbox.db` → `/config/boxarr.db`:

```go
DatabasePath string `envconfig:"DATABASE_PATH" default:"/config/boxarr.db"`
```

- [ ] **Step 5: Verify build + tests**

Run: `gofmt -s -w . && go build ./... && go test ./... -race`
Expected: green.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor: rename cmd to boxarr, BOXARR_LISTEN_ADDR healthcheck, /config/boxarr.db default"
```

---

### Task 3: Switch the env prefix to `boxarr`

**Files:**
- Modify: `internal/config/config.go` (the `envconfig.Process` call)
- Modify: `internal/config/config_test.go` (existing tests set `SAB2TORBOX_*`)

- [ ] **Step 1: Update the existing config test to the new prefix (failing test first)**

In `internal/config/config_test.go`, change every `t.Setenv("SAB2TORBOX_...", ...)` to `t.Setenv("BOXARR_...", ...)`.

Run: `go test ./internal/config/ -v`
Expected: FAIL — `envconfig` still reads the `sab2torbox` prefix, so required vars are now unset.

- [ ] **Step 2: Flip the prefix**

In `internal/config/config.go` `Load()`:

```go
if err := envconfig.Process("boxarr", &c); err != nil {
	return nil, fmt.Errorf("processing env config: %w", err)
}
```

- [ ] **Step 3: Verify the config tests pass**

Run: `go test ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "refactor: envconfig prefix sab2torbox -> boxarr (BOXARR_* vars)"
```

---

### Task 4: Delete the SABnzbd emulation surface

**Files:**
- Delete: `internal/api/responses.go`, `internal/api/responses_test.go`
- Modify: `internal/api/handlers.go` (remove `handleAPI`, the `/api`+`/sabnzbd/api` route loop, all `mode` handlers + SAB helpers; keep `Server`, `Router`, `/healthz`, `writeJSON`, `param`, `validAPIKey`)
- Modify: `internal/api/handlers_test.go` (remove SAB-mode tests; keep `/healthz` test)
- Modify: `internal/config/config.go` (remove `SABAPIKey` field)
- Modify: `internal/config/config_test.go` (remove `SAB_API_KEY` from env setup)

- [ ] **Step 1: Reduce the router to `/healthz` only (write the new test first)**

Replace the SAB-mode cases in `internal/api/handlers_test.go` with a router-surface test:

```go
func TestRouter_DropsSABSurface(t *testing.T) {
	s := NewServer(testStore(t), testConfig(t), slog.Default())
	r := s.Router()
	for _, path := range []string{"/api", "/sabnzbd/api"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: want 404, got %d", path, rec.Code)
		}
	}
	// /healthz survives
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/healthz: want 200, got %d", rec.Code)
	}
}
```
(`testStore`/`testConfig` helpers: open an in-temp-dir store and a minimal valid `*config.Config` — copy the construction the existing handlers_test already uses.)

Run: `go test ./internal/api/ -run TestRouter_DropsSABSurface -v`
Expected: FAIL to compile or `/api` returns 200 (routes still present).

- [ ] **Step 2: Delete the SAB response file and its test**

```bash
git rm internal/api/responses.go internal/api/responses_test.go
```

- [ ] **Step 3: Strip the SAB surface from `handlers.go`**

In `internal/api/handlers.go`: delete `handleAPI` and every SAB helper (`handleAddURL`, `handleAddFile`, `handleQueue`, `handleHistory`, `handleDelete`, `parseNzoID`, `formatTimeLeft`, `queueStatusLabel`, the `maxNZBSize` const). Replace the `Router()` body so only `/healthz` (and the existing heal endpoints, which 0c will reshape) remain — for 0a keep it minimal:

```go
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", s.handleHealthz)
	return r
}
```
Keep `Server`, `NewServer`, `SetHealth`, `SetHealReporter`, `Checker`, `HealReporter`, `handleHealthz`, `writeJSON`, `param`, and `validAPIKey` (`subtle.ConstantTimeCompare`) — they are reused by 0c.

- [ ] **Step 4: Remove `SABAPIKey` from config**

In `internal/config/config.go` delete the field `SABAPIKey string \`envconfig:"SAB_API_KEY" required:"true"\``. In `internal/config/config_test.go` remove any `t.Setenv("BOXARR_SAB_API_KEY", ...)`.

- [ ] **Step 5: Verify**

Run: `gofmt -s -w . && go vet ./... && go test ./... -race`
Expected: PASS, including `TestRouter_DropsSABSurface`. Confirm no leftovers: `grep -rn 'SAB_API_KEY\|sabnzbd\|VersionResponse\|AddResponse\|QueueResponse' --include='*.go' .` returns nothing.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat!: drop SABnzbd download-client emulation surface (00 Assumption B)"
```

---

## Group B — Config superset (`BOXARR_*`)

### Task 5: Extend the config struct, helpers, and validation

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: Write failing tests for the new surface**

Add to `internal/config/config_test.go`:

```go
func TestLoad_NewRequiredAndDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BOXARR_TORBOX_API_TOKEN", "tok")
	t.Setenv("BOXARR_WEBDAV_MOUNT_ROOT", dir)
	t.Setenv("BOXARR_MOVIE_LIBRARY_ROOT", dir)
	t.Setenv("BOXARR_TV_LIBRARY_ROOT", dir)
	t.Setenv("BOXARR_PROWLARR_URL", "http://prowlarr:9696")
	t.Setenv("BOXARR_PROWLARR_API_KEY", "pk")
	t.Setenv("BOXARR_TMDB_API_KEY", "tk")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.ReconcileInterval.String() != "15m0s" {
		t.Errorf("ReconcileInterval default = %s, want 15m", c.ReconcileInterval)
	}
	if c.MetadataInterval.String() != "24h0m0s" {
		t.Errorf("MetadataInterval default = %s, want 24h", c.MetadataInterval)
	}
	if c.TorrentPath() != c.WebDAVMountRoot { // empty subpath -> flat root
		t.Errorf("TorrentPath = %q, want %q", c.TorrentPath(), c.WebDAVMountRoot)
	}
	if c.PlexEnabled() { // no URL/token set
		t.Error("PlexEnabled should be false without URL+token")
	}
}

func TestLoad_MissingRequiredFails(t *testing.T) {
	t.Setenv("BOXARR_TORBOX_API_TOKEN", "tok")
	// deliberately omit PROWLARR_URL/PROWLARR_API_KEY/TMDB_API_KEY + roots
	if _, err := Load(); err == nil {
		t.Fatal("Load should fail when required vars are unset")
	}
}
```

Run: `go test ./internal/config/ -run TestLoad_ -v`
Expected: FAIL (fields/helpers don't exist yet).

- [ ] **Step 2: Replace the config struct with the `BOXARR_*` superset**

In `internal/config/config.go`, set the `Config` struct to (full verbatim version: `08 §2.1`):

```go
type Config struct {
	// Core (kept)
	TorBoxAPIToken      string        `envconfig:"TORBOX_API_TOKEN" required:"true"`
	WebDAVMountRoot     string        `envconfig:"WEBDAV_MOUNT_ROOT" required:"true"`
	WebDAVUsenetSubpath string        `envconfig:"WEBDAV_USENET_SUBPATH"`
	ListenAddr          string        `envconfig:"LISTEN_ADDR" default:":8080"`
	APIKey              string        `envconfig:"API_KEY"`
	DatabasePath        string        `envconfig:"DATABASE_PATH" default:"/config/boxarr.db"`
	PollInterval        time.Duration `envconfig:"POLL_INTERVAL" default:"1m"`
	LogLevel            string        `envconfig:"LOG_LEVEL" default:"info"`

	// TorBox WebDAV force-refresh (kept)
	TorBoxWebDAVUser       string        `envconfig:"TORBOX_WEBDAV_USER"`
	TorBoxWebDAVPass       string        `envconfig:"TORBOX_WEBDAV_PASS"`
	TorBoxWebDAVRefreshURL string        `envconfig:"TORBOX_WEBDAV_REFRESH_URL" default:"https://webdav.torbox.app/refresh"`
	WebDAVRefreshCooldown  time.Duration `envconfig:"TORBOX_WEBDAV_REFRESH_COOLDOWN" default:"2m"`

	// Torrent WebDAV path (new)
	WebDAVTorrentSubpath string `envconfig:"WEBDAV_TORRENT_SUBPATH"`

	// Indexers / metadata / playback (new)
	ProwlarrURL      string `envconfig:"PROWLARR_URL" required:"true"`
	ProwlarrAPIKey   string `envconfig:"PROWLARR_API_KEY" required:"true"`
	TMDBAPIKey       string `envconfig:"TMDB_API_KEY" required:"true"`
	TVDBAPIKey       string `envconfig:"TVDB_API_KEY"`
	TVDBPin          string `envconfig:"TVDB_PIN"`
	PlexURL          string `envconfig:"PLEX_URL"`
	PlexToken        string `envconfig:"PLEX_TOKEN"`
	PlexMovieSection string `envconfig:"PLEX_MOVIE_SECTION"`
	PlexTVSection    string `envconfig:"PLEX_TV_SECTION"`

	// Seerr emulation inbound keys (new)
	SeerrAPIKeys []string `envconfig:"SEERR_API_KEYS"`

	// Library roots (new — replace SYMLINK_ROOT)
	MovieLibraryRoot string `envconfig:"MOVIE_LIBRARY_ROOT" default:"/data/movies"`
	TVLibraryRoot    string `envconfig:"TV_LIBRARY_ROOT" default:"/data/tv"`

	// Same-path escape hatch (new)
	HostToPlexPathPrefix string `envconfig:"HOST_TO_PLEX_PATH_PREFIX"`

	// Intervals (new)
	ReconcileInterval time.Duration `envconfig:"RECONCILE_INTERVAL" default:"15m"`
	MetadataInterval  time.Duration `envconfig:"METADATA_REFRESH_INTERVAL" default:"24h"`
	SearchInterval    time.Duration `envconfig:"SEARCH_INTERVAL" default:"6h"`

	// Selection score knobs (new — full set; algorithm in 06 §3)
	SelectAllowedResolutions   []string `envconfig:"SELECT_ALLOWED_RESOLUTIONS"`
	SelectMinSize              int64    `envconfig:"SELECT_MIN_SIZE" default:"0"`
	SelectMaxSize              int64    `envconfig:"SELECT_MAX_SIZE" default:"0"`
	SelectSizeLimits           string   `envconfig:"SELECT_SIZE_LIMITS" default:"{}"`
	SelectMinSeeders           int      `envconfig:"SELECT_MIN_SEEDERS" default:"1"`
	SelectMinGrabs             int      `envconfig:"SELECT_MIN_GRABS" default:"0"`
	SelectRequireCached        bool     `envconfig:"SELECT_REQUIRE_CACHED" default:"false"`
	SelectBlockedGroups        []string `envconfig:"SELECT_BLOCKED_GROUPS"`
	SelectBlockedKeywords      []string `envconfig:"SELECT_BLOCKED_KEYWORDS"`
	SelectMinScore             int      `envconfig:"SELECT_MIN_SCORE" default:"0"`
	SelectPreferredResolutions []string `envconfig:"SELECT_PREFERRED_RESOLUTIONS" default:"2160p,1080p,720p"`
	SelectPreferredQualities   []string `envconfig:"SELECT_PREFERRED_QUALITIES" default:"WEB-DL,BluRay,WEBRip,HDTV"`
	SelectPreferredGroups      []string `envconfig:"SELECT_PREFERRED_GROUPS"`
	SelectPreferredKeywords    []string `envconfig:"SELECT_PREFERRED_KEYWORDS"`
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

	// Limit knobs (new)
	MaxActiveDownloads int `envconfig:"MAX_ACTIVE_DOWNLOADS" default:"0"`
	MaxCreatePerHour   int `envconfig:"MAX_CREATE_PER_HOUR" default:"60"`
	MaxTorrentPerMin   int `envconfig:"MAX_TORRENT_PER_MIN" default:"300"`
	SearchConcurrency  int `envconfig:"SEARCH_CONCURRENCY" default:"3"`

	// Auto-heal (kept + new fallback)
	HealEnabled          bool          `envconfig:"HEAL_ENABLED" default:"false"`
	HealInterval         time.Duration `envconfig:"HEAL_INTERVAL" default:"1h"`
	HealLibraryRoots     []string      `envconfig:"HEAL_LIBRARY_ROOTS"`
	HealDryRun           bool          `envconfig:"HEAL_DRY_RUN" default:"false"`
	HealMaxAttempts      int           `envconfig:"HEAL_MAX_ATTEMPTS" default:"3"`
	HealBackoffInitial   time.Duration `envconfig:"HEAL_BACKOFF_INITIAL" default:"5m"`
	HealProwlarrFallback bool          `envconfig:"HEAL_PROWLARR_FALLBACK" default:"true"`
	HealWebhookURL       string        `envconfig:"HEAL_WEBHOOK_URL"`
	HealWebhookEvents    []string      `envconfig:"HEAL_WEBHOOK_EVENTS" default:"failed"`

	// Plex scan tuning (new)
	PlexScanTimeout time.Duration `envconfig:"PLEX_SCAN_TIMEOUT" default:"60s"`
}
```

- [ ] **Step 3: Update `Load()` validation and helper methods**

Replace the old `SymlinkRoot` validation with movie+TV library-root validation, keep the WebDAV-mount and heal validation, and add helpers:

```go
func Load() (*Config, error) {
	var c Config
	if err := envconfig.Process("boxarr", &c); err != nil {
		return nil, fmt.Errorf("processing env config: %w", err)
	}
	if err := mustDir(c.WebDAVMountRoot, "webdav mount root"); err != nil {
		return nil, err
	}
	if err := mustDir(c.MovieLibraryRoot, "movie library root"); err != nil {
		return nil, err
	}
	if err := mustDir(c.TVLibraryRoot, "tv library root"); err != nil {
		return nil, err
	}
	if c.HealEnabled {
		if len(c.HealLibraryRoots) == 0 {
			return nil, fmt.Errorf("HEAL_ENABLED requires HEAL_LIBRARY_ROOTS")
		}
		for _, r := range c.HealLibraryRoots {
			if err := mustDir(r, "heal library root"); err != nil {
				return nil, err
			}
		}
		if c.HealMaxAttempts <= 0 {
			return nil, fmt.Errorf("HEAL_MAX_ATTEMPTS must be > 0")
		}
	}
	return &c, nil
}

func mustDir(p, what string) error {
	info, err := os.Stat(p)
	if err != nil {
		return fmt.Errorf("%s %q: %w", what, p, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s %q is not a directory", what, p)
	}
	return nil
}

func (c *Config) UsenetPath() string  { return filepath.Join(c.WebDAVMountRoot, c.WebDAVUsenetSubpath) }
func (c *Config) TorrentPath() string { return filepath.Join(c.WebDAVMountRoot, c.WebDAVTorrentSubpath) }
func (c *Config) WebDAVRefreshEnabled() bool { return c.TorBoxWebDAVUser != "" && c.TorBoxWebDAVPass != "" }
func (c *Config) PlexEnabled() bool   { return c.PlexURL != "" && c.PlexToken != "" }
func (c *Config) TVDBEnabled() bool   { return c.TVDBAPIKey != "" }
func (c *Config) SeerrEnabled() bool  { return len(c.SeerrAPIKeys) > 0 }
```
Keep the existing `SlogLevel()` method unchanged. Remove the now-unused `AllowsCategory`/`Categories` and `validateMount` if present (Categories is dropped — `08 §2.1`).

- [ ] **Step 4: Verify**

Run: `gofmt -s -w . && go test ./internal/config/ -v`
Expected: PASS (`TestLoad_NewRequiredAndDefaults`, `TestLoad_MissingRequiredFails`, and the pre-existing tests).

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat(config): BOXARR_* superset (prowlarr/tmdb/tvdb/plex/seerr/library/intervals/selection/limits) + helpers"
```

---

## Group C — Schema, domain types, and store methods

### Task 6: Add `StateSeeding` to the job state machine

**Files:**
- Modify: `internal/job/job.go`
- Test: `internal/job/job_test.go`

- [ ] **Step 1: Write the failing transition test**

Add to `internal/job/job_test.go`:

```go
func TestStateSeedingTransitions(t *testing.T) {
	if !StateDownloading.CanTransitionTo(StateSeeding) {
		t.Error("downloading -> seeding should be allowed")
	}
	if !StateSeeding.CanTransitionTo(StateCompleted) {
		t.Error("seeding -> completed should be allowed")
	}
	if !StateSeeding.CanTransitionTo(StateFailed) {
		t.Error("seeding -> failed should be allowed")
	}
	if StateSeeding.IsTerminal() {
		t.Error("seeding must not be terminal")
	}
}
```

Run: `go test ./internal/job/ -run TestStateSeedingTransitions -v`
Expected: FAIL (`StateSeeding` undefined).

- [ ] **Step 2: Add the constant and transitions**

In `internal/job/job.go` add the constant `StateSeeding State = "seeding"` and update the `transitions` map: add `downloading -> [seeding, completed, failed]` and `seeding -> [completed, failed]`.

- [ ] **Step 3: Verify**

Run: `go test ./internal/job/ -race -v`
Expected: PASS (new + existing).

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat(job): add StateSeeding (torrent upload phase) to the Go state machine"
```

---

### Task 7: Migration 004 — torrent + media columns on `jobs`, extended `Job` + store finders

**Files:**
- Create: `internal/store/migrations/004_protocol_media.sql`
- Modify: `internal/job/job.go` (`Job` struct + new fields)
- Modify: `internal/store/store.go` (`jobColumns`, `CreateJob`, `UpdateJob`, scan; add `FindByTorrentHash`, `FindJobByMedia`)
- Test: `internal/store/store_test.go`

- [ ] **Step 1: Write the failing dedup/roundtrip test**

Add to `internal/store/store_test.go`:

```go
func TestTorrentJobRoundtripAndDedup(t *testing.T) {
	st := openTestStore(t) // existing helper that opens a temp DB + migrates
	ctx := context.Background()
	j := &job.Job{
		State: job.StatePending, Category: "movie", NZBName: "X",
		Protocol: "torrent", TorrentHash: "abc123", TorrentMagnet: "magnet:?xt=urn:btih:abc123",
		MediaType: "movie", MediaRef: 7,
	}
	id, err := st.CreateJob(ctx, j)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetJob(ctx, id)
	if err != nil || got.Protocol != "torrent" || got.TorrentHash != "abc123" || got.MediaRef != 7 {
		t.Fatalf("roundtrip mismatch: %+v err=%v", got, err)
	}
	dup, err := st.FindByTorrentHash(ctx, "abc123", "movie")
	if err != nil || dup == nil || dup.ID != id {
		t.Fatalf("FindByTorrentHash should return the existing job, got %+v", dup)
	}
	none, _ := st.FindByTorrentHash(ctx, "abc123", "series") // category-scoped
	if none != nil {
		t.Fatal("dedup must be category-scoped")
	}
}
```

Run: `go test ./internal/store/ -run TestTorrentJobRoundtripAndDedup -v`
Expected: FAIL (fields/migration/method missing).

- [ ] **Step 2: Write the migration (verbatim `02 §3.1`)**

Create `internal/store/migrations/004_protocol_media.sql`:

```sql
-- +goose Up
ALTER TABLE jobs ADD COLUMN protocol TEXT NOT NULL DEFAULT 'usenet';
ALTER TABLE jobs ADD COLUMN media_type TEXT;
ALTER TABLE jobs ADD COLUMN media_ref INTEGER;
ALTER TABLE jobs ADD COLUMN torrent_magnet TEXT;
ALTER TABLE jobs ADD COLUMN torrent_hash TEXT;
ALTER TABLE jobs ADD COLUMN torrent_file BLOB;

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

- [ ] **Step 3: Extend the `Job` struct**

In `internal/job/job.go`, append to `Job` (keep all existing fields): `Protocol string`, `MediaType string`, `MediaRef int64`, `TorrentMagnet string`, `TorrentHash string`, `TorrentFile []byte`.

- [ ] **Step 4: Extend the store**

In `internal/store/store.go`:
- Append the six columns to the `jobColumns` constant (append-only, after `last_heal_error`): `, protocol, media_type, media_ref, torrent_magnet, torrent_hash, torrent_file`.
- Extend the `scanJob` (or inline scan) to read the six new columns (`protocol` plain string; `media_type`/`torrent_magnet`/`torrent_hash` via `nullStr`; `media_ref` via `nullInt`; `torrent_file` via `[]byte`).
- Extend `CreateJob` INSERT to set the new columns:
  ```sql
  INSERT INTO jobs (state, category, nzb_name, nzb_content, nzb_url, nzb_sha256,
    protocol, media_type, media_ref, torrent_magnet, torrent_hash, torrent_file)
  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
  ```
  (default `protocol` to `"usenet"` when `j.Protocol == ""`).
- Extend `UpdateJob` SET clause to rewrite the six new columns.
- Add finders mirroring `FindBySHA256`:
  ```go
  func (s *Store) FindByTorrentHash(ctx context.Context, hash, category string) (*job.Job, error) {
  	return s.findOne(ctx, "torrent_hash=? AND category=?", hash, category)
  }
  func (s *Store) FindJobByMedia(ctx context.Context, mediaType string, mediaRef int64) (*job.Job, error) {
  	return s.findOne(ctx, "media_type=? AND media_ref=?", mediaType, mediaRef)
  }
  ```

- [ ] **Step 5: Verify roundtrip + back-fill on an existing DB**

Run: `go test ./internal/store/ -race -v`
Expected: PASS. Add/confirm a test asserting a pre-004 `jobs` row reads back `Protocol == "usenet"` after migration (the `NOT NULL DEFAULT 'usenet'` back-fill) — `openTestStore` starts empty so insert a row via raw SQL before re-migrate, or assert the default on a freshly inserted row with `Protocol` unset.

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat(store): migration 004 torrent+media columns; FindByTorrentHash/FindJobByMedia"
```

---

### Task 8: Migration 005 — catalog tables + `internal/media` types + catalog store methods

**Files:**
- Create: `internal/store/migrations/005_catalog.sql`
- Create: `internal/media/types.go` (`MediaStatus`, `Series`, `Season`, `Episode`, `Movie`)
- Modify: `internal/store/store.go` (catalog CRUD + wanted queries)
- Test: `internal/store/store_catalog_test.go`

- [ ] **Step 1: Write the failing catalog test**

Create `internal/store/store_catalog_test.go`:

```go
func TestCatalogCRUDAndWanted(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	sid, err := st.CreateSeries(ctx, &media.Series{TMDBID: 1399, Title: "GoT", Year: 2011, Monitored: true})
	if err != nil { t.Fatal(err) }
	if _, err := st.GetSeriesByTMDB(ctx, 1399); err != nil { t.Fatalf("GetSeriesByTMDB: %v", err) }

	seasonID, err := st.UpsertSeason(ctx, &media.Season{SeriesID: sid, SeasonNumber: 1, Monitored: true, EpisodeCount: 2})
	if err != nil { t.Fatal(err) }
	aired := &media.Episode{SeriesID: sid, SeasonID: seasonID, SeasonNumber: 1, EpisodeNumber: 1,
		AirDate: "2011-04-17", Monitored: true, Status: media.MediaWanted}
	unaired := &media.Episode{SeriesID: sid, SeasonID: seasonID, SeasonNumber: 1, EpisodeNumber: 2,
		AirDate: "2099-01-01", Monitored: true, Status: media.MediaWanted}
	if _, err := st.UpsertEpisode(ctx, aired); err != nil { t.Fatal(err) }
	if _, err := st.UpsertEpisode(ctx, unaired); err != nil { t.Fatal(err) }

	wanted, err := st.WantedEpisodes(ctx)
	if err != nil { t.Fatal(err) }
	if len(wanted) != 1 || wanted[0].EpisodeNumber != 1 {
		t.Fatalf("WantedEpisodes should return only the aired episode, got %d", len(wanted))
	}
	// CASCADE: deleting the series removes seasons+episodes
	if err := st.DeleteSeries(ctx, sid); err != nil { t.Fatal(err) }
	if eps, _ := st.ListEpisodes(ctx, sid); len(eps) != 0 {
		t.Fatal("DeleteSeries should cascade to episodes")
	}
}
```

Run: `go test ./internal/store/ -run TestCatalogCRUDAndWanted -v`
Expected: FAIL (types/migration/methods missing).

- [ ] **Step 2: Write migration 005 (verbatim `02 §3.2` — series/season/episode/movie with FKs + indexes)**

Create `internal/store/migrations/005_catalog.sql` with the four `CREATE TABLE` statements and their indexes exactly as in `docs/specs/02-data-model.md §3.2` (series, season, episode, movie; `ON DELETE CASCADE` on `season.series_id`, `episode.series_id`, `episode.season_id`; `ON DELETE SET NULL` on `episode.job_id`/`movie.job_id`; `UNIQUE` on `series.tmdb_id`, `movie.tmdb_id`, `season(series_id,season_number)`, `episode(series_id,season_number,episode_number)`), and a `-- +goose Down` dropping `movie, episode, season, series` in that order.

- [ ] **Step 3: Create `internal/media/types.go`**

Define `type MediaStatus string` with the six constants (`MediaWanted="wanted"`, `MediaSearching="searching"`, `MediaDownloading="downloading"`, `MediaAvailable="available"`, `MediaMissing="missing"`, `MediaExpired="expired_broken"`) and the `Series`, `Season`, `Episode`, `Movie` structs exactly as in `docs/specs/02-data-model.md §4.2` (field-for-field; `*time.Time` for nullable timestamps, plain zero-values for nullable strings/ints).

- [ ] **Step 4: Implement catalog store methods**

In `internal/store/store.go` (or a new `store_catalog.go`), implement, following the existing `jobColumns`/`findOne`/`nullStr` patterns and `SetMaxOpenConns(1)`: `CreateSeries`, `GetSeries`, `GetSeriesByTMDB`, `GetSeriesByTVDB`, `UpdateSeries`, `ListSeries` (ORDER BY `sort_title`), `DeleteSeries`; `UpsertSeason` (`ON CONFLICT(series_id,season_number) DO UPDATE`), `ListSeasons`, `SetSeasonMonitored`; `UpsertEpisode` (`ON CONFLICT(series_id,season_number,episode_number) DO UPDATE` that does **not** clobber `status`/`has_file`/`job_id`/`library_path`), `GetEpisode`, `ListEpisodes`, `UpdateEpisode`, `SetEpisodeStatus`, `WantedEpisodes`; `CreateMovie`, `GetMovie`, `GetMovieByTMDB`, `UpdateMovie`, `ListMovies`, `DeleteMovie`, `SetMovieStatus`, `WantedMovies`. The two wanted queries use the exact air-date-aware SQL in `docs/specs/02-data-model.md §5.2` (`... AND air_date <> '' AND air_date <= date('now')`).

- [ ] **Step 5: Verify**

Run: `go test ./internal/store/ -race -v`
Expected: PASS including cascade + air-date-aware wanted.

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat(store): migration 005 catalog (series/season/episode/movie) + internal/media + CRUD"
```

---

### Task 9: Migration 006 — notifications + `internal/notify` + store

**Files:**
- Create: `internal/store/migrations/006_notifications.sql`
- Create: `internal/notify/types.go` (`Notification`)
- Modify: `internal/store/store.go` (notification methods)
- Test: `internal/store/store_notify_test.go`

- [ ] **Step 1: Failing test**

```go
func TestNotificationsQueueAndRead(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if _, err := st.EnqueueNotification(ctx, &notify.Notification{Type: "grab_failed", Payload: `{"x":1}`}); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.UnreadCount(ctx); n != 1 {
		t.Fatalf("UnreadCount = %d, want 1", n)
	}
	list, _ := st.ListNotifications(ctx, true, 50)
	if len(list) != 1 { t.Fatalf("want 1 unread, got %d", len(list)) }
	if err := st.MarkNotificationRead(ctx, list[0].ID); err != nil { t.Fatal(err) }
	if n, _ := st.UnreadCount(ctx); n != 0 { t.Fatalf("UnreadCount after read = %d, want 0", n) }
}
```

Run: `go test ./internal/store/ -run TestNotificationsQueueAndRead -v` → FAIL.

- [ ] **Step 2: Migration 006 (verbatim `02 §3.3`)** — `notification` table + `idx_notification_read_created` + `idx_notification_job`; `-- +goose Down` drops the table.

- [ ] **Step 3: `internal/notify/types.go`** — the `Notification` struct (`02 §4.3`).

- [ ] **Step 4: Store methods** — `EnqueueNotification`, `ListNotifications(unreadOnly bool, limit int)` (`ORDER BY created_at DESC, id DESC`), `UnreadCount`, `MarkNotificationRead`, `MarkAllNotificationsRead` (`02 §5.3`).

- [ ] **Step 5: Verify** — `go test ./internal/store/ -race -v` → PASS.

- [ ] **Step 6: Commit** — `git add -A && git commit -m "feat(store): migration 006 notifications + internal/notify + store"`

---

### Task 10: Migration 007 — webdav_items + `internal/webdav` types + store

**Files:**
- Create: `internal/store/migrations/007_webdav_items.sql`
- Create: `internal/webdav/types.go` (`WebDAVItem`)
- Modify: `internal/store/store.go`
- Test: `internal/store/store_webdav_test.go`

- [ ] **Step 1: Failing test**

```go
func TestWebDAVUpsertAndUsage(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	w := &webdav.WebDAVItem{Name: "X", RemotePath: "/mnt/torbox/X", Size: 100, Category: "movie", Known: true}
	if err := st.UpsertWebDAVItem(ctx, w); err != nil { t.Fatal(err) }
	w.Size = 250 // re-seen, bigger
	if err := st.UpsertWebDAVItem(ctx, w); err != nil { t.Fatal(err) } // ON CONFLICT(remote_path)
	if got, _ := st.WebDAVUsageBytes(ctx); got != 250 {
		t.Fatalf("usage = %d, want 250 (upsert, not duplicate)", got)
	}
}
```

Run → FAIL.

- [ ] **Step 2: Migration 007 (verbatim `02 §3.4`)** — `webdav_item` with `remote_path TEXT NOT NULL UNIQUE` + indexes; Down drops it.

- [ ] **Step 3: `internal/webdav/types.go`** — `WebDAVItem` (`02 §4.3`).

- [ ] **Step 4: Store methods** — `UpsertWebDAVItem` (`ON CONFLICT(remote_path) DO UPDATE ... last_seen=CURRENT_TIMESTAMP, is_broken=0`), `ListWebDAVItems`, `ListUnknownWebDAVItems`, `MarkWebDAVItemsBrokenNotSeenSince`, `WebDAVUsageBytes` (`SUM(size) WHERE is_broken=0`) (`02 §5.4`).

- [ ] **Step 5: Verify** → PASS.

- [ ] **Step 6: Commit** — `git add -A && git commit -m "feat(store): migration 007 webdav_items + internal/webdav + store"`

---

### Task 11: Migrations 008 + 009 — settings KV and seeded servarr tables

**Files:**
- Create: `internal/store/migrations/008_settings.sql`, `internal/store/migrations/009_servarr.sql`
- Create: `internal/servarr/types.go` (`QualityProfile`, `RootFolder`, `Tag`)
- Modify: `internal/store/store.go`
- Test: `internal/store/store_settings_test.go`

- [ ] **Step 1: Failing test**

```go
func TestSettingsKVAndServarrSeed(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if err := st.SetSetting(ctx, "prowlarr.url", "http://x:9696"); err != nil { t.Fatal(err) }
	if v, ok, _ := st.GetSetting(ctx, "prowlarr.url"); !ok || v != "http://x:9696" {
		t.Fatalf("GetSetting = %q,%v", v, ok)
	}
	profs, _ := st.ListQualityProfiles(ctx)
	if len(profs) != 1 || profs[0].ID != 1 || profs[0].Name != "Any" {
		t.Fatalf("expected seeded profile id=1 'Any', got %+v", profs)
	}
	tvRoots, _ := st.ListRootFolders(ctx, "tv")
	if len(tvRoots) != 1 || tvRoots[0].ID != 1 {
		t.Fatalf("expected seeded tv root id=1, got %+v", tvRoots)
	}
	movieRoots, _ := st.ListRootFolders(ctx, "movie")
	if len(movieRoots) != 1 || movieRoots[0].ID != 2 {
		t.Fatalf("expected seeded movie root id=2, got %+v", movieRoots)
	}
}
```

Run → FAIL.

- [ ] **Step 2: Migration 008 (verbatim `02 §3.5`)** — `settings(key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '', updated_at)`.

- [ ] **Step 3: Migration 009 (verbatim `02 §3.6`)** — `quality_profile`, `root_folder` (`path UNIQUE`, `media_kind`), `tag` tables; seed `INSERT`s for profile id 1 `'Any'`, root id 1 `('/data/tv','tv')`, root id 2 `('/data/movies','movie')`; Down drops `tag, root_folder, quality_profile`.

- [ ] **Step 4: `internal/servarr/types.go`** — `QualityProfile`, `RootFolder`, `Tag` (`02 §4.3`).

- [ ] **Step 5: Store methods** — `GetSetting`, `SetSetting` (`ON CONFLICT(key) DO UPDATE`), `AllSettings`; `ListQualityProfiles`, `ListRootFolders(kind string)` (empty kind = all), `ListTags`, `UpsertRootFolder(id int64, path, kind string)` (`ON CONFLICT(id) DO UPDATE`), `CreateTag` (`02 §5.5/§5.6`).

- [ ] **Step 6: Verify the full migration chain on a fresh DB** — `go test ./internal/store/ -race -v` (every migration `001`–`009` applies cleanly; seeds present).

- [ ] **Step 7: Commit** — `git add -A && git commit -m "feat(store): migrations 008 settings + 009 seeded servarr tables; internal/servarr + store"`

---

## Phase 0a — Definition of done

Run `gofmt -s -l .` (no output), `go vet ./...`, and `go test ./... -race -cover` — all green — against a binary whose module is `github.com/radaiko/boxarr`, whose `cmd/boxarr` builds, whose config parses `BOXARR_*` (required `TORBOX_API_TOKEN`/`WEBDAV_MOUNT_ROOT`/`PROWLARR_URL`/`PROWLARR_API_KEY`/`TMDB_API_KEY`, every other var defaulted, library roots `os.Stat`-validated), whose router serves **only** `/healthz` (`/api` and `/sabnzbd/api` 404; no `SAB_API_KEY`/SAB structs remain), and whose SQLite store applies migrations `001`–`009` cleanly on both a fresh DB and one already at `003` (legacy jobs back-fill `protocol='usenet'`), exposing the extended `Job` plus the `media`/`notify`/`webdav`/`servarr` domain types and their store methods (catalog CRUD with cascade + air-date-aware `WantedEpisodes`/`WantedMovies`, notification queue, webdav upsert, settings KV, seeded servarr ids 1/1/2) — all under the single-writer DSN with application-level dedup and the Go-only state machine (now 12 states incl. `StateSeeding`).

---

## Phase 0b–0d roadmap (next sub-plans)

Each is independently shippable and built on 0a. I will expand each into a full bite-sized plan on request.

- **0b — Outbound clients (`docs/specs/03`).** Extend `internal/torbox` (`CreateTorrent`/`ListTorrents`/`ControlTorrent`/`CheckCached`/`UserMe` reusing `do()`/`Envelope`/`FlexInt`/rate-limit, with `httptest` fixtures); new `internal/prowlarr`, `internal/metadata/tmdb`, `internal/metadata/tvdb` (JWT lifecycle), `internal/plex`. Each copies the TorBox `do()` shape; TDD against recorded JSON fixtures; decode defensively per the `00 §9` register. *Ships: a `GET /api/v1/account` proxy of `/user/me` provable end-to-end (folds into 0c).* 
- **0c — `/api/v1` chassis (`docs/specs/04`, `01 §5`).** chi `/api/v1` subrouter + constant-time `X-Api-Key` middleware (loopback-empty-key bypass) + the `{error:{code,message,details}}` envelope; repoint `/healthz` Pinger to DB+TorBox; implement `GET /api/v1/status`, `GET /api/v1/health`, `GET/PUT /api/v1/settings` (DB-overlay), `GET /api/v1/account`, and stub the series/movies/search routes so the SPA compiles; wire `main.go run()` to construct clients + Server + start the HTTP server with graceful shutdown. TDD with `httptest`. *Ships: a configured Boxarr that boots, serves a settings round-trip, and reports account/health.*
- **0d — Frontend embed + CI/Docker (`docs/specs/07`, `08`).** `internal/web` `//go:embed all:dist` + SPA index.html fallback (+ committed placeholder); `web/` React+TS+Vite scaffold (`.npmrc`, pinned `package.json`, `pnpm-lock.yaml`, nav shell + working Settings page); multi-stage `deploy/Dockerfile` (pnpm frontend → Go build embedding `dist` → distroless); `ci.yml` `frontend` job + verbatim Go gates + no-push docker build; `release.yml` renamed to `ghcr.io/radaiko/boxarr`; `docker-compose.yml` + `.env.example`. *Ships: `main` publishes a multi-arch image with the SPA embedded from the first commit — the Phase 0 acceptance bar in `09 §2`.*
