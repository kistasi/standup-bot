package gcal

import (
	"testing"
	"time"

	"google.golang.org/api/calendar/v3"
)

// timed builds a plain timed event at the given RFC3339 start.
func timed(summary, start string) *calendar.Event {
	return &calendar.Event{
		Summary: summary,
		Start:   &calendar.EventDateTime{DateTime: start},
		End:     &calendar.EventDateTime{DateTime: start},
	}
}

func TestToEvent_Keeps(t *testing.T) {
	e := timed("Sprint planning", "2026-07-20T10:00:00+02:00")
	got, ok := toEvent(e)
	if !ok {
		t.Fatal("toEvent dropped a normal timed event")
	}
	if got.Summary != "Sprint planning" {
		t.Errorf("Summary = %q, want %q", got.Summary, "Sprint planning")
	}
	want := time.Date(2026, 7, 20, 10, 0, 0, 0, time.FixedZone("", 2*60*60))
	if !got.Start.Equal(want) {
		t.Errorf("Start = %v, want %v", got.Start, want)
	}
}

func TestToEvent_Skips(t *testing.T) {
	tests := []struct {
		name  string
		event *calendar.Event
	}{
		{
			name: "all-day event (Date, no DateTime)",
			event: &calendar.Event{
				Summary: "Marton OOO",
				Start:   &calendar.EventDateTime{Date: "2026-07-20"},
			},
		},
		{
			name: "cancelled instance",
			event: func() *calendar.Event {
				e := timed("Cancelled sync", "2026-07-20T09:00:00+02:00")
				e.Status = "cancelled"
				return e
			}(),
		},
		{
			name: "owner declined",
			event: func() *calendar.Event {
				e := timed("Optional review", "2026-07-20T15:00:00+02:00")
				e.Attendees = []*calendar.EventAttendee{
					{Email: "other@x.com", ResponseStatus: "accepted"},
					{Self: true, ResponseStatus: "declined"},
				}
				return e
			}(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := toEvent(tt.event); ok {
				t.Errorf("toEvent kept event that should be skipped: %s", tt.name)
			}
		})
	}
}

func TestToEvent_KeepsAcceptedByOthersDeclined(t *testing.T) {
	// Someone else declined, but the owner (Self) accepted: keep it.
	e := timed("Team demo", "2026-07-20T11:00:00+02:00")
	e.Attendees = []*calendar.EventAttendee{
		{Email: "other@x.com", ResponseStatus: "declined"},
		{Self: true, ResponseStatus: "accepted"},
	}
	if _, ok := toEvent(e); !ok {
		t.Error("toEvent dropped an event the owner accepted")
	}
}
