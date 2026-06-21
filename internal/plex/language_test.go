package plex

import "testing"

func aud(id int, code string) Stream { return Stream{ID: id, StreamType: 2, LanguageCode: code} }
func sub(id int, code string) Stream { return Stream{ID: id, StreamType: 3, LanguageCode: code} }

func TestPickStreamsRanked(t *testing.T) {
	pref := []string{"DE", "EN"} // movies/series: German first, English fallback
	if a, s, m := PickStreams(pref, false, []Stream{aud(1, "eng"), aud(2, "deu")}, nil); a != 2 || s != 0 || m {
		t.Errorf("german dub: a=%d s=%d m=%v", a, s, m)
	}
	if a, s, m := PickStreams(pref, false, []Stream{aud(1, "eng")}, []Stream{sub(5, "ger"), sub(6, "eng")}); a != 1 || s != 5 || m {
		t.Errorf("eng dub+de sub: a=%d s=%d m=%v", a, s, m)
	}
	if a, s, m := PickStreams(pref, false, []Stream{aud(1, "eng")}, []Stream{sub(9, "fre")}); a != 1 || s != 0 || !m {
		t.Errorf("eng dub no de/en sub → missing: a=%d s=%d m=%v", a, s, m)
	}
	if _, _, m := PickStreams(pref, false, []Stream{aud(1, "jpn")}, []Stream{sub(2, "jpn")}); !m {
		t.Error("no de/en anywhere should be missing")
	}
}

func TestPickStreamsRequireAny(t *testing.T) {
	pref := []string{"DE", "EN"} // anime: either audio fine
	if a, s, m := PickStreams(pref, true, []Stream{aud(1, "eng")}, nil); a != 1 || s != 0 || m {
		t.Errorf("anime eng audio: a=%d s=%d m=%v", a, s, m)
	}
	if a, s, m := PickStreams(pref, true, []Stream{aud(1, "jpn")}, []Stream{sub(7, "eng")}); a != 0 || s != 7 || m {
		t.Errorf("anime jpn audio + en sub: a=%d s=%d m=%v", a, s, m)
	}
	if _, _, m := PickStreams(pref, true, []Stream{aud(1, "jpn")}, []Stream{sub(2, "jpn")}); !m {
		t.Error("anime jpn with no en/de subs should be missing")
	}
}

func TestPickStreamsNoGoal(t *testing.T) {
	if a, s, m := PickStreams(nil, false, []Stream{aud(1, "eng")}, nil); a != 0 || s != 0 || m {
		t.Errorf("no language goal → leave as-is: a=%d s=%d m=%v", a, s, m)
	}
}
