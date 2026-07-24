// Package gcal wraps the Google Calendar v3 API for the standup bot. It reads a
// single calendar read-only from a Google credentials JSON — typically
// authorized-user credentials (from `gcloud auth application-default login`,
// which reads the signed-in user's own calendar with full event details), or a
// service-account key (which needs the calendar shared with its email).
package gcal

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// Event is a single timed calendar event in the reporting window.
type Event struct {
	Summary string
	Start   time.Time
	End     time.Time
}

// Digest is the result of scanning one calendar for events.
type Digest struct {
	CalendarID string
	Events     []Event
}

// Client reads events from a single calendar.
type Client struct {
	svc        *calendar.Service
	calendarID string
}

// New builds a Client from a Google credentials JSON (authorized-user or
// service-account). calendarID is the calendar to read — the owner's email
// address, or "primary" for the credential owner's own primary calendar.
func New(ctx context.Context, credsJSON []byte, calendarID string) (*Client, error) {
	svc, err := calendar.NewService(ctx,
		option.WithCredentialsJSON(credsJSON),
		option.WithScopes(calendar.CalendarReadonlyScope),
	)
	if err != nil {
		return nil, fmt.Errorf("gcal: %w", err)
	}
	return &Client{svc: svc, calendarID: calendarID}, nil
}

// Events returns the timed events on the calendar between since and until,
// oldest first. Recurring events are expanded to individual instances.
// Cancelled, all-day, and events the calendar owner declined are dropped as
// standup noise, matching the "meetings I actually attended" intent.
func (c *Client) Events(ctx context.Context, since, until time.Time) (*Digest, error) {
	digest := &Digest{CalendarID: c.calendarID}

	call := c.svc.Events.List(c.calendarID).
		TimeMin(since.Format(time.RFC3339)).
		TimeMax(until.Format(time.RFC3339)).
		SingleEvents(true).
		OrderBy("startTime").
		MaxResults(250)

	for {
		resp, err := call.Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("gcal: list %q: %w", c.calendarID, err)
		}
		for _, e := range resp.Items {
			if ev, ok := toEvent(e); ok {
				digest.Events = append(digest.Events, ev)
			}
		}
		if resp.NextPageToken == "" {
			break
		}
		call = call.PageToken(resp.NextPageToken)
	}

	return digest, nil
}

// toEvent flattens a calendar event into an Event, returning ok=false when the
// event should be skipped: cancelled instances, all-day events (which carry a
// Start.Date rather than a Start.DateTime and are usually OOO/birthday markers),
// and events the calendar owner declined.
func toEvent(e *calendar.Event) (Event, bool) {
	if e.Status == "cancelled" {
		return Event{}, false
	}
	// All-day events have Start.Date set and Start.DateTime empty.
	if e.Start == nil || e.Start.DateTime == "" {
		return Event{}, false
	}
	for _, a := range e.Attendees {
		// Self marks the calendar owner's own attendee entry.
		if a.Self && a.ResponseStatus == "declined" {
			return Event{}, false
		}
	}

	// DateTime is RFC3339 with an offset; a parse failure leaves a zero time,
	// which the renderer still prints — better than dropping the event.
	start, _ := time.Parse(time.RFC3339, e.Start.DateTime)
	var end time.Time
	if e.End != nil {
		end, _ = time.Parse(time.RFC3339, e.End.DateTime)
	}
	return Event{Summary: e.Summary, Start: start, End: end}, true
}
