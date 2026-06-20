package job

import "testing"

func TestStateSeedingTransitions(t *testing.T) {
	if !StateDownloading.CanTransitionTo(StateSeeding) {
		t.Error("downloading -> seeding should be allowed")
	}
	if !StateSeeding.CanTransitionTo(StateCompleted) {
		t.Error("seeding -> completed should be allowed")
	}
	if !StateSeeding.CanTransitionTo(StateFailed) {
		t.Error("seeding -> failed should be allowed")
	}
	if StateSeeding.IsTerminal() {
		t.Error("seeding must not be terminal")
	}
}
