package release

import (
	"regexp"
	"strings"
)

// langTokens maps scene language markers to ISO-ish display codes.
var langTokens = map[string]string{
	"german": "DE", "ger": "DE", "deutsch": "DE",
	"english": "EN", "eng": "EN",
	"french": "FR", "fre": "FR", "truefrench": "FR", "vff": "FR", "vof": "FR", "vostfr": "FR",
	"spanish": "ES", "spa": "ES", "esp": "ES", "castellano": "ES", "latino": "ES",
	"italian": "IT", "ita": "IT",
	"dutch": "NL", "nld": "NL",
	"japanese": "JA", "jpn": "JA", "jap": "JA",
	"korean": "KO", "kor": "KO",
	"russian": "RU", "rus": "RU",
	"portuguese": "PT", "por": "PT",
	"chinese": "ZH", "chi": "ZH", "cantonese": "ZH",
	"multi": "MULTI",
}

var tokenSplit = regexp.MustCompile(`[^a-z0-9]+`)

// DetectLanguages extracts spoken-language hints from a release name by scanning
// its tokens for scene language markers (best-effort; ffprobe later gives the
// exact audio tracks). "DL" (dual language) alongside German implies English too.
func DetectLanguages(name string) []string {
	toks := tokenSplit.Split(strings.ToLower(name), -1)
	var out []string
	seen := map[string]bool{}
	add := func(code string) {
		if code != "" && !seen[code] {
			seen[code] = true
			out = append(out, code)
		}
	}
	hasDL := false
	for _, t := range toks {
		if t == "dl" {
			hasDL = true
		}
		if c, ok := langTokens[t]; ok {
			add(c)
		}
	}
	// "German DL" dual-audio convention pairs the primary language with English.
	if hasDL && seen["DE"] && !seen["EN"] {
		add("EN")
	}
	return out
}

// subMarkers signal embedded subtitles in a release name (English-ish; VOSTFR is
// French subs and deliberately excluded).
var subMarkers = []string{"esub", "esubs", "engsub", "engsubs", "english.sub", "eng.sub", "multisub", "multi.sub", "subbed"}

// HasEnglishSubs best-effort reports whether a release name signals English (or
// multi) subtitles. Filename subtitle info is unreliable, so this is only used to
// prefer — never to reject. ffprobe can confirm exact tracks later.
func HasEnglishSubs(name string) bool {
	s := strings.ToLower(name)
	for _, m := range subMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}
