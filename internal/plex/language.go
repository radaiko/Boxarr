package plex

import "strings"

// Stream is one audio or subtitle track of a library item.
type Stream struct {
	ID           int    `json:"id"`
	StreamType   int    `json:"streamType"` // 1=video, 2=audio, 3=subtitle
	LanguageCode string `json:"languageCode"`
	Language     string `json:"language"`
	Selected     bool   `json:"selected"`
}

// langAliases maps a 2-letter language code to the variants Plex may emit.
var langAliases = map[string][]string{
	"de": {"de", "deu", "ger", "german", "deutsch"},
	"en": {"en", "eng", "english"},
}

// langMatch reports whether a stream is in the wanted language (a 2-letter code).
// Plex emits languageCode as de/deu/ger or en/eng, so match loosely; unknown
// codes fall back to a prefix/name match.
func langMatch(s Stream, want string) bool {
	want = strings.ToLower(want)
	code := strings.ToLower(s.LanguageCode)
	name := strings.ToLower(s.Language)
	if al, ok := langAliases[want]; ok {
		for _, a := range al {
			if code == a || strings.Contains(name, a) {
				return true
			}
		}
		return false
	}
	return code == want || strings.HasPrefix(code, want) || strings.Contains(name, want)
}

// firstPreferred returns the id of the first stream whose language is highest in
// the preferred order (so preferred[0] wins over preferred[1]); 0 if none match.
func firstPreferred(ss []Stream, preferred []string) int {
	for _, want := range preferred {
		for _, s := range ss {
			if langMatch(s, want) {
				return s.ID
			}
		}
	}
	return 0
}

// PickStreams chooses the default audio + subtitle stream ids for an item from
// the configured preferred languages, returning missing=true when the wanted
// language can't be met. subID 0 means "no subtitle"; audioID 0 means "leave the
// default audio". Behaviour derives entirely from settings:
//
//   - requireAny (e.g. anime "any one is enough"): any preferred-language audio is
//     fine with no subtitles; otherwise a preferred-language subtitle; else missing.
//   - ranked (movies/series): preferred[0] audio is ideal (no subs); when it's
//     absent we keep the best available audio and add a preferred subtitle, or
//     flag missing if no preferred subtitle exists either.
func PickStreams(preferred []string, requireAny bool, audio, subs []Stream) (audioID, subID int, missing bool) {
	if len(preferred) == 0 {
		return 0, 0, false // no language goal configured → leave Plex as-is
	}
	if requireAny {
		if a := firstPreferred(audio, preferred); a != 0 {
			return a, 0, false
		}
		if s := firstPreferred(subs, preferred); s != 0 {
			return 0, s, false
		}
		return 0, 0, true
	}
	// Ranked: preferred[0] is the ideal dub.
	if a := firstPreferred(audio, preferred[:1]); a != 0 {
		return a, 0, false
	}
	// Ideal audio absent — keep the best available preferred audio (e.g. the
	// English fallback) and add a preferred subtitle in priority order.
	a := firstPreferred(audio, preferred)
	if s := firstPreferred(subs, preferred); s != 0 {
		return a, s, false
	}
	return a, 0, true // no preferred subtitle either → ideal language unreachable
}
