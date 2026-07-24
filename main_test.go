package main

import (
	"strings"
	"testing"
	"time"
)

// Only the rejection branch is exercised here: the other two reach out to
// GitHub and Anthropic, so they belong to the internal packages' tests.
func TestDispatch_UnknownCommand(t *testing.T) {
	err := dispatch([]string{"weekly"})
	if err == nil {
		t.Fatal("dispatch() error = nil, want an error for an unknown command")
	}
	if !strings.Contains(err.Error(), `"weekly"`) || !strings.Contains(err.Error(), "usage") {
		t.Errorf("dispatch() error = %v, want it to name the command and show usage", err)
	}
}

// The Drive filename is date-based so a re-run on the same day replaces that
// day's backup instead of adding a second copy.
func TestStandupName(t *testing.T) {
	now := time.Date(2026, 7, 24, 9, 30, 0, 0, time.UTC)
	if got, want := standupName(now), "2026-07-24-standup.md"; got != want {
		t.Errorf("standupName() = %q, want %q", got, want)
	}
	later := now.Add(6 * time.Hour)
	if standupName(later) != standupName(now) {
		t.Error("standupName() changed within the same day, want one file per day")
	}
}
