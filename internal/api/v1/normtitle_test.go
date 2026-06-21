package v1

import "testing"

func TestNormTitle(t *testing.T) {
	cases := []struct{ a, b string }{
		{"Avengers Endgame", "Avengers: Endgame"},
		{"Spider-Man", "Spider Man"},
		{"WALL·E", "WALL E"},
		{"The  Matrix", "the matrix"},
	}
	for _, c := range cases {
		if normTitle(c.a) != normTitle(c.b) {
			t.Errorf("normTitle(%q)=%q != normTitle(%q)=%q", c.a, normTitle(c.a), c.b, normTitle(c.b))
		}
	}
	// Genuinely different titles must NOT collide.
	if normTitle("Anaconda") == normTitle("Apex") {
		t.Error("distinct titles collided")
	}
}
