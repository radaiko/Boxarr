package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenReportsUnwritableDir(t *testing.T) {
	// A regular file standing in for the /config dir: SQLite would just say
	// "unable to open database file (14)"; Open must surface an actionable error.
	notADir := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Open(context.Background(), filepath.Join(notADir, "boxarr.db"))
	if err == nil {
		t.Fatal("expected an error opening a DB under a non-directory")
	}
	if !strings.Contains(err.Error(), "boxarr.db") && !strings.Contains(err.Error(), notADir) {
		t.Errorf("error should name the path: %v", err)
	}
}
