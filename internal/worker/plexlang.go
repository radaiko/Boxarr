package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/radaiko/boxarr/internal/notify"
	"github.com/radaiko/boxarr/internal/plex"
)

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
					w.applyPlexLanguage(ctx, "movie", it.RatingKey, m.Title)
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
				w.applyPlexLanguage(ctx, kind, it.RatingKey,
					fmt.Sprintf("%s S%02dE%02d", sr.Title, ep.SeasonNumber, ep.EpisodeNumber))
			}
		}
	}
	return nil
}

// applyPlexLanguage reads an item's streams, picks the right defaults, and (only
// when they differ) sets them; raises a notification when the language is missing.
func (w *Workers) applyPlexLanguage(ctx context.Context, kind, ratingKey, label string) {
	partID, audio, subs, err := w.plex.ItemStreams(ctx, ratingKey)
	if err != nil {
		w.logger.Debug("plex language: streams", "item", label, "error", err)
		return
	}
	wantA, wantS, missing := plex.PickStreams(kind, audio, subs)
	if missing {
		w.notifyLanguageMissing(ctx, label)
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

func (w *Workers) notifyLanguageMissing(ctx context.Context, label string) {
	existing, _ := w.store.ListNotifications(ctx, false, 500)
	for _, n := range existing {
		if n.Type == "language_missing" && containsName(n.Payload, label) {
			return // already raised
		}
	}
	payload, _ := json.Marshal(map[string]any{
		"item": label, "message": "No German/English audio or subtitles available for " + label,
	})
	if _, err := w.store.EnqueueNotification(ctx, &notify.Notification{
		Type: "language_missing", Payload: string(payload),
	}); err != nil {
		w.logger.Error("plex language: enqueuing language_missing", "error", err)
	}
}
