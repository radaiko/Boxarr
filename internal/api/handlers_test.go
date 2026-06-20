package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/store"
)

// newAPITestStore opens a throwaway migrated store.
func newAPITestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func testServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st := newAPITestStore(t)
	cfg := &config.Config{WebDAVMountRoot: t.TempDir()}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewServer(st, cfg, logger), st
}

// TestRouter_DropsSABSurface proves the SABnzbd emulation paths are gone while
// /healthz survives (00 Assumption B).
func TestRouter_DropsSABSurface(t *testing.T) {
	srv, _ := testServer(t)
	r := srv.Router()
	for _, path := range []string{"/api", "/sabnzbd/api"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: want 404, got %d", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("/healthz: want 200, got %d", rec.Code)
	}
}

func TestHealthz(t *testing.T) {
	srv, _ := testServer(t)
	srv.health = &fakeHealth{ok: true}
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("healthz: got %d", rec.Code)
	}
}

func TestHealthzUnhealthyAndNil(t *testing.T) {
	srv, _ := testServer(t)
	srv.health = &fakeHealth{ok: false}
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("unhealthy: got %d want 503", rec.Code)
	}

	srv.health = nil
	rec = httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("nil health: got %d want 200", rec.Code)
	}
}

// fakeHealth is a canned Checker.
type fakeHealth struct{ ok bool }

func (f *fakeHealth) Check(context.Context) error {
	if f.ok {
		return nil
	}
	return io.EOF
}
