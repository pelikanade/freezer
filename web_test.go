package main

import (
	"testing"
	"time"
)

func TestHumanAgo(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		then time.Time
		want string
	}{
		{"seconds", now.Add(-30 * time.Second), "just now"},
		{"future", now.Add(5 * time.Second), "just now"},
		{"one minute", now.Add(-time.Minute), "1 min ago"},
		{"minutes", now.Add(-59 * time.Minute), "59 min ago"},
		{"one hour", now.Add(-time.Hour), "1 h ago"},
		{"hours", now.Add(-23 * time.Hour), "23 h ago"},
		{"one day", now.Add(-24 * time.Hour), "1 d ago"},
		{"days", now.Add(-29 * 24 * time.Hour), "29 d ago"},
		{"old", now.Add(-30 * 24 * time.Hour), "2026-09-18"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := humanAgo(now, tc.then); got != tc.want {
				t.Errorf("humanAgo() = %q, want %q", got, tc.want)
			}
		})
	}
}
