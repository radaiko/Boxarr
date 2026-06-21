// Package task is a tiny in-process background runner for user-triggered work
// (adopting/deleting WebDAV content) that must outlive the HTTP request — so
// navigating away in the UI doesn't cancel it. Tasks run sequentially against a
// long-lived context and their status is listable for an Activity view.
package task

import (
	"context"
	"sync"
	"time"
)

// Task is the listable status of a background unit of work.
type Task struct {
	ID         int64      `json:"id"`
	Type       string     `json:"type"`  // "adopt" | "delete"
	Label      string     `json:"label"` // human label (release/show name)
	State      string     `json:"state"` // queued | running | done | error
	Current    int        `json:"current,omitempty"`
	Total      int        `json:"total,omitempty"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// Progress reports how far a running task is (done out of total).
type Progress func(done, total int)

type queued struct {
	t  *Task
	fn func(context.Context, Progress) error
}

// Manager owns the queue + recent-task history.
type Manager struct {
	mu    sync.Mutex
	seq   int64
	tasks []*Task // newest first, capped
	queue chan queued
	ctx   context.Context
	now   func() time.Time
}

// New starts a background worker bound to ctx (the app lifetime). When ctx is
// cancelled the worker drains and stops.
func New(ctx context.Context) *Manager {
	m := &Manager{queue: make(chan queued, 256), ctx: ctx, now: time.Now}
	go m.run()
	return m
}

func (m *Manager) run() {
	for {
		select {
		case <-m.ctx.Done():
			return
		case j := <-m.queue:
			m.set(j.t, "running", "")
			progress := func(done, total int) { m.setProgress(j.t, done, total) }
			err := j.fn(m.ctx, progress)
			if err != nil {
				m.set(j.t, "error", err.Error())
			} else {
				m.set(j.t, "done", "")
			}
		}
	}
}

func (m *Manager) setProgress(t *Task, done, total int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t.Current, t.Total = done, total
}

// Enqueue records a task and schedules fn to run on the background worker. fn
// receives a Progress callback to report done/total as it works.
func (m *Manager) Enqueue(typ, label string, fn func(context.Context, Progress) error) int64 {
	m.mu.Lock()
	m.seq++
	t := &Task{ID: m.seq, Type: typ, Label: label, State: "queued", CreatedAt: m.now()}
	m.tasks = append([]*Task{t}, m.tasks...)
	if len(m.tasks) > 100 {
		m.tasks = m.tasks[:100]
	}
	m.mu.Unlock()
	m.queue <- queued{t: t, fn: fn}
	return t.ID
}

func (m *Manager) set(t *Task, state, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t.State, t.Error = state, errMsg
	now := m.now()
	if state == "running" && t.StartedAt == nil {
		t.StartedAt = &now
	}
	if state == "done" || state == "error" {
		t.FinishedAt = &now
	}
}

// List returns a snapshot of recent tasks, newest first.
func (m *Manager) List() []Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Task, len(m.tasks))
	for i, t := range m.tasks {
		out[i] = *t
	}
	return out
}

// ActiveCount returns how many tasks are queued or running (for nav badges).
func (m *Manager) ActiveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, t := range m.tasks {
		if t.State == "queued" || t.State == "running" {
			n++
		}
	}
	return n
}
