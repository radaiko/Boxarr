package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/radaiko/boxarr/internal/notify"
	"github.com/radaiko/boxarr/internal/plex"
	"github.com/radaiko/boxarr/internal/release"
)

// releaseNameForJob returns the NZB/release name of a linked job (for the
// language knowledge base), or "" if there's no job.
func (w *Workers) releaseNameForJob(ctx context.Context, jobID int64) string {
	if jobID == 0 {
		return ""
	}
	if j, err := w.store.GetJob(ctx, jobID); err == nil && j != nil {
		return j.NZBName
	}
	return ""
}

// PlexLanguageSweep runs one Plex auto-language pass on demand (manual button) —
// it bypasses the auto-sweep toggle.
func (w *Workers) PlexLanguageSweep(ctx context.Context) error { return w.plexLanguageOnce(ctx) }

// plexLanguageLoop is the periodic sweep, gated on the auto-language toggle.
func (w *Workers) plexLanguageLoop(ctx context.Context) error {
	if !w.set.PlexAutoLanguageEnabled() {
		return nil
	}
	return w.plexLanguageOnce(ctx)
}

// plexLanguageOnce sweeps the library and, per item, sets the default audio +
// subtitle streams in Plex to satisfy the language rules (German preferred for
// movies/series; German or English for anime, with subtitle fallback). When the
// wanted language can't be met it raises a 'language_missing' notification.
//
// It runs as an idempotent periodic sweep (no Plex webhooks without Plex Pass, so
// setting right after import would race the scan). Every Plex call is best-effort:
// per-item failures are logged and never abort the sweep.
func (w *Workers) plexLanguageOnce(ctx context.Context) error {
	if w.plex == nil || !w.set.PlexEnabled() {
		return nil
	}

	// Movies — one section fetch, then per-movie lookup.
	if sec := w.set.PlexMovieSection(); sec != "" {
		if items, err := w.plex.SectionItems(ctx, sec, 1); err == nil {
			byTitle := make(map[string]plex.LibItem, len(items))
			for _, it := range items {
				byTitle[strings.ToLower(it.Title)] = it
			}
			movies, _ := w.store.ListMovies(ctx)
			for _, m := range movies {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if !m.HasFile {
					continue
				}
				if it, ok := byTitle[strings.ToLower(m.Title)]; ok {
					mid := m.ID
					w.applyPlexLanguage(ctx, "movie", it.RatingKey, mid, m.Title, w.releaseNameForJob(ctx, m.JobID),
						func(miss bool) { _ = w.store.SetMovieLangMissing(ctx, mid, miss) })
				}
			}
		} else {
			w.logger.Debug("plex language: listing movie section", "error", err)
		}
	}

	// Series / anime — one show-index fetch per section, then allLeaves per show.
	showIndex := map[string]map[string]string{} // sectionID → lowerTitle → showRatingKey
	showsFor := func(sec string) map[string]string {
		if idx, ok := showIndex[sec]; ok {
			return idx
		}
		idx := map[string]string{}
		if shows, err := w.plex.SectionItems(ctx, sec, 2); err == nil {
			for _, s := range shows {
				idx[strings.ToLower(s.Title)] = s.RatingKey
			}
		} else {
			w.logger.Debug("plex language: listing show section", "section", sec, "error", err)
		}
		showIndex[sec] = idx
		return idx
	}

	series, _ := w.store.ListSeries(ctx)
	for _, sr := range series {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		kind, sec := "series", w.set.PlexTVSection()
		if sr.SeriesType == "anime" {
			kind = "anime"
			if a := w.set.PlexAnimeSection(); a != "" {
				sec = a
			}
		}
		if sec == "" {
			continue
		}
		eps, _ := w.store.ListEpisodes(ctx, sr.ID)
		anyFile := false
		for _, ep := range eps {
			if ep.HasFile {
				anyFile = true
				break
			}
		}
		if !anyFile {
			continue
		}
		showKey := showsFor(sec)[strings.ToLower(sr.Title)]
		if showKey == "" {
			continue // not scanned yet
		}
		leaves, err := w.plex.ShowEpisodes(ctx, showKey)
		if err != nil {
			continue
		}
		bySE := make(map[[2]int]plex.LibItem, len(leaves))
		for _, l := range leaves {
			bySE[[2]int{l.Season, l.Episode}] = l
		}
		for _, ep := range eps {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !ep.HasFile {
				continue
			}
			if it, ok := bySE[[2]int{ep.SeasonNumber, ep.EpisodeNumber}]; ok {
				eid := ep.ID
				w.applyPlexLanguage(ctx, kind, it.RatingKey, sr.ID,
					fmt.Sprintf("%s S%02dE%02d", sr.Title, ep.SeasonNumber, ep.EpisodeNumber),
					w.releaseNameForJob(ctx, ep.JobID),
					func(miss bool) { _ = w.store.SetEpisodeLangMissing(ctx, eid, miss) })
			}
		}
	}
	return nil
}

// applyPlexLanguage reads an item's streams, picks the right defaults, and (only
// when they differ) sets them; raises a notification when the language is missing.
func (w *Workers) applyPlexLanguage(ctx context.Context, kind, ratingKey string, navID int64, label, releaseName string, setMissing func(bool)) {
	partID, audio, subs, err := w.plex.ItemStreams(ctx, ratingKey)
	if err != nil {
		w.logger.Debug("plex language: streams", "item", label, "error", err)
		return // can't read streams — leave the flag as-is
	}
	cfg := w.set.SelectionConfigFor(kind)
	preferred := cfg.PreferredLanguages
	if len(preferred) == 0 {
		preferred = cfg.RequiredLanguages
	}
	wantA, wantS, missing := plex.PickStreams(preferred, cfg.RequireAnyLanguage, audio, subs)
	if setMissing != nil {
		setMissing(missing) // drives the in-list marker + re-search
	}
	// Record the verified languages into the knowledge base (release → real
	// audio/subtitle languages), keyed by the release name, for group-tendency
	// learning + a future shared service.
	if releaseName != "" {
		group := ""
		if p, perr := release.ParseRelease(releaseName); perr == nil && p != nil {
			group = p.Group
		}
		_ = w.store.UpsertReleaseLang(ctx, releaseName, group,
			plex.StreamLangCodes(audio), plex.StreamLangCodes(subs), "plex")
	}
	if missing {
		detail := fmt.Sprintf("wanted %s — available audio: %s; subtitles: %s",
			strings.Join(preferred, " / "), streamLangs(audio), streamLangs(subs))
		w.notifyLanguageMissing(ctx, kind, navID, label, detail)
	}
	curA, curS := plex.CurrentSelection(audio, subs)
	if (wantA == 0 || wantA == curA) && wantS == curS {
		return // already as desired — no PUT
	}
	audioToSet := wantA
	if audioToSet == 0 {
		audioToSet = curA // keep current audio when we don't choose one
	}
	if err := w.plex.SetDefaultStreams(ctx, partID, audioToSet, wantS); err != nil {
		w.logger.Warn("plex language: setting defaults", "item", label, "error", err)
		return
	}
	w.logger.Info("plex language set", "item", label, "audio_stream", audioToSet, "subtitle_stream", wantS)
}

// streamLangs lists the distinct languages present in a set of streams.
func streamLangs(ss []plex.Stream) string {
	var out []string
	seen := map[string]bool{}
	for _, s := range ss {
		l := s.LanguageCode
		if l == "" {
			l = s.Language
		}
		if l == "" {
			l = "?"
		}
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ", ")
}

// payloadItemEquals reports whether a notification payload's "item" field equals label.
func payloadItemEquals(payload, label string) bool {
	var m map[string]any
	if json.Unmarshal([]byte(payload), &m) != nil {
		return false
	}
	s, _ := m["item"].(string)
	return s == label
}

func (w *Workers) notifyLanguageMissing(ctx context.Context, kind string, catalogID int64, label, detail string) {
	existing, _ := w.store.ListNotifications(ctx, false, 500)
	for _, n := range existing {
		// Dedup on the payload "item" field — language_missing payloads key the
		// item there (not "remotePath", which containsName checks). Without this
		// the check never matched and every hourly sweep re-raised the same items.
		if n.Type == "language_missing" && payloadItemEquals(n.Payload, label) {
			return // already raised for this item
		}
	}
	payload, _ := json.Marshal(map[string]any{
		"item": label, "message": detail,
		"kind": kind, "catalogId": catalogID, // lets the UI jump to the item
		"actions": []string{"open"},
	})
	if _, err := w.store.EnqueueNotification(ctx, &notify.Notification{
		Type: "language_missing", Payload: string(payload),
	}); err != nil {
		w.logger.Error("plex language: enqueuing language_missing", "error", err)
	}
}
