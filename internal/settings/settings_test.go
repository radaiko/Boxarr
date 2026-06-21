package settings

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestSeedFallbackAndDBOverride(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	seed := &config.Config{ProwlarrURL: "http://env:9696", TMDBAPIKey: "envtoken", MovieLibraryRoot: "/data/movies"}
	s, err := New(ctx, st, seed)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Falls back to the env/default seed.
	if s.ProwlarrURL() != "http://env:9696" || s.TMDBToken() != "envtoken" {
		t.Fatalf("seed fallback wrong: prowlarr=%q tmdb=%q", s.ProwlarrURL(), s.TMDBToken())
	}
	// DB override wins and persists.
	if err := s.Set(ctx, KeyProwlarrURL, "http://ui:9696"); err != nil {
		t.Fatal(err)
	}
	if s.ProwlarrURL() != "http://ui:9696" {
		t.Fatalf("DB override should win, got %q", s.ProwlarrURL())
	}
	// Survives a reload (new Store over the same DB).
	s2, _ := New(ctx, st, seed)
	if s2.ProwlarrURL() != "http://ui:9696" {
		t.Fatalf("override should persist across reload, got %q", s2.ProwlarrURL())
	}
}

func TestClientHotReloadOnCredentialChange(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, newTestStore(t), &config.Config{TMDBAPIKey: "a"})
	if err != nil {
		t.Fatal(err)
	}
	c1 := s.TMDB()
	if s.TMDB() != c1 {
		t.Fatal("TMDB() should be memoized when the token is unchanged")
	}
	if err := s.Set(ctx, KeyTMDBToken, "b"); err != nil {
		t.Fatal(err)
	}
	if s.TMDB() == c1 {
		t.Fatal("TMDB() must rebuild after the token changes (hot-reload)")
	}
}

func TestEnabledPredicatesAndRedaction(t *testing.T) {
	ctx := context.Background()
	s, _ := New(ctx, newTestStore(t), &config.Config{})
	if s.PlexEnabled() || s.TVDBEnabled() || s.SeerrEnabled() {
		t.Fatal("nothing should be enabled on an empty config")
	}
	_ = s.Set(ctx, KeyTMDBToken, "secret-token")
	_ = s.Set(ctx, KeyProwlarrURL, "http://x:9696")
	red := s.Redacted()
	if red[KeyTMDBToken] != "********" {
		t.Errorf("secret must be redacted, got %q", red[KeyTMDBToken])
	}
	if red[KeyProwlarrURL] != "http://x:9696" {
		t.Errorf("non-secret must be shown, got %q", red[KeyProwlarrURL])
	}
	if !Writable(KeyTMDBToken) || Writable("bogus.key") {
		t.Error("Writable allow-list wrong")
	}
}

func TestBootNeverFailsWithEmptyConfig(t *testing.T) {
	// The whole point: a fresh install with no env still boots.
	s, err := New(context.Background(), newTestStore(t), &config.Config{})
	if err != nil {
		t.Fatalf("settings must build from an empty config: %v", err)
	}
	if s.TorBox() == nil || s.Prowlarr() == nil || s.TMDB() == nil {
		t.Fatal("client factories must return non-nil even unconfigured")
	}
}
