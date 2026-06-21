package worker

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/media"
	"github.com/radaiko/boxarr/internal/settings"
	"github.com/radaiko/boxarr/internal/store"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestHealResearchFallbackRecoversDeadArtifact(t *testing.T) {
	ctx := context.Background()
	fake := &fakeTorBox{}

	// Prowlarr returns a fresh usenet release; its .nzb is fetched from nzbSrv.
	nzbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<nzb>fresh</nzb>"))
	}))
	defer nzbSrv.Close()
	prowlarrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"title":"The.Matrix.1999.1080p.WEB-DL","protocol":"usenet","grabs":50,"size":1000000000,"downloadUrl":"` + nzbSrv.URL + `/x.nzb","guid":"g1"}]`))
	}))
	defer prowlarrSrv.Close()

	st, err := store.Open(ctx, t.TempDir()+"/h.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := &config.Config{
		HealProwlarrFallback: true, SelectMinSeeders: 1,
		SelectWeightProtocolUsenet: 200, SelectWeightHealth: 100, SelectSeedSaturation: 100,
	}
	set, err := settings.New(ctx, st, cfg)
	if err != nil {
		t.Fatal(err)
	}
	_ = set.Set(ctx, settings.KeyProwlarrURL, prowlarrSrv.URL)
	_ = set.Set(ctx, settings.KeyProwlarrAPIKey, "k")
	w := New(st, fake, set, testLogger())

	// A movie + an imported job with NO stored artifact (the dead-artifact case).
	mid, _ := st.CreateMovie(ctx, &media.Movie{TMDBID: 603, Title: "The Matrix", Year: 1999})
	id, _ := st.CreateJob(ctx, &job.Job{
		State: job.StateImported, Category: "movie", NZBName: "The.Matrix.1999",
		Protocol: "usenet", MediaType: "movie", MediaRef: mid, // no NZBContent/URL
	})
	j, _ := st.GetJob(ctx, id)

	w.startHeal(ctx, j, 1)

	got, _ := st.GetJob(ctx, id)
	if got.State != job.StateHealing {
		t.Fatalf("re-search should move the job to healing, got %s (err=%q)", got.State, got.LastHealError)
	}
	if string(got.NZBContent) != "<nzb>fresh</nzb>" || got.TorBoxID == 0 {
		t.Fatalf("job artifact not rewritten from re-search: content=%q tbid=%d", got.NZBContent, got.TorBoxID)
	}
	if len(fake.created) != 1 {
		t.Errorf("expected the fresh NZB to be resubmitted, got %d creates", len(fake.created))
	}
}

func TestHealNoFallbackWhenDisabled(t *testing.T) {
	ctx := context.Background()
	fake := &fakeTorBox{}
	w, st, _ := testWorkers(t, fake) // HealProwlarrFallback defaults false
	id, _ := st.CreateJob(ctx, &job.Job{
		State: job.StateImported, Category: "movie", NZBName: "X", Protocol: "usenet",
	})
	j, _ := st.GetJob(ctx, id)
	w.startHeal(ctx, j, 1)
	if got, _ := st.GetJob(ctx, id); got.State != job.StateHealFailed {
		t.Fatalf("with no artifact and fallback off, heal must fail, got %s", got.State)
	}
}
