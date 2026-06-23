// Package logbuf keeps the most recent log records in memory so the web UI can
// show a live tail without shelling into the container. It wraps any slog.Handler
// (the real JSON-to-stdout handler) and mirrors each record into a ring buffer.
package logbuf

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Entry is one captured log record.
type Entry struct {
	Time  time.Time         `json:"time"`
	Level slog.Level        `json:"level"`
	Msg   string            `json:"msg"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

// Ring is a fixed-size, thread-safe circular buffer of log entries.
type Ring struct {
	mu   sync.RWMutex
	buf  []Entry
	size int
	n    int // total entries ever written
}

// New returns a ring buffer holding the last size entries (default 2000).
func New(size int) *Ring {
	if size <= 0 {
		size = 2000
	}
	return &Ring{buf: make([]Entry, 0, size), size: size}
}

func (r *Ring) add(e Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buf) < r.size {
		r.buf = append(r.buf, e)
	} else {
		r.buf[r.n%r.size] = e
	}
	r.n++
}

// Entries returns up to limit most-recent entries (newest first) at or above
// minLevel, optionally filtered to those containing q (in message or any attr).
func (r *Ring) Entries(limit int, minLevel slog.Level, q string) []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var ordered []Entry
	if len(r.buf) < r.size {
		ordered = r.buf
	} else {
		start := r.n % r.size
		ordered = append(ordered, r.buf[start:]...)
		ordered = append(ordered, r.buf[:start]...)
	}
	q = strings.ToLower(q)
	out := make([]Entry, 0, limit)
	for i := len(ordered) - 1; i >= 0; i-- {
		e := ordered[i]
		if e.Level < minLevel {
			continue
		}
		if q != "" && !matches(e, q) {
			continue
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func matches(e Entry, q string) bool {
	if strings.Contains(strings.ToLower(e.Msg), q) {
		return true
	}
	for k, v := range e.Attrs {
		if strings.Contains(strings.ToLower(k), q) || strings.Contains(strings.ToLower(v), q) {
			return true
		}
	}
	return false
}

// handler mirrors records into a Ring, then delegates to the wrapped handler.
type handler struct {
	inner slog.Handler
	ring  *Ring
	attrs map[string]string
}

// NewHandler wraps inner so every record is also captured in ring.
func NewHandler(inner slog.Handler, ring *Ring) slog.Handler {
	return &handler{inner: inner, ring: ring, attrs: map[string]string{}}
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool { return h.inner.Enabled(ctx, l) }

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	attrs := make(map[string]string, len(h.attrs)+r.NumAttrs())
	for k, v := range h.attrs {
		attrs[k] = v
	}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()
		return true
	})
	t := r.Time
	if t.IsZero() {
		t = time.Now()
	}
	h.ring.add(Entry{Time: t, Level: r.Level, Msg: r.Message, Attrs: attrs})
	return h.inner.Handle(ctx, r)
}

func (h *handler) WithAttrs(as []slog.Attr) slog.Handler {
	na := make(map[string]string, len(h.attrs)+len(as))
	for k, v := range h.attrs {
		na[k] = v
	}
	for _, a := range as {
		na[a.Key] = a.Value.String()
	}
	return &handler{inner: h.inner.WithAttrs(as), ring: h.ring, attrs: na}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{inner: h.inner.WithGroup(name), ring: h.ring, attrs: h.attrs}
}

// ParseLevel maps a level name to a slog.Level; unknown/empty → Debug (show all).
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "err":
		return slog.LevelError
	default:
		return slog.LevelDebug
	}
}
