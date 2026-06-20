// Package release parses scene/torrent release names into a unified result used
// for selection scoring and season-pack→episode mapping. It is pure Go (no
// Python/JS sidecar): chill-institute/torrentname is the primary parser,
// nssteinbrenner/anitogo supplements anime, and three in-house regexes cover
// gaps (adjacent multi-episode, daily-show dates, bare season packs).
package release

import (
	"regexp"
	"strconv"
	"time"

	"github.com/chill-institute/torrentname"
	"github.com/nssteinbrenner/anitogo"
)

// ParsedRelease is Boxarr's unified parse result.
type ParsedRelease struct {
	Title            string
	Year             int
	SeasonNumber     int
	EpisodeStart     int    // 0 = unknown / season-pack
	EpisodeEnd       int    // 0 = single; >0 = range/adjacent end
	AbsoluteEpisodes []int  // anime absolute numbering
	AirDate          string // "YYYY-MM-DD" for daily shows, else ""
	IsSeasonPack     bool
	IsAnime          bool
	Quality          string
	Resolution       string
	Codec            string
	HDR              string
	Audio            string
	Group            string
	Source           string
	Proper           bool
	Repack           bool
}

var (
	// Daily show: "The.Daily.Show.2024.01.15" or "...2024-01-15".
	reDailyDate = regexp.MustCompile(
		`(?i)(?:19|20)\d{2}[-_. ]+(?:0\d|1[0-2])[-_. ]+(?:[0-2]\d|3[01])(?:[^0-9]|$)`)
	reDailyParts = regexp.MustCompile(
		`(?i)((?:19|20)\d{2})[-_. ]+(0\d|1[0-2])[-_. ]+([0-2]\d|3[01])`)
	// Adjacent multi-episode with no dash, e.g. S01E01E02 (>=2 E groups).
	reAdjacentMultiEp = regexp.MustCompile(`(?i)S(\d{1,2})((?:E\d{2,3}){2,})`)
	reEpNum           = regexp.MustCompile(`(?i)E(\d{2,3})`)
	// Bare season pack with no COMPLETE keyword, e.g. "Show.S01.1080p.BluRay".
	reBareSeasonPack = regexp.MustCompile(`(?i)\bS(\d{1,2})\b(?:[^E0-9]|$)`)
	// Anime fansub prefix, e.g. "[HorribleSubs] ...".
	reAnimeGroupPrefix = regexp.MustCompile(`^\s*\[`)
)

// ParseRelease parses a release name (or a single file name) into a ParsedRelease.
func ParseRelease(filename string) (*ParsedRelease, error) {
	info, err := torrentname.Parse(filename)
	if err != nil {
		return nil, err
	}
	out := &ParsedRelease{
		Title:        info.Title,
		Year:         info.Year,
		SeasonNumber: info.Season,
		EpisodeStart: info.Episode,
		EpisodeEnd:   info.EpisodeEnd,
		IsSeasonPack: info.Complete,
		Quality:      info.Quality,
		Resolution:   info.Resolution,
		Codec:        info.Codec,
		HDR:          info.HDR,
		Audio:        info.Audio,
		Group:        info.Group,
		Source:       info.Source,
		Proper:       info.Proper,
		Repack:       info.Repack,
	}

	// 1) Adjacent multi-episode (chill-institute gap): S01E01E02.
	if m := reAdjacentMultiEp.FindStringSubmatch(filename); m != nil {
		eps := reEpNum.FindAllStringSubmatch(m[2], -1)
		if len(eps) >= 2 {
			if out.SeasonNumber == 0 {
				out.SeasonNumber, _ = strconv.Atoi(m[1])
			}
			first, _ := strconv.Atoi(eps[0][1])
			last, _ := strconv.Atoi(eps[len(eps)-1][1])
			out.EpisodeStart, out.EpisodeEnd = first, last
		}
	}

	// 2) Daily-show air date (chill-institute gap): YYYY-MM-DD.
	if reDailyDate.MatchString(filename) {
		if p := reDailyParts.FindStringSubmatch(filename); p != nil {
			candidate := p[1] + "-" + p[2] + "-" + p[3]
			if _, perr := time.Parse("2006-01-02", candidate); perr == nil {
				out.AirDate = candidate
			}
		}
	}

	// 3) Bare season pack with no COMPLETE keyword.
	if !out.IsSeasonPack && out.EpisodeStart == 0 && out.AirDate == "" {
		if m := reBareSeasonPack.FindStringSubmatch(filename); m != nil {
			out.IsSeasonPack = true
			if out.SeasonNumber == 0 {
				out.SeasonNumber, _ = strconv.Atoi(m[1])
			}
		}
	}

	// 4) Anime supplement (fansub [Group] prefix or a high bare episode number).
	if reAnimeGroupPrefix.MatchString(filename) || (out.SeasonNumber == 0 && out.EpisodeStart > 99) {
		out.IsAnime = true
		el := anitogo.Parse(filename, anitogo.DefaultOptions)
		if el != nil {
			if out.Title == "" && el.AnimeTitle != "" {
				out.Title = el.AnimeTitle
			}
			for _, s := range el.EpisodeNumber {
				if n, convErr := strconv.Atoi(s); convErr == nil {
					out.AbsoluteEpisodes = append(out.AbsoluteEpisodes, n)
				}
			}
			if out.SeasonNumber == 0 && len(el.AnimeSeason) > 0 {
				if n, convErr := strconv.Atoi(el.AnimeSeason[0]); convErr == nil {
					out.SeasonNumber = n
				}
			}
		}
	}

	return out, nil
}
