package selection

import "testing"

func TestFailedGroupPenalty(t *testing.T) {
	cfg := Config{WeightPreferredGroup: 50}
	rel := Release{Title: "Movie.2024.1080p.BluRay-BADGRP", Protocol: "usenet"}
	without := cfg.Score(rel)
	cfg.FailedGroups = map[string]bool{"badgrp": true}
	with := cfg.Score(rel)
	if with != without-50 {
		t.Errorf("failure-prone group should lose WeightPreferredGroup: without=%d with=%d", without, with)
	}
}
