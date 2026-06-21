package plex

import "testing"

func aud(id int, code string) Stream { return Stream{ID: id, StreamType: 2, LanguageCode: code} }
func sub(id int, code string) Stream { return Stream{ID: id, StreamType: 3, LanguageCode: code} }

func TestPickStreamsMovie(t *testing.T) {
	// German dub present → German audio, no subs.
	if a, s, m := PickStreams("movie", []Stream{aud(1, "eng"), aud(2, "deu")}, nil); a != 2 || s != 0 || m {
		t.Errorf("german dub: a=%d s=%d m=%v", a, s, m)
	}
	// English dub + German subs → English audio + German subs.
	if a, s, m := PickStreams("movie", []Stream{aud(1, "eng")}, []Stream{sub(5, "ger"), sub(6, "eng")}); a != 1 || s != 5 || m {
		t.Errorf("eng dub+de sub: a=%d s=%d m=%v", a, s, m)
	}
	// English dub, no German/English subs → set English audio, flag missing.
	if a, s, m := PickStreams("movie", []Stream{aud(1, "eng")}, []Stream{sub(9, "fre")}); a != 1 || s != 0 || !m {
		t.Errorf("eng dub no de/en sub: a=%d s=%d m=%v", a, s, m)
	}
	// Nothing German/English at all → missing.
	if _, _, m := PickStreams("movie", []Stream{aud(1, "jpn")}, []Stream{sub(2, "jpn")}); !m {
		t.Error("no de/en anywhere should be missing")
	}
}

func TestPickStreamsAnime(t *testing.T) {
	// English audio is fine for anime (DE==EN).
	if a, s, m := PickStreams("anime", []Stream{aud(1, "eng")}, nil); a != 1 || s != 0 || m {
		t.Errorf("anime eng audio: a=%d s=%d m=%v", a, s, m)
	}
	// Japanese audio → at least English subs.
	if a, s, m := PickStreams("anime", []Stream{aud(1, "jpn")}, []Stream{sub(7, "eng")}); a != 0 || s != 7 || m {
		t.Errorf("anime jpn audio + en sub: a=%d s=%d m=%v", a, s, m)
	}
	// Japanese audio, no en/de subs → missing.
	if _, _, m := PickStreams("anime", []Stream{aud(1, "jpn")}, []Stream{sub(2, "jpn")}); !m {
		t.Error("anime jpn with no en/de subs should be missing")
	}
}
