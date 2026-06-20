// Command boxarr is a TorBox-backed media manager for TV and movies: it
// orchestrates Prowlarr search, TorBox usenet+torrent downloads, and a
// Plex-readable library over a TorBox WebDAV mount. It evolves the proven
// sab2torbox pipeline. (Phase 0: chassis; grab/import land in later phases.)
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/radaiko/boxarr/internal/api"
	"github.com/radaiko/boxarr/internal/api/seerr"
	apiv1 "github.com/radaiko/boxarr/internal/api/v1"
	"github.com/radaiko/boxarr/internal/catalog"
	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/metadata/tmdb"
	"github.com/radaiko/boxarr/internal/plex"
	"github.com/radaiko/boxarr/internal/prowlarr"
	"github.com/radaiko/boxarr/internal/store"
	"github.com/radaiko/boxarr/internal/torbox"
	"github.com/radaiko/boxarr/internal/web"
	"github.com/radaiko/boxarr/internal/worker"
)

// version is the build identifier shown in the startup log. It is "dev" for
// local builds and is overridden at release time via
// -ldflags "-X main.version=<n>", where <n> is the CI build number that
// increments on every published image.
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(runHealthcheck())
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

// run loads config, starts workers and the HTTP server, and blocks until a
// shutdown signal arrives.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.SlogLevel()}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := worker.EnsureCategoryDirs(cfg.SymlinkRoot, cfg.Categories); err != nil {
		return fmt.Errorf("preparing symlink category directories: %w", err)
	}

	st, err := store.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("opening store: %w", err)
	}
	defer func() { _ = st.Close() }()

	tb := torbox.New(cfg.TorBoxAPIToken)
	tmdbClient := tmdb.New(cfg.TMDBAPIKey)
	prowlarrClient := prowlarr.New(cfg.ProwlarrURL, cfg.ProwlarrAPIKey)
	cat := catalog.New(st, tmdbClient, cfg)
	cat.SetSearcher(prowlarrClient)
	workers := worker.New(st, tb, cfg, logger)
	if cfg.PlexEnabled() {
		workers.SetPlex(plex.New(cfg.PlexURL, cfg.PlexToken))
	}
	if cfg.AutomationEnabled {
		workers.SetAutomation(cat)
		logger.Info("automation enabled", "search_interval", cfg.SearchInterval.String(),
			"metadata_interval", cfg.MetadataInterval.String())
	}
	srv := api.NewServer(st, cfg, logger)
	srv.SetHealth(api.NewHealth(st, tb, 5*time.Minute))
	srv.SetHealReporter(workers)
	srv.SetV1Router(apiv1.NewHandler(apiv1.Deps{
		Store: st, Cfg: cfg, TorBox: tb, Prowlarr: prowlarrClient,
		Catalog: cat, Health: workers, Logger: logger, Version: version,
	}).Router())
	seerrDeps := seerr.Deps{Store: st, Cfg: cfg, Catalog: cat, TMDB: tmdbClient, Logger: logger}
	srv.SetSeerr(
		seerr.NewRouter(seerr.KindSonarr, seerrDeps),
		seerr.NewRouter(seerr.KindRadarr, seerrDeps),
	)
	srv.SetSPA(web.SPAHandler())

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		workers.Run(ctx)
	}()
	go func() {
		defer wg.Done()
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutCtx); err != nil {
			logger.Error("http shutdown", "error", err)
		}
	}()

	logger.Info("boxarr started",
		"version", version,
		"listen_addr", cfg.ListenAddr, "usenet_path", cfg.UsenetPath(),
		"poll_interval", cfg.PollInterval.String())

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		stop()
		wg.Wait()
		return fmt.Errorf("http server: %w", err)
	}
	wg.Wait()
	logger.Info("boxarr stopped")
	return nil
}

// runHealthcheck implements the `healthcheck` subcommand: it GETs the
// service's own /healthz so the distroless container HEALTHCHECK works
// without a shell. It returns a process exit code.
func runHealthcheck() int {
	addr := os.Getenv("BOXARR_LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if addr[0] == ':' {
		addr = "127.0.0.1" + addr
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", resp.StatusCode)
		return 1
	}
	return 0
}
