package worker

import "testing"

func TestPayloadItemEquals(t *testing.T) {
	p := `{"item":"Solo Leveling S01E25","message":"wanted EN","kind":"anime"}`
	if !payloadItemEquals(p, "Solo Leveling S01E25") {
		t.Error("should match the item field")
	}
	if payloadItemEquals(p, "Solo Leveling S01E24") {
		t.Error("must not match a different item")
	}
	// A WebDAV-style payload (no item field) must not match — the old bug.
	if payloadItemEquals(`{"remotePath":"/mnt/x"}`, "Solo Leveling S01E25") {
		t.Error("payload without item must not match")
	}
}
