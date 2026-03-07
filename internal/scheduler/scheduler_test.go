package scheduler

import (
	"testing"
	"time"

	"sauron-sees/internal/config"
)

func TestWithinWorkWindow(t *testing.T) {
	cfg := config.Default()
	planner, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	loc, _ := time.LoadLocation(cfg.Timezone)

	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"inside", time.Date(2026, 3, 9, 10, 0, 0, 0, loc), true},
		{"before", time.Date(2026, 3, 9, 8, 59, 0, 0, loc), false},
		{"after", time.Date(2026, 3, 9, 18, 31, 0, 0, loc), false},
		{"weekend", time.Date(2026, 3, 8, 10, 0, 0, 0, loc), false},
	}
	for _, tc := range cases {
		if got := planner.WithinWorkWindow(tc.at); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestCrossMidnightWindow(t *testing.T) {
	cfg := config.Default()
	cfg.WorkStart = "22:00"
	cfg.WorkEnd = "02:00"
	cfg.Workdays = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
	planner, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	loc, _ := time.LoadLocation(cfg.Timezone)

	if !planner.WithinWorkWindow(time.Date(2026, 3, 9, 23, 0, 0, 0, loc)) {
		t.Fatalf("expected 23:00 to be inside cross-midnight window")
	}
	if !planner.WithinWorkWindow(time.Date(2026, 3, 10, 1, 30, 0, 0, loc)) {
		t.Fatalf("expected 01:30 to be inside cross-midnight window")
	}
	if planner.WithinWorkWindow(time.Date(2026, 3, 10, 3, 0, 0, 0, loc)) {
		t.Fatalf("expected 03:00 to be outside cross-midnight window")
	}
}

func TestShouldCaptureAndAutoClose(t *testing.T) {
	cfg := config.Default()
	planner, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	loc, _ := time.LoadLocation(cfg.Timezone)
	now := time.Date(2026, 3, 9, 10, 0, 0, 0, loc)

	if !planner.ShouldCapture(now, time.Time{}, false) {
		t.Fatalf("expected first capture to be allowed")
	}
	last := now.Add(-4 * time.Minute)
	if planner.ShouldCapture(now, last, false) {
		t.Fatalf("expected capture to wait for interval")
	}
	if !planner.ShouldCapture(now, now.Add(-6*time.Minute), false) {
		t.Fatalf("expected capture after interval")
	}
	if !planner.ShouldAutoClose(time.Date(2026, 3, 9, 18, 30, 0, 0, loc), "2026-03-09", false, false) {
		t.Fatalf("expected auto close at close time")
	}
	if !planner.ShouldAutoClose(time.Date(2026, 3, 10, 9, 0, 0, 0, loc), "2026-03-09", false, false) {
		t.Fatalf("expected previous day to auto close on next day")
	}
}
