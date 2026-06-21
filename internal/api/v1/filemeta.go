package v1

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/radaiko/boxarr/internal/release"
)

// fileMeta describes the downloaded file behind a library item, parsed from the
// release name (filename-based now; ffprobe enrichment can fill exact tracks later).
type fileMeta struct {
	Name         string   `json:"name"` // release folder name (what was downloaded)
	Path         string   `json:"path,omitempty"`
	Resolution   string   `json:"resolution,omitempty"`
	Source       string   `json:"source,omitempty"`
	Codec        string   `json:"codec,omitempty"`
	DynamicRange string   `json:"dynamicRange,omitempty"`
	Audio        string   `json:"audio,omitempty"`
	Languages    []string `json:"languages,omitempty"`
	Group        string   `json:"group,omitempty"`
	Quality      string   `json:"quality,omitempty"`
}

// symlinkTargets maps each library symlink path → the real downloaded file it
// points at (from imported_symlinks), fetched once per request.
func (h *Handler) symlinkTargets(ctx context.Context) map[string]string {
	syms, err := h.deps.Store.ListImportedSymlinks(ctx)
	if err != nil {
		return nil
	}
	m := make(map[string]string, len(syms))
	for _, s := range syms {
		m[s.SymlinkPath] = s.TargetPath
	}
	return m
}

// fileMetaFromTarget parses release metadata from a downloaded file's path. It
// reads both the release folder and the file name and keeps the richer field of
// the two (the folder usually carries the full scene name).
func fileMetaFromTarget(symlinkPath, targetPath string) *fileMeta {
	if targetPath == "" {
		return nil
	}
	dirName := filepath.Base(filepath.Dir(targetPath))
	fileName := filepath.Base(targetPath)
	var d, f release.ParsedRelease
	if p, err := release.ParseRelease(dirName); err == nil && p != nil {
		d = *p
	}
	if p, err := release.ParseRelease(fileName); err == nil && p != nil {
		f = *p
	}
	pick := func(a, b string) string {
		if a != "" {
			return a
		}
		return b
	}
	fm := &fileMeta{
		Name:         dirName,
		Path:         symlinkPath,
		Resolution:   pick(d.Resolution, f.Resolution),
		Source:       pick(d.Source, f.Source),
		Codec:        pick(d.Codec, f.Codec),
		DynamicRange: pick(d.HDR, f.HDR),
		Audio:        pick(d.Audio, f.Audio),
		Group:        pick(d.Group, f.Group),
		Quality:      pick(d.Quality, f.Quality),
	}
	if fm.Languages = release.DetectLanguages(dirName); len(fm.Languages) == 0 {
		fm.Languages = release.DetectLanguages(fileName)
	}
	return fm
}

// parseReleaseMeta parses a release name (e.g. a job's NZB name) into display
// metadata — used to show what release a download queue entry picked.
func parseReleaseMeta(name string) *fileMeta {
	if name == "" {
		return nil
	}
	var p release.ParsedRelease
	if pr, err := release.ParseRelease(name); err == nil && pr != nil {
		p = *pr
	}
	langs := release.DetectLanguages(name)
	if len(langs) == 0 && hasEnglishSource(name) {
		// Display-only: releases from English-origin streaming services rarely
		// carry a language tag; surface EN so the queue isn't blank. (Selection's
		// DetectLanguages is left untouched so grab decisions don't change.)
		langs = []string{"EN"}
	}
	return &fileMeta{
		Name: name, Resolution: p.Resolution, Source: p.Source, Codec: p.Codec,
		DynamicRange: p.HDR, Audio: p.Audio, Group: p.Group, Quality: p.Quality,
		Languages: langs,
	}
}

// englishSourceTokens are streaming services whose releases are English unless
// tagged otherwise — used only to infer a display language when none is declared.
var englishSourceTokens = []string{
	"amzn", "nf", "dsnp", "hmax", "max", "hulu", "atvp", "pcok", "stan", "ma",
	"cr", "crunchyroll", "funi", "hdtv", "ip", "red", "roku",
}

func hasEnglishSource(name string) bool {
	low := strings.ToLower(name)
	for _, s := range englishSourceTokens {
		if strings.Contains(low, "."+s+".") || strings.Contains(low, " "+s+" ") {
			return true
		}
	}
	return false
}

// fileMetaFor resolves the downloaded-file metadata for a library symlink path.
func fileMetaFor(targets map[string]string, symlinkPath string) *fileMeta {
	if symlinkPath == "" || targets == nil {
		return nil
	}
	if tgt := targets[symlinkPath]; tgt != "" {
		return fileMetaFromTarget(symlinkPath, tgt)
	}
	return nil
}
