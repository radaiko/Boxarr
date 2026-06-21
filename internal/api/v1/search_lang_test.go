package v1

import (
	"testing"

	"github.com/radaiko/boxarr/internal/prowlarr"
	"github.com/radaiko/boxarr/internal/selection"
)

func TestReleaseDTOLanguagesAndSubs(t *testing.T) {
	rr := prowlarr.ReleaseResource{Title: "Avatar.2009.German.DL.1080p.BluRay-GRP", Protocol: "usenet", Grabs: 5}
	d := toReleaseDTO(rr, selection.Scored{Release: selection.Release{Title: rr.Title, Quality: "BluRay"}, Score: 10}, "1080p")
	hasDE, hasEN := false, false
	for _, l := range d.Languages {
		hasDE = hasDE || l == "DE"
		hasEN = hasEN || l == "EN"
	}
	if !hasDE || !hasEN {
		t.Errorf("languages = %v, want DE+EN", d.Languages)
	}
	if d.Subs {
		t.Error("no subs marker expected")
	}

	subbed := prowlarr.ReleaseResource{Title: "Frieren.S01E12.SUBBED.1080p.WEB-GRP", Protocol: "usenet"}
	if !toReleaseDTO(subbed, selection.Scored{Release: selection.Release{Title: subbed.Title}}, "1080p").Subs {
		t.Error("expected subs marker for SUBBED release")
	}
}
