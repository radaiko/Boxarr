package task

import (
	"context"
	"errors"
	"testing"
	"time"
)

func waitState(t *testing.T, m *Manager, id int64, want string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		for _, x := range m.List() {
			if x.ID == id && x.State == want {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("task %d never reached state %q", id, want)
}

func TestManagerRunsTasks(t *testing.T) {
	m := New(context.Background())
	ran := make(chan struct{}, 1)
	id := m.Enqueue("adopt", "The Matrix", func(context.Context) error { ran <- struct{}{}; return nil })
	if id != 1 {
		t.Fatalf("first id = %d, want 1", id)
	}
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("task fn never ran")
	}
	waitState(t, m, id, "done")
}

func TestManagerRecordsErrors(t *testing.T) {
	m := New(context.Background())
	id := m.Enqueue("delete", "X", func(context.Context) error { return errors.New("boom") })
	waitState(t, m, id, "error")
	for _, x := range m.List() {
		if x.ID == id && x.Error != "boom" {
			t.Fatalf("error = %q, want boom", x.Error)
		}
	}
}
