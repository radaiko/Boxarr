package store

import (
	"context"
	"testing"
	"time"

	"github.com/radaiko/boxarr/internal/job"
)

func TestLimitEvents(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.RecordLimitEvent(ctx, "rate_limit", "after 12 grabs"); err != nil {
		t.Fatal(err)
	}
	evs, err := st.ListLimitEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Kind != "rate_limit" || evs[0].Detail != "after 12 grabs" {
		t.Fatalf("events = %+v", evs)
	}
}

func TestCountJobsSubmittedSince(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	jid, err := st.CreateJob(ctx, &job.Job{State: job.StateImported, NZBName: "a", MediaType: "movie"})
	if err != nil {
		t.Fatal(err)
	}
	j, _ := st.GetJob(ctx, jid)
	now := time.Now()
	j.SubmittedAt = &now
	if err := st.UpdateJob(ctx, j); err != nil {
		t.Fatal(err)
	}
	if n, _ := st.CountJobsSubmittedSince(ctx, now.Add(-time.Hour)); n != 1 {
		t.Errorf("count since 1h ago = %d, want 1", n)
	}
	if n, _ := st.CountJobsSubmittedSince(ctx, now.Add(time.Hour)); n != 0 {
		t.Errorf("count since 1h ahead = %d, want 0", n)
	}
}
