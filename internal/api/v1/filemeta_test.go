package v1

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/radaiko/boxarr/internal/config"
	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/media"
)

func TestGetMovieIncludesFileMetadata(t *testing.T) {
	st := mkStore(t)
	ctx := context.Background()
	set := mkSettings(t, st, &config.Config{})

	link := "/data/movies/The Matrix (1999)/The Matrix (1999).mkv"
	mid, err := st.CreateMovie(ctx, &media.Movie{
		TMDBID: 603, Title: "The Matrix", Year: 1999, Status: media.MediaAvailable,
		HasFile: true, LibraryPath: link,
	})
	if err != nil {
		t.Fatal(err)
	}
	jid, _ := st.CreateJob(ctx, &job.Job{State: job.StateImported, NZBName: "x", MediaType: "movie"})
	if err := st.UpsertImportedSymlink(ctx, &job.ImportedSymlink{
		JobID: jid, SymlinkPath: link,
		TargetPath: "/mnt/torbox/usenet/The.Matrix.1999.German.DL.2160p.UHD.BluRay.HDR.x265-GRP/movie.mkv",
	}); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(Deps{Store: st, Settings: set, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).Router()

	rec := req(t, h, http.MethodGet, "/movies/"+itoa(mid), "", "127.0.0.1:1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get movie = %d", rec.Code)
	}
	var resp struct {
		File *fileMeta `json:"file"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.File == nil {
		t.Fatal("expected file metadata, got none")
	}
	if resp.File.Resolution != "2160p" || resp.File.Codec == "" ||
		(resp.File.Source == "" && resp.File.Quality == "") || resp.File.DynamicRange == "" {
		t.Errorf("parsed fields off: %+v", resp.File)
	}
	hasDE, hasEN := false, false
	for _, l := range resp.File.Languages {
		hasDE = hasDE || l == "DE"
		hasEN = hasEN || l == "EN"
	}
	if !hasDE || !hasEN {
		t.Errorf("languages = %v, want DE+EN (German DL)", resp.File.Languages)
	}
}
