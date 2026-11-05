package main

import (
	"testing"
	"time"
)

func TestTodayStartsAtTheOperatorsMidnight(t *testing.T) {
	madrid := time.FixedZone("CET", 3600)
	// 00:30 in Madrid is still 4 November in UTC.
	got := startOfToday(time.Date(2026, 11, 5, 0, 30, 0, 0, madrid))
	if want := time.Date(2026, 11, 4, 23, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("start of today = %s, want %s", got.UTC(), want)
	}
}
