package catalog

import (
	"testing"
	"time"
)

func TestSearchDue(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) *time.Time { x := now.Add(-d); return &x }
	dateAgo := func(d time.Duration) string { return now.Add(-d).Format("2006-01-02") }

	if !searchDue("", nil, now) {
		t.Error("never-searched item must be due")
	}
	// Fresh release (<48h): hourly.
	if searchDue(dateAgo(10*time.Hour), ago(30*time.Minute), now) {
		t.Error("fresh release searched 30m ago should not be due (hourly)")
	}
	if !searchDue(dateAgo(10*time.Hour), ago(2*time.Hour), now) {
		t.Error("fresh release searched 2h ago should be due (hourly)")
	}
	// Mid window (2-14 days): daily.
	if searchDue(dateAgo(5*24*time.Hour), ago(12*time.Hour), now) {
		t.Error("5-day-old release searched 12h ago should not be due (daily)")
	}
	if !searchDue(dateAgo(5*24*time.Hour), ago(25*time.Hour), now) {
		t.Error("5-day-old release searched 25h ago should be due (daily)")
	}
	// Old (>14 days): monthly.
	if searchDue(dateAgo(60*24*time.Hour), ago(10*24*time.Hour), now) {
		t.Error("old release searched 10d ago should not be due (monthly)")
	}
	if !searchDue(dateAgo(60*24*time.Hour), ago(31*24*time.Hour), now) {
		t.Error("old release searched 31d ago should be due (monthly)")
	}
}
