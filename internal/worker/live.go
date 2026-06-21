package worker

import (
	"context"

	"github.com/radaiko/boxarr/internal/settings"
	"github.com/radaiko/boxarr/internal/torbox"
)

// LiveTorBox returns a TorBoxAPI that resolves the current TorBox client from the
// settings store on every call, so a UI credential change applies without a
// restart (the factory is memoized; it only rebuilds when the token changes).
func LiveTorBox(set *settings.Store) TorBoxAPI { return liveTorBox{set} }

type liveTorBox struct{ set *settings.Store }

func (l liveTorBox) CreateUsenetDownload(ctx context.Context, r torbox.CreateRequest) (*torbox.CreateResult, error) {
	return l.set.TorBox().CreateUsenetDownload(ctx, r)
}
func (l liveTorBox) ListUsenet(ctx context.Context) ([]torbox.UsenetDownload, error) {
	return l.set.TorBox().ListUsenet(ctx)
}
func (l liveTorBox) ControlUsenet(ctx context.Context, id int64, op string) error {
	return l.set.TorBox().ControlUsenet(ctx, id, op)
}
func (l liveTorBox) CreateTorrent(ctx context.Context, r torbox.TorrentCreateRequest) (*torbox.TorrentCreateResult, error) {
	return l.set.TorBox().CreateTorrent(ctx, r)
}
func (l liveTorBox) ListTorrents(ctx context.Context) ([]torbox.TorrentDownload, error) {
	return l.set.TorBox().ListTorrents(ctx)
}
func (l liveTorBox) ControlTorrent(ctx context.Context, id int64, op string) error {
	return l.set.TorBox().ControlTorrent(ctx, id, op)
}
func (l liveTorBox) Ping(ctx context.Context) error { return l.set.TorBox().Ping(ctx) }

// LivePlex returns a PlexScanner that resolves the current Plex client live.
func LivePlex(set *settings.Store) PlexScanner { return livePlex{set} }

type livePlex struct{ set *settings.Store }

func (l livePlex) ScanPath(ctx context.Context, sectionID, path string) error {
	return l.set.Plex().ScanPath(ctx, sectionID, path)
}
