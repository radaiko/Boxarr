package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSPAHandlerServesIndexAndFallback(t *testing.T) {
	h := SPAHandler()

	// Root serves the embedded index.html.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/: got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Boxarr") {
		t.Errorf("/ should serve index.html, got %q", rec.Body.String())
	}

	// An unknown client route falls back to index.html (SPA shell), not 404.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/movies/123", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/movies/123: got %d, want 200 (SPA fallback)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<!doctype html>") {
		t.Errorf("deep link should fall back to index.html")
	}
}
