package gcal

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// stubServer returns a Client whose Calendar endpoint is an httptest server
// replying with the given JSON bodies in order, one per Events.List call, plus
// the query strings the client sent (so a test can assert the window and paging
// parameters). Credentials are bypassed with WithoutAuthentication, mirroring
// how internal/gh points its typed client at a test server.
func stubServer(t *testing.T, calendarID string, bodies ...string) (*Client, *[]url.Values) {
	t.Helper()
	var queries []url.Values
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query())

		if i >= len(bodies) {
			t.Errorf("unexpected request #%d (only %d responses stubbed)", i+1, len(bodies))
			http.Error(w, "no more responses", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, bodies[i])
		i++
	}))
	t.Cleanup(srv.Close)

	svc, err := calendar.NewService(context.Background(),
		option.WithEndpoint(srv.URL),
		option.WithHTTPClient(srv.Client()),
		option.WithoutAuthentication(),
	)
	if err != nil {
		t.Fatalf("calendar.NewService() error = %v", err)
	}
	return &Client{svc: svc, calendarID: calendarID}, &queries
}

func TestEvents_PagesAndFiltersNoise(t *testing.T) {
	// Page 1: a normal meeting (kept) and an all-day marker (dropped).
	// Page 2: a declined meeting (dropped) and a second meeting (kept).
	page1 := `{
		"items": [
			{"summary": "Sprint planning",
			 "start": {"dateTime": "2026-07-23T10:00:00+02:00"},
			 "end":   {"dateTime": "2026-07-23T11:00:00+02:00"}},
			{"summary": "Marton OOO", "start": {"date": "2026-07-23"}}
		],
		"nextPageToken": "page2"
	}`
	page2 := `{
		"items": [
			{"summary": "Optional review",
			 "start": {"dateTime": "2026-07-23T15:00:00+02:00"},
			 "end":   {"dateTime": "2026-07-23T16:00:00+02:00"},
			 "attendees": [{"self": true, "responseStatus": "declined"}]},
			{"summary": "Team demo",
			 "start": {"dateTime": "2026-07-23T16:00:00+02:00"},
			 "end":   {"dateTime": "2026-07-23T16:30:00+02:00"}}
		]
	}`

	client, queries := stubServer(t, "me@example.com", page1, page2)

	since := time.Date(2026, 7, 23, 5, 0, 0, 0, time.UTC)
	until := time.Date(2026, 7, 24, 5, 0, 0, 0, time.UTC)
	digest, err := client.Events(context.Background(), since, until)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}

	if digest.CalendarID != "me@example.com" {
		t.Errorf("CalendarID = %q, want %q", digest.CalendarID, "me@example.com")
	}
	var summaries []string
	for _, e := range digest.Events {
		summaries = append(summaries, e.Summary)
	}
	want := []string{"Sprint planning", "Team demo"}
	if strings.Join(summaries, "|") != strings.Join(want, "|") {
		t.Errorf("events = %v, want %v", summaries, want)
	}

	if len(*queries) != 2 {
		t.Fatalf("made %d requests, want 2 (paging on nextPageToken)", len(*queries))
	}
	first := (*queries)[0]
	if got := first.Get("timeMin"); got != since.Format(time.RFC3339) {
		t.Errorf("timeMin = %q, want %q", got, since.Format(time.RFC3339))
	}
	if got := first.Get("timeMax"); got != until.Format(time.RFC3339) {
		t.Errorf("timeMax = %q, want %q", got, until.Format(time.RFC3339))
	}
	if got := first.Get("singleEvents"); got != "true" {
		t.Errorf("singleEvents = %q, want true (recurrences expanded)", got)
	}
	if got := first.Get("pageToken"); got != "" {
		t.Errorf("first request sent pageToken %q, want none", got)
	}
	if got := (*queries)[1].Get("pageToken"); got != "page2" {
		t.Errorf("second request pageToken = %q, want page2", got)
	}
}

func TestEvents_EmptyCalendar(t *testing.T) {
	client, _ := stubServer(t, "primary", `{"items": []}`)

	digest, err := client.Events(context.Background(), time.Now().AddDate(0, 0, -1), time.Now())
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	if len(digest.Events) != 0 {
		t.Errorf("Events = %v, want none", digest.Events)
	}
	if digest.CalendarID != "primary" {
		t.Errorf("CalendarID = %q, want %q", digest.CalendarID, "primary")
	}
}

func TestEvents_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error": {"message": "not found"}}`, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	svc, err := calendar.NewService(context.Background(),
		option.WithEndpoint(srv.URL),
		option.WithHTTPClient(srv.Client()),
		option.WithoutAuthentication(),
	)
	if err != nil {
		t.Fatalf("calendar.NewService() error = %v", err)
	}
	client := &Client{svc: svc, calendarID: "missing@example.com"}

	if _, err := client.Events(context.Background(), time.Now().AddDate(0, 0, -1), time.Now()); err == nil {
		t.Fatal("Events() error = nil, want an error")
	} else if !strings.Contains(err.Error(), "missing@example.com") {
		t.Errorf("error = %v, want it to name the calendar", err)
	}
}

func TestNew_RejectsMalformedCredentials(t *testing.T) {
	if _, err := New(context.Background(), []byte("not json"), "primary"); err == nil {
		t.Fatal("New() error = nil, want an error for malformed credentials JSON")
	}
}
