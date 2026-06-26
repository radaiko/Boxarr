package store

import (
	"context"
	"testing"
)

func TestGrabBlocklist(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if set, _ := st.BlocklistedGrabs(ctx); len(set) != 0 {
		t.Fatal("blocklist should start empty")
	}
	_ = st.BlocklistGrab(ctx, "Show.S01E01.GROUP", "incomplete")
	_ = st.BlocklistGrab(ctx, "Show.S01E01.GROUP", "incomplete again") // upsert, no dup
	set, err := st.BlocklistedGrabs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(set) != 1 || !set["Show.S01E01.GROUP"] {
		t.Errorf("blocklist = %v, want the one release", set)
	}
}

func TestBlocklistListAndRemove(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	_ = st.BlocklistGrab(ctx, "A.S01E01-GRP", "incomplete")
	_ = st.BlocklistGrab(ctx, "B.2024-GRP", "stalled")
	list, err := st.ListBlocklistedGrabs(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].CreatedAt == "" {
		t.Fatalf("expected 2 entries with timestamps, got %+v", list)
	}
	_ = st.RemoveBlocklistedGrab(ctx, "A.S01E01-GRP")
	if set, _ := st.BlocklistedGrabs(ctx); set["A.S01E01-GRP"] || !set["B.2024-GRP"] {
		t.Errorf("remove should drop only A, got %v", set)
	}
}

func TestGroupFailedGrabCounts(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	_ = st.BlocklistGrab(ctx, "Movie.2024.1080p.BluRay-BADGRP", "x")
	_ = st.BlocklistGrab(ctx, "Other.2024.720p.WEB-BADGRP", "x")
	_ = st.BlocklistGrab(ctx, "Third.2024.1080p.BluRay-GOODGRP", "x")
	counts, err := st.GroupFailedGrabCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts["badgrp"] != 2 || counts["goodgrp"] != 1 {
		t.Errorf("group failed counts = %v, want badgrp:2 goodgrp:1", counts)
	}
}
