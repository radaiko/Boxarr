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

// langMatch reports whether a stream is in the wanted language. want is a 2-letter
// code ("de"/"en"); Plex emits languageCode as de/deu/ger or en/eng, so match
// loosely on the code and the human name.
func langMatch(s Stream, want string) bool {
	code := strings.ToLower(s.LanguageCode)
	name := strings.ToLower(s.Language)
	switch want {
	case "de":
		return code == "de" || code == "deu" || code == "ger" || strings.Contains(name, "german") || strings.Contains(name, "deutsch")
	case "en":
		return code == "en" || code == "eng" || strings.Contains(name, "english")
	}
	return false
}

// firstLang returns the id of the first stream matching want, or 0.
func firstLang(ss []Stream, want string) int {
	for _, s := range ss {
		if langMatch(s, want) {
			return s.ID
		}
	}
	return 0
}

// PickStreams chooses the default audio + subtitle stream ids for an item per the
// language rules, returning missing=true when the wanted language can't be met
// (so the caller can raise a 'language missing' notification). subID 0 means
// "no subtitle"; audioID 0 means "leave the default audio".
//
// Movies/series: German audio preferred; else English audio + German subs (or
// English subs) since the dub isn't German. Anime: German OR English audio is
// equally fine; otherwise at least English (or German) subtitles.
func PickStreams(kind string, audio, subs []Stream) (audioID, subID int, missing bool) {
	deA, enA := firstLang(audio, "de"), firstLang(audio, "en")
	deS, enS := firstLang(subs, "de"), firstLang(subs, "en")

	if kind == "anime" {
		switch {
		case deA != 0:
			return deA, 0, false
		case enA != 0:
			return enA, 0, false
		case enS != 0:
			return 0, enS, false
		case deS != 0:
			return 0, deS, false
		default:
			return 0, 0, true
		}
	}

	// Movies / series — German is the target.
	if deA != 0 {
		return deA, 0, false // German dub, no subtitles needed
	}
	if enA != 0 {
		switch { // English dub → add German (else English) subtitles
		case deS != 0:
			return enA, deS, false
		case enS != 0:
			return enA, enS, false
		default:
			return enA, 0, true // English audio but no German/English subs
		}
	}
	// No German/English audio at all — rely on subtitles.
	switch {
	case deS != 0:
		return 0, deS, false
	case enS != 0:
		return 0, enS, false
	default:
		return 0, 0, true
	}
}
