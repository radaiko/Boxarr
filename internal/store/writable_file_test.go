package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A pre-existing DB owned/locked read-only must fail fast at startup with an
// actionable message, not surface as opaque 500s on every read later.
func TestOpenReportsUnwritableDBFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root bypasses file permission bits")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "boxarr.db")
	// Create a good DB, then make the file unwritable.
	st, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	_, err = Open(context.Background(), path)
	if err == nil {
		t.Fatal("expected an error opening a read-only DB file")
	}
	if !strings.Contains(err.Error(), "not writable") {
		t.Errorf("error should explain the file is not writable: %v", err)
	}
}
