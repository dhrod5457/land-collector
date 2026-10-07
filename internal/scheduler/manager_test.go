package scheduler

import (
	"testing"
	"time"

	"github.com/dhrod5457/land-collector/internal/domain"
)

func TestIsDueDaily(t *testing.T) {
	loc := time.FixedZone("KST", 9*60*60)
	now := time.Date(2026, 10, 7, 2, 5, 0, 0, loc)
	s := domain.Schedule{Frequency: "daily", Hour: 2, Minute: 0, Enabled: true}
	if !IsDue(s, now, loc) {
		t.Fatal("expected schedule to be due")
	}

	last := time.Date(2026, 10, 7, 2, 1, 0, 0, loc)
	s.LastRunAt = &last
	if IsDue(s, now, loc) {
		t.Fatal("expected same-day schedule not to run twice")
	}
}

func TestParseClock(t *testing.T) {
	h, m, err := ParseClock("23:45")
	if err != nil || h != 23 || m != 45 {
		t.Fatalf("unexpected clock result %d:%d %v", h, m, err)
	}
	if _, _, err := ParseClock("25:00"); err == nil {
		t.Fatal("expected invalid hour")
	}
}
