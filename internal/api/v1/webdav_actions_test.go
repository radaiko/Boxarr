package v1

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/settings"
	"github.com/radaiko/boxarr/internal/webdav"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type fakeDeleter struct{ removed []int64 }

func (f *fakeDeleter) RemoveImport(_ context.Context, jobID int64) {
	f.removed = append(f.removed, jobID)
}

func TestDeleteTrackedItemTearsDownImport(t *testing.T) {
	st := mkStore(t)
	ctx := context.Background()
	tb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
	}))
	defer tb.Close()
	mountRoot := t.TempDir()
	rel := filepath.Join(mountRoot, "Tracked.Show.S01E01")
	if err := os.MkdirAll(rel, 0o755); err != nil {
		t.Fatal(err)
	}
	// A tracked item carrying a real job id — deletion must route through RemoveImport.
	jid, err := st.CreateJob(ctx, &job.Job{State: job.StateImported, NZBName: "Tracked.Show.S01E01", MediaType: "series"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{
		Name: "Tracked.Show.S01E01", RemotePath: rel, Category: "series", Known: true, JobID: jid}); err != nil {
		t.Fatal(err)
	}
	it, _ := st.GetWebDAVItemByPath(ctx, rel)

	set := mkSettings(t, st, &config.Config{})
	_ = set.Set(ctx, settings.KeyWebDAVMountRoot, mountRoot)
	_ = set.Set(ctx, settings.KeyTorBoxToken, "x")
	_ = set.Set(ctx, settings.KeyTorBoxBaseURL, tb.URL)
	del := &fakeDeleter{}
	h := NewHandler(Deps{Store: st, Settings: set, Deleter: del, Logger: discardLog()}).Router()

	rec := req(t, h, http.MethodPost, "/webdav/delete", "", "127.0.0.1:1", `{"ids":[`+itoa(it.ID)+`]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(del.removed) != 1 || del.removed[0] != jid {
		t.Fatalf("expected RemoveImport(%d), got %v", jid, del.removed)
	}
	if _, err := os.Stat(rel); !os.IsNotExist(err) {
		t.Error("the mount folder should still be removed")
	}
}

func TestAdoptWebDAVUsesRequestedKind(t *testing.T) {
	st := mkStore(t)
	ctx := context.Background()
	set := mkSettings(t, st, &config.Config{})
	_ = st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{
		Name: "Frieren.S01E12.1080p.WEB.x264", RemotePath: "/mnt/torbox/Frieren.S01E12", Category: "series"})
	it, _ := st.GetWebDAVItemByPath(ctx, "/mnt/torbox/Frieren.S01E12")
	ad := &fakeAdopter{}
	h := NewHandler(Deps{Store: st, Settings: set, Adopter: ad, Logger: discardLog()}).Router()

	rec := req(t, h, http.MethodPost, "/webdav/"+itoa(it.ID)+"/adopt", "", "127.0.0.1:1", `{"kind":"anime"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("adopt = %d body=%s", rec.Code, rec.Body.String())
	}
	if !ad.called || ad.kind != "anime" || ad.name != "Frieren.S01E12.1080p.WEB.x264" {
		t.Fatalf("adopter called=%v kind=%q name=%q", ad.called, ad.kind, ad.name)
	}
}

func TestAdoptWebDAVAutoKindFromName(t *testing.T) {
	st := mkStore(t)
	ctx := context.Background()
	set := mkSettings(t, st, &config.Config{})
	_ = st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{
		Name: "The.Matrix.1999.1080p.BluRay.x264-GRP", RemotePath: "/mnt/torbox/Matrix", Category: "movie"})
	it, _ := st.GetWebDAVItemByPath(ctx, "/mnt/torbox/Matrix")
	ad := &fakeAdopter{}
	h := NewHandler(Deps{Store: st, Settings: set, Adopter: ad, Logger: discardLog()}).Router()

	rec := req(t, h, http.MethodPost, "/webdav/"+itoa(it.ID)+"/adopt", "", "127.0.0.1:1", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("adopt = %d body=%s", rec.Code, rec.Body.String())
	}
	if ad.kind != "movie" {
		t.Fatalf("auto kind = %q, want movie", ad.kind)
	}
}

func TestDeleteWebDAVBatchRemovesFoldersAndRows(t *testing.T) {
	st := mkStore(t)
	ctx := context.Background()
	tb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":[]}`))
	}))
	defer tb.Close()

	mountRoot := t.TempDir()
	var ids []string
	for _, name := range []string{"Show.S01E01", "Show.S01E02"} {
		rel := filepath.Join(mountRoot, name)
		if err := os.MkdirAll(rel, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = st.UpsertWebDAVItem(ctx, &webdav.WebDAVItem{Name: name, RemotePath: rel, Category: "series"})
		it, _ := st.GetWebDAVItemByPath(ctx, rel)
		ids = append(ids, itoa(it.ID))
	}
	set := mkSettings(t, st, &config.Config{})
	_ = set.Set(ctx, settings.KeyWebDAVMountRoot, mountRoot)
	_ = set.Set(ctx, settings.KeyTorBoxToken, "x")
	_ = set.Set(ctx, settings.KeyTorBoxBaseURL, tb.URL)
	h := NewHandler(Deps{Store: st, Settings: set, Logger: discardLog()}).Router()

	rec := req(t, h, http.MethodPost, "/webdav/delete", "", "127.0.0.1:1", `{"ids":[`+ids[0]+`,`+ids[1]+`]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d body=%s", rec.Code, rec.Body.String())
	}
	left, _ := st.ListWebDAVItems(ctx)
	if len(left) != 0 {
		t.Errorf("expected all rows removed, %d left", len(left))
	}
	if entries, _ := os.ReadDir(mountRoot); len(entries) != 0 {
		t.Errorf("expected mount folders removed, %d left", len(entries))
	}
}
