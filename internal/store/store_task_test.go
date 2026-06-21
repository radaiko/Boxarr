package store

import (
	"context"
	"testing"
	"time"

	"github.com/radaiko/boxarr/internal/task"
)

func TestTaskPersistenceRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	st.SaveTask(task.Task{
		ID: 1, Type: "delete", Label: "5 items", State: "running", Current: 2, Total: 5,
		Details: []string{"a.mkv", "b.mkv"}, CreatedAt: time.Now(),
	})
	got, err := st.ListTasks(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].State != "running" || got[0].Total != 5 || len(got[0].Details) != 2 {
		t.Fatalf("round-trip = %+v", got)
	}
	// Upsert updates in place.
	st.SaveTask(task.Task{ID: 1, Type: "delete", Label: "5 items", State: "done", Current: 5, Total: 5, CreatedAt: time.Now()})
	if got, _ = st.ListTasks(ctx, 10); len(got) != 1 || got[0].State != "done" {
		t.Fatalf("after upsert = %+v", got)
	}
	// A second running task gets flagged interrupted on restart.
	st.SaveTask(task.Task{ID: 2, Type: "adopt", Label: "X", State: "running", CreatedAt: time.Now()})
	if err := st.MarkRunningTasksInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ = st.ListTasks(ctx, 10)
	for _, x := range got {
		if x.ID == 2 && (x.State != "error" || x.Error == "") {
			t.Errorf("task 2 not interrupted: %+v", x)
		}
		if x.ID == 1 && x.State != "done" {
			t.Errorf("done task should be untouched: %+v", x)
		}
	}
}
