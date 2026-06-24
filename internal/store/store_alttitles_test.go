package store

import (
	"context"
	"testing"

	"github.com/radaiko/boxarr/internal/media"
)

func TestMovieAltTitlesRoundtrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	id, err := st.CreateMovie(ctx, &media.Movie{TMDBID: 671, Title: "Harry Potter and the Prisoner of Azkaban"})
	if err != nil {
		t.Fatal(err)
	}
	alts := []string{"Harry Potter und der Gefangene von Askaban", "Harry Potter 3"}
	if err := st.SetMovieAltTitles(ctx, id, alts); err != nil {
		t.Fatal(err)
	}
	m, err := st.GetMovie(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.AltTitles) != 2 || m.AltTitles[0] != alts[0] {
		t.Fatalf("alt titles not persisted: %#v", m.AltTitles)
	}
}
