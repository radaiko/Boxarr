package store

import (
	"context"
	"testing"

	"github.com/radaiko/boxarr/internal/job"
)

func TestTorrentJobRoundtripAndDedup(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	j := &job.Job{
		State: job.StatePending, Category: "movie", NZBName: "X",
		Protocol: "torrent", TorrentHash: "abc123",
		TorrentMagnet: "magnet:?xt=urn:btih:abc123", TorrentFile: []byte("d8:announce"),
		MediaType: "movie", MediaRef: 7,
	}
	id, err := st.CreateJob(ctx, j)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	got, err := st.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Protocol != "torrent" || got.TorrentHash != "abc123" ||
		got.TorrentMagnet != "magnet:?xt=urn:btih:abc123" || got.MediaType != "movie" || got.MediaRef != 7 {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if string(got.TorrentFile) != "d8:announce" {
		t.Fatalf("torrent_file roundtrip mismatch: %q", got.TorrentFile)
	}

	dup, err := st.FindByTorrentHash(ctx, "abc123", "movie")
	if err != nil {
		t.Fatalf("FindByTorrentHash: %v", err)
	}
	if dup == nil || dup.ID != id {
		t.Fatalf("FindByTorrentHash should return the existing job, got %+v", dup)
	}
	if none, _ := st.FindByTorrentHash(ctx, "abc123", "series"); none != nil {
		t.Fatal("dedup must be category-scoped")
	}

	byMedia, err := st.FindJobByMedia(ctx, "movie", 7)
	if err != nil || byMedia == nil || byMedia.ID != id {
		t.Fatalf("FindJobByMedia should return the job, got %+v err=%v", byMedia, err)
	}
}

func TestUsenetJobDefaultsProtocol(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id, err := st.CreateJob(ctx, &job.Job{State: job.StatePending, Category: "sonarr", NZBName: "U"})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	got, _ := st.GetJob(ctx, id)
	if got.Protocol != "usenet" {
		t.Fatalf("protocol should default to usenet, got %q", got.Protocol)
	}
}
