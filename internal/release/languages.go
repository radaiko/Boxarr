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
