package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/media"
)

func TestGrabTorrentCreatesJobAndDedups(t *testing.T) {
	h, st := newV1Cat(t)
	ctx := context.Background()
	mid, _ := st.CreateMovie(ctx, &media.Movie{TMDBID: 603, Title: "The Matrix", Monitored: true, Status: media.MediaWanted})

	rid := encodeReleaseID(grabRef{Protocol: "torrent", MagnetURL: "magnet:?xt=urn:btih:abc",
		InfoHash: "ABC", Title: "The.Matrix.1999.1080p"})

	rec := req(t, h, http.MethodPost, "/movies/"+itoa(mid)+"/grab", "", "127.0.0.1:1",
		`{"releaseId":"`+rid+`"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("grab: %d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		JobID   int64  `json:"jobId"`
		State   string `json:"state"`
		Deduped bool   `json:"deduped"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.JobID == 0 || got.State != "pending" || got.Deduped {
		t.Fatalf("grab result: %+v", got)
	}
	j, _ := st.GetJob(ctx, got.JobID)
	if j.Protocol != "torrent" || j.TorrentHash != "abc" || j.MediaType != "movie" || j.MediaRef != mid {
		t.Fatalf("job not linked correctly: %+v", j)
	}
	// Movie flips to queued (a job is on TorBox) and links the job.
	m, _ := st.GetMovie(ctx, mid)
	if m.Status != media.MediaQueued || m.JobID != got.JobID {
		t.Fatalf("movie not linked: status=%s jobID=%d", m.Status, m.JobID)
	}
	// Re-grab dedups to the same job.
	rec = req(t, h, http.MethodPost, "/movies/"+itoa(mid)+"/grab", "", "127.0.0.1:1", `{"releaseId":"`+rid+`"}`)
	var dup struct {
		JobID   int64 `json:"jobId"`
		Deduped bool  `json:"deduped"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &dup)
	if dup.JobID != got.JobID || !dup.Deduped {
		t.Fatalf("re-grab should dedup to same job: %+v", dup)
	}
}

func TestGrabUsenetFetchesAndStoresNZB(t *testing.T) {
	h, st := newV1Cat(t)
	ctx := context.Background()
	mid, _ := st.CreateMovie(ctx, &media.Movie{TMDBID: 11, Title: "Star Wars", Monitored: true, Status: media.MediaWanted})

	nzbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<nzb>star wars</nzb>"))
	}))
	defer nzbSrv.Close()

	rid := encodeReleaseID(grabRef{Protocol: "usenet", DownloadURL: nzbSrv.URL + "/x.nzb", Title: "Star.Wars.1977"})
	rec := req(t, h, http.MethodPost, "/movies/"+itoa(mid)+"/grab", "", "127.0.0.1:1", `{"releaseId":"`+rid+`"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("grab usenet: %d body=%s", rec.Code, rec.Body.String())
	}
	jobs, _ := st.JobsByState(ctx, job.StatePending)
	if len(jobs) != 1 || jobs[0].Protocol != "usenet" || string(jobs[0].NZBContent) != "<nzb>star wars</nzb>" || jobs[0].NZBSHA256 == "" {
		t.Fatalf("usenet job artifact not stored: %+v", jobs)
	}
}
