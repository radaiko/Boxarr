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
