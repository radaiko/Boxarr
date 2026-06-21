package store

import (
	"context"
	"testing"

	"github.com/radaiko/boxarr/internal/job"
)

func TestActiveJobForMedia(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	if j, _ := st.ActiveJobForMedia(ctx, "movie", 5); j != nil {
		t.Fatal("no job yet, want nil")
	}
	id, err := st.CreateJob(ctx, &job.Job{State: job.StatePending, MediaType: "movie", MediaRef: 5, NZBName: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if j, _ := st.ActiveJobForMedia(ctx, "movie", 5); j == nil || j.ID != id {
		t.Fatalf("want active job %d, got %+v", id, j)
	}
	// A different media ref is not matched.
	if j, _ := st.ActiveJobForMedia(ctx, "movie", 6); j != nil {
		t.Fatal("different media ref should not match")
	}
	// Once imported (terminal), it no longer counts as active.
	jb, _ := st.GetJob(ctx, id)
	jb.State = job.StateImported
	if err := st.UpdateJob(ctx, jb); err != nil {
		t.Fatal(err)
	}
	if j, _ := st.ActiveJobForMedia(ctx, "movie", 5); j != nil {
		t.Fatal("imported job should not count as active")
	}
}

func TestJobAheadForMedia(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	// Two jobs for the same episode: one downloading, one pending.
	a, _ := st.CreateJob(ctx, &job.Job{State: job.StateDownloading, MediaType: "episode", MediaRef: 7, NZBName: "a"})
	b, _ := st.CreateJob(ctx, &job.Job{State: job.StatePending, MediaType: "episode", MediaRef: 7, NZBName: "b"})
	// b is redundant: a is ahead.
	if ahead, _ := st.JobAheadForMedia(ctx, b, "episode", 7); !ahead {
		t.Error("pending job should see the downloading one as ahead")
	}
	// a is not redundant: b (pending) does not count as ahead.
	if ahead, _ := st.JobAheadForMedia(ctx, a, "episode", 7); ahead {
		t.Error("downloading job should not be superseded by a pending one")
	}
}
