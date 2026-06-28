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

func TestLanguageAvailable(t *testing.T) {
	// Required DE present as audio → available even without the preferred EN.
	if !LanguageAvailable([]string{"DE"}, []Stream{aud(1, "deu")}, nil) {
		t.Error("german audio should satisfy required DE")
	}
	// Required DE present only as a subtitle → still available.
	if !LanguageAvailable([]string{"DE"}, []Stream{aud(1, "eng")}, []Stream{sub(2, "ger")}) {
		t.Error("german subtitle should satisfy required DE")
	}
	// Required DE absent everywhere → not available.
	if LanguageAvailable([]string{"DE"}, []Stream{aud(1, "eng")}, []Stream{sub(2, "fre")}) {
		t.Error("no german anywhere → not available")
	}
	// Nothing required → always available.
	if !LanguageAvailable(nil, []Stream{aud(1, "jpn")}, nil) {
		t.Error("no required langs → available")
	}
}

func TestPickStreamsForRequiredPresentPreferredAbsent(t *testing.T) {
	// movie rules: required DE, preferred EN backup. German dub present, no English.
	a, _, m := PickStreamsFor([]string{"DE"}, []string{"EN"}, false, []Stream{aud(1, "deu")}, nil)
	if m {
		t.Error("required DE present → must NOT be language-missing when only the preferred EN backup is absent")
	}
	if a != 1 {
		t.Errorf("should default to the german audio (id 1); got %d", a)
	}
}

func TestPickStreamsForPrefersPreferredWhenPresent(t *testing.T) {
	// English present → default to the preferred English track, not missing.
	a, _, m := PickStreamsFor([]string{"DE"}, []string{"EN"}, false,
		[]Stream{aud(1, "deu"), aud(2, "eng")}, nil)
	if m || a != 2 {
		t.Errorf("preferred EN present → default to id 2, not missing; got a=%d m=%v", a, m)
	}
}

func TestPickStreamsForRequiredAbsentIsMissing(t *testing.T) {
	_, _, m := PickStreamsFor([]string{"DE"}, []string{"EN"}, false,
		[]Stream{aud(1, "fre")}, []Stream{sub(2, "fre")})
	if !m {
		t.Error("no german (required) anywhere → language-missing")
	}
}

func TestPickStreamsForNoRequiredFallsBackToPreferred(t *testing.T) {
	// When no required languages are configured, the preferred set gates "missing".
	_, _, m := PickStreamsFor(nil, []string{"EN"}, false, []Stream{aud(1, "jpn")}, nil)
	if !m {
		t.Error("no required configured, preferred EN absent → missing")
	}
}
