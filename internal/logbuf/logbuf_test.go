package logbuf

import (
	"io"
	"log/slog"
	"testing"
)

func TestRingCaptureFilterWrap(t *testing.T) {
	r := New(3)
	l := slog.New(NewHandler(slog.NewTextHandler(io.Discard, nil), r))
	l.Info("one")
	l.Warn("two")
	l.Error("three")
	l.Info("four") // overflows size-3 ring → drops "one"

	all := r.Entries(10, slog.LevelDebug, "")
	if len(all) != 3 || all[0].Msg != "four" || all[2].Msg != "two" {
		t.Fatalf("newest-first wrap wrong: %+v", msgs(all))
	}
	if errs := r.Entries(10, slog.LevelError, ""); len(errs) != 1 || errs[0].Msg != "three" {
		t.Errorf("level filter: %+v", msgs(errs))
	}

	l.With("job_id", "42").Info("grabbing")
	if hits := r.Entries(10, slog.LevelDebug, "42"); len(hits) != 1 || hits[0].Msg != "grabbing" {
		t.Errorf("attr query filter: %+v", msgs(hits))
	}
}

func msgs(es []Entry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Msg)
	}
	return out
}
