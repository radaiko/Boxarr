// Package worker runs the background loops that drive jobs to completion.
package worker

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/radaiko/boxarr/internal/plex"
	"github.com/radaiko/boxarr/internal/settings"
	"github.com/radaiko/boxarr/internal/store"
	"github.com/radaiko/boxarr/internal/torbox"
)

// TorBoxAPI is the subset of the TorBox client the workers depend on.
type TorBoxAPI interface {
	CreateUsenetDownload(ctx context.Context, req torbox.CreateRequest) (*torbox.CreateResult, error)
	ListUsenet(ctx context.Context) ([]torbox.UsenetDownload, error)
	ControlUsenet(ctx context.Context, id int64, op string) error
	CreateTorrent(ctx context.Context, req torbox.TorrentCreateRequest) (*torbox.TorrentCreateResult, error)
	ListTorrents(ctx context.Context) ([]torbox.TorrentDownload, error)
	ControlTorrent(ctx context.Context, id int64, op string) error
	Ping(ctx context.Context) error
}

// PlexScanner triggers a Plex library scan after import. *plex.Client satisfies it.
type PlexScanner interface {
	ScanPath(ctx context.Context, sectionID, path string) error
	ScanSection(ctx context.Context, sectionID string) error
	SectionLocations(ctx context.Context, sectionID string) ([]string, error)
	SectionItems(ctx context.Context, sectionID string, typ int) ([]plex.LibItem, error)
	ShowEpisodes(ctx context.Context, showRatingKey string) ([]plex.LibItem, error)
	ItemStreams(ctx context.Context, ratingKey string) (partID int, audio, subs []plex.Stream, err error)
	SetDefaultStreams(ctx context.Context, partID, audioStreamID, subtitleStreamID int) error
}

// Automation drives the optional Phase-5 scheduled loops. *catalog.Service
// satisfies it. Registered only when config enables automation.
type Automation interface {
	AutoSearchWanted(ctx context.Context) error
	UpgradeWanted(ctx context.Context) error
	RefreshMetadata(ctx context.Context) error
}

// Workers owns the Submitter, Poller, and Reaper background loops.
type Workers struct {
	store  *store.Store
	tb     TorBoxAPI
	set    *settings.Store
	logger *slog.Logger

	// missingPolls counts, per TorBox ID, how many consecutive polls a job
	// has been absent from the TorBox list. Touched only by the single
	// poller goroutine, so it needs no lock.
	missingPolls map[int64]int

	// deleteAttempts counts, per job ID, failed TorBox-delete attempts.
	// Touched only by the single deleter goroutine, so it needs no lock.
	deleteAttempts map[int64]int

	// submitBackoffUntil pauses NZB submissions after TorBox rate-limits us
	// with a 429. Touched only by the single submitter goroutine, no lock.
	submitBackoffUntil time.Time

	// torrent-loop state, mirroring the usenet maps/timers so a usenet 429 can
	// never pause torrents and vice-versa. Each touched by one goroutine only.
	torrentMissingPolls       map[int64]int
	torrentSubmitBackoffUntil time.Time

	// plex, when set, receives a partial-scan call after each import (optional).
	plex PlexScanner
	// plexLocCache memoizes each section's Plex-side library paths (sectionID →
	// []path) so the post-import scan can remap Boxarr's path onto Plex's mount
	// without re-querying Plex every time.
	plexLocCache sync.Map

	// automation, when set, enables the Phase-5 scheduled loops (optional).
	automation Automation

	// adoptResolver resolves unknown-content folders to catalog links (optional).
	adoptResolver AdoptResolver

	// WebDAV /refresh state, also poller-goroutine-only.
	httpClient         *http.Client
	lastWebDAVRefresh  time.Time
	webdavBackoffUntil time.Time

	// healLastRun is the Unix-nanos timestamp of the last healOnce run,
	// read by the /health/symlinks endpoint. Atomic for cross-goroutine reads.
	healLastRun atomic.Int64

	// loopRuns records each background loop's last run + interval, for the
	// dashboard "running now / up next" schedule. Guarded by loopMu.
	loopMu   sync.Mutex
	loopRuns map[string]loopState
}

type loopState struct {
	last     time.Time
	interval time.Duration
}

// New constructs a Workers.
func New(st *store.Store, tb TorBoxAPI, set *settings.Store, logger *slog.Logger) *Workers {
	return &Workers{
		store:               st,
		tb:                  tb,
		set:                 set,
		logger:              logger,
		missingPolls:        map[int64]int{},
		torrentMissingPolls: map[int64]int{},
		deleteAttempts:      map[int64]int{},
		httpClient:          &http.Client{Timeout: 30 * time.Second},
		loopRuns:            map[string]loopState{},
	}
}

// SetPlex attaches an optional Plex scanner invoked after each import.
func (w *Workers) SetPlex(p PlexScanner) { w.plex = p }

// SetAutomation enables the Phase-5 scheduled auto-search + metadata-refresh loops.
func (w *Workers) SetAutomation(a Automation) { w.automation = a }

// loopSpec is one background loop: a name, a tick interval, and the function
// run each tick.
type loopSpec struct {
	name     string
	interval time.Duration
	fn       func(context.Context) error
}

// Run starts every background loop and blocks until ctx is cancelled.
func (w *Workers) Run(ctx context.Context) {
	loops := []loopSpec{
		{"submitter", w.set.PollInterval(), w.submitOnce},
		{"poller", w.set.PollInterval(), w.pollOnce},
		{"torrent-submitter", w.set.PollInterval(), w.submitTorrentsOnce},
		{"torrent-poller", w.set.PollInterval(), w.pollTorrentsOnce},
		{"deleter", w.set.PollInterval(), w.deleteOnce},
		{"reaper", 5 * time.Minute, w.reapOnce},
		{"reconciler", w.set.ReconcileInterval(), w.reconcileOnce},
		{"plex-language", w.set.SearchInterval(), w.plexLanguageLoop},
	}
	if w.set.HealEnabled() {
		loops = append(loops,
			loopSpec{"healer", w.set.HealInterval(), w.healOnce},
			loopSpec{"heal-reconciler", w.set.PollInterval(), w.healReconcileOnce},
		)
	}
	if w.automation != nil {
		loops = append(loops,
			loopSpec{"metadata-refresh", w.set.MetadataInterval(), w.metadataRefreshOnce},
			loopSpec{"auto-search", w.set.SearchInterval(), w.autoSearchOnce},
		)
	}
	var wg sync.WaitGroup
	for _, l := range loops {
		wg.Add(1)
		go func(spec loopSpec) {
			defer wg.Done()
			w.loop(ctx, spec.name, spec.interval, spec.fn)
		}(l)
	}
	wg.Wait()
}

// HealRunInfo returns the last and next scheduled healer run. Both are zero
// before the first run or when healing is disabled.
func (w *Workers) HealRunInfo() (last, next time.Time) {
	n := w.healLastRun.Load()
	if n == 0 {
		return time.Time{}, time.Time{}
	}
	last = time.Unix(0, n)
	return last, last.Add(w.set.HealInterval())
}

// loopLabels gives the background loops friendly names for the dashboard.
var loopLabels = map[string]string{
	"poller": "TorBox poll", "torrent-poller": "Torrent poll", "submitter": "Submit to TorBox",
	"torrent-submitter": "Submit torrents", "deleter": "Process deletions", "reaper": "Reaper",
	"reconciler": "Reconcile mount", "plex-language": "Plex language sweep",
	"healer": "Heal broken links", "heal-reconciler": "Heal reconcile",
	"metadata-refresh": "Metadata refresh", "auto-search": "Auto-search + upgrade",
}

// LoopSchedule returns each background loop's last/next run for the dashboard,
// soonest-next first. Loops that haven't run yet are omitted.
func (w *Workers) LoopSchedule() []map[string]any {
	w.loopMu.Lock()
	defer w.loopMu.Unlock()
	out := make([]map[string]any, 0, len(w.loopRuns))
	for name, st := range w.loopRuns {
		label := loopLabels[name]
		if label == "" {
			label = name
		}
		out = append(out, map[string]any{
			"name": label, "everySeconds": int(st.interval.Seconds()),
			"lastRun": st.last.UTC().Format(time.RFC3339),
			"nextRun": st.last.Add(st.interval).UTC().Format(time.RFC3339),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["nextRun"].(string) < out[j]["nextRun"].(string) })
	return out
}

// loop runs fn immediately, then every interval, until ctx is cancelled.
func (w *Workers) loop(ctx context.Context, name string, interval time.Duration, fn func(context.Context) error) {
	if interval <= 0 {
		interval = time.Minute // defensive: a misconfigured interval must not panic
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if err := fn(ctx); err != nil && ctx.Err() == nil {
			w.logger.Error("worker iteration failed", "worker", name, "error", err)
		}
		w.loopMu.Lock()
		w.loopRuns[name] = loopState{last: time.Now(), interval: interval}
		w.loopMu.Unlock()
		select {
		case <-ctx.Done():
			w.logger.Info("worker stopped", "worker", name)
			return
		case <-t.C:
		}
	}
}
