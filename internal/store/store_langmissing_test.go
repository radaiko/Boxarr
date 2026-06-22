package store

import (
	"context"
	"testing"

	"github.com/radaiko/boxarr/internal/media"
)

func TestSetMovieLangMissing(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	mid, err := st.CreateMovie(ctx, &media.Movie{TMDBID: 603, Title: "The Matrix", Monitored: true})
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := st.GetMovie(ctx, mid); m.LangMissing {
		t.Fatal("new movie should not be lang_missing")
	}
	if err := st.SetMovieLangMissing(ctx, mid, true); err != nil {
		t.Fatal(err)
	}
	if m, _ := st.GetMovie(ctx, mid); !m.LangMissing {
		t.Fatal("movie lang_missing should be true")
	}
	_ = st.SetMovieLangMissing(ctx, mid, false)
	if m, _ := st.GetMovie(ctx, mid); m.LangMissing {
		t.Fatal("movie lang_missing should be cleared")
	}
}
