package v1

import (
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/radaiko/boxarr/internal/config"
)

func newPlexHandler(t *testing.T) http.Handler {
	t.Helper()
	st := mkStore(t)
	set := mkSettings(t, st, &config.Config{})
	return NewHandler(Deps{Store: st, Settings: set, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).Router()
}

func TestPlexServersRequiresSignIn(t *testing.T) {
	rec := req(t, newPlexHandler(t), http.MethodGet, "/plex/servers", "", "127.0.0.1:1", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("servers without token = %d, want 400", rec.Code)
	}
}

func TestPlexSectionsRequiresConfig(t *testing.T) {
	rec := req(t, newPlexHandler(t), http.MethodGet, "/plex/sections", "", "127.0.0.1:1", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("sections without url/token = %d, want 400", rec.Code)
	}
}

func TestPlexPinCheckRejectsBadID(t *testing.T) {
	rec := req(t, newPlexHandler(t), http.MethodGet, "/plex/pin/not-a-number", "", "127.0.0.1:1", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("pin check bad id = %d, want 400", rec.Code)
	}
}
