package config

import (
	"os"
	"testing"
	"time"
)

// baseEnv sets every required BOXARR_* var (all filesystem roots pointed at one
// temp dir) so Load() succeeds; individual tests then tweak what they exercise.
func baseEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("BOXARR_TORBOX_API_TOKEN", "tok")
	t.Setenv("BOXARR_WEBDAV_MOUNT_ROOT", dir)
	t.Setenv("BOXARR_SYMLINK_ROOT", dir)
	t.Setenv("BOXARR_MOVIE_LIBRARY_ROOT", dir)
	t.Setenv("BOXARR_TV_LIBRARY_ROOT", dir)
	t.Setenv("BOXARR_PROWLARR_URL", "http://prowlarr:9696")
	t.Setenv("BOXARR_PROWLARR_API_KEY", "pk")
	t.Setenv("BOXARR_TMDB_API_KEY", "tk")
	return dir
}

func TestLoadDefaults(t *testing.T) {
	dir := baseEnv(t)
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.ListenAddr != ":8080" {
		t.Errorf("ListenAddr default: %q", c.ListenAddr)
	}
	if c.PollInterval != time.Minute {
		t.Errorf("PollInterval default: %v", c.PollInterval)
	}
	if c.UsenetPath() != dir {
		t.Errorf("UsenetPath default should be the mount root: %q", c.UsenetPath())
	}
	if len(c.Categories) != 3 {
		t.Errorf("Categories default: %v", c.Categories)
	}
	if c.TorBoxWebDAVRefreshURL != "https://webdav.torbox.app/refresh" {
		t.Errorf("refresh URL default: %q", c.TorBoxWebDAVRefreshURL)
	}
	if c.WebDAVRefreshCooldown != 2*time.Minute {
		t.Errorf("refresh cooldown default: %v", c.WebDAVRefreshCooldown)
	}
	if c.WebDAVRefreshEnabled() {
		t.Error("WebDAV refresh must be disabled without credentials")
	}
}

func TestLoadNewDefaults(t *testing.T) {
	dir := baseEnv(t)
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.ReconcileInterval != 15*time.Minute {
		t.Errorf("ReconcileInterval default: %v, want 15m", c.ReconcileInterval)
	}
	if c.MetadataInterval != 24*time.Hour {
		t.Errorf("MetadataInterval default: %v, want 24h", c.MetadataInterval)
	}
	if c.SearchInterval != 6*time.Hour {
		t.Errorf("SearchInterval default: %v, want 6h", c.SearchInterval)
	}
	if c.PlexScanTimeout != time.Minute {
		t.Errorf("PlexScanTimeout default: %v, want 60s", c.PlexScanTimeout)
	}
	if c.TorrentPath() != dir { // empty subpath -> flat mount root
		t.Errorf("TorrentPath default should be the mount root: %q", c.TorrentPath())
	}
	if !c.HealProwlarrFallback {
		t.Error("HealProwlarrFallback should default to true")
	}
	if c.APIKey != "" {
		t.Errorf("APIKey default should be empty, got %q", c.APIKey)
	}
	if c.SelectMinSeeders != 1 || c.SelectWeightResolution != 400 ||
		c.SelectWeightProtocolCachedTorrent != 300 || c.SelectSeedSaturation != 100 {
		t.Errorf("selection defaults wrong: minSeeders=%d wRes=%d wCached=%d sat=%d",
			c.SelectMinSeeders, c.SelectWeightResolution, c.SelectWeightProtocolCachedTorrent, c.SelectSeedSaturation)
	}
	if len(c.SelectPreferredResolutions) != 3 || c.SelectPreferredResolutions[0] != "2160p" {
		t.Errorf("SelectPreferredResolutions default: %v", c.SelectPreferredResolutions)
	}
	if c.MaxActiveDownloads != 0 || c.MaxCreatePerHour != 60 || c.MaxTorrentPerMin != 300 {
		t.Errorf("limit defaults wrong: active=%d perHour=%d perMin=%d",
			c.MaxActiveDownloads, c.MaxCreatePerHour, c.MaxTorrentPerMin)
	}
}

// Load no longer fails on missing connection vars or directories — they are
// configured from the UI now (internal/settings). These boot-never-fails
// guarantees are covered in the settings package tests.

func TestEnabledPredicates(t *testing.T) {
	if !(&Config{TorBoxWebDAVUser: "u", TorBoxWebDAVPass: "p"}).WebDAVRefreshEnabled() {
		t.Error("WebDAVRefreshEnabled: both creds set should be enabled")
	}
	if (&Config{TorBoxWebDAVUser: "u"}).WebDAVRefreshEnabled() {
		t.Error("WebDAVRefreshEnabled: missing password should be disabled")
	}
	if !(&Config{PlexURL: "u", PlexToken: "t"}).PlexEnabled() {
		t.Error("PlexEnabled: url+token should be enabled")
	}
	if (&Config{PlexURL: "u"}).PlexEnabled() {
		t.Error("PlexEnabled: missing token should be disabled")
	}
	if !(&Config{TVDBAPIKey: "k"}).TVDBEnabled() {
		t.Error("TVDBEnabled: key set should be enabled")
	}
	if !(&Config{SeerrAPIKeys: []string{"k"}}).SeerrEnabled() {
		t.Error("SeerrEnabled: a key should be enabled")
	}
	if (&Config{}).SeerrEnabled() {
		t.Error("SeerrEnabled: no keys should be disabled")
	}
}

func TestSlogLevel(t *testing.T) {
	cases := map[string]string{"debug": "DEBUG", "warn": "WARN", "error": "ERROR", "info": "INFO", "": "INFO"}
	for in, want := range cases {
		if got := (&Config{LogLevel: in}).SlogLevel().String(); got != want {
			t.Errorf("SlogLevel(%q): got %s want %s", in, got, want)
		}
	}
}

func TestAllowsCategory(t *testing.T) {
	c := &Config{Categories: []string{"sonarr", "radarr"}}
	if !c.AllowsCategory("sonarr") {
		t.Error("sonarr should be allowed")
	}
	if c.AllowsCategory("lidarr") {
		t.Error("lidarr should not be allowed")
	}
}

func TestValidateMountNotDir(t *testing.T) {
	f := t.TempDir() + "/afile"
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (&Config{WebDAVMountRoot: f}).validateMount(); err == nil {
		t.Error("expected error when mount root is a file")
	}
}

func TestValidateMountMissing(t *testing.T) {
	c := &Config{WebDAVMountRoot: "/nonexistent/path/xyz"}
	if err := c.validateMount(); err == nil {
		t.Fatal("expected error for missing mount root")
	}
}

func TestHealConfigDefaults(t *testing.T) {
	baseEnv(t)
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.HealEnabled {
		t.Error("HealEnabled must default to false")
	}
	if c.HealInterval != time.Hour {
		t.Errorf("HealInterval default: %v", c.HealInterval)
	}
	if c.HealMaxAttempts != 3 {
		t.Errorf("HealMaxAttempts default: %d", c.HealMaxAttempts)
	}
	if c.HealBackoffInitial != 5*time.Minute {
		t.Errorf("HealBackoffInitial default: %v", c.HealBackoffInitial)
	}
}

func TestHealWebhookDefaults(t *testing.T) {
	baseEnv(t)
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.HealWebhookURL != "" {
		t.Errorf("HealWebhookURL default should be empty, got %q", c.HealWebhookURL)
	}
	if len(c.HealWebhookEvents) != 1 || c.HealWebhookEvents[0] != "failed" {
		t.Errorf("HealWebhookEvents default should be [failed], got %v", c.HealWebhookEvents)
	}
}
