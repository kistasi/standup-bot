// Package toggl wraps the Toggl Track API v9 for the standup bot: it reads the
// token owner's own time entries for a reporting window.
package toggl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// baseURL is the Toggl Track API v9 root. It is a variable so tests can point
// the client at an httptest server.
var baseURL = "https://api.track.toggl.com/api/v9"

// Lookback is how far back /me/time_entries will serve entries — roughly three
// months. Toggl rejects an earlier start_date outright, so the standup (a window
// of days) is always fine while an old monthly report is not.
const Lookback = 3 * 30 * 24 * time.Hour

// ErrBeyondLookback reports a window starting further back than Toggl serves.
// It is a sentinel so callers can carry on without the section — a monthly
// report for an old month is still worth writing without its hours.
var ErrBeyondLookback = errors.New("start of window is beyond Toggl's ~3-month lookback limit")

// ticketRe matches a GitHub-style ticket reference — "#412" — which is how the
// owner flags an entry as belonging to a ticket. A reference anywhere in the
// description or tags counts, since it is often written mid-sentence.
var ticketRe = regexp.MustCompile(`#\d+\b`)

// Entry is one time entry belonging to the token owner.
type Entry struct {
	Description string // may be empty: an entry needs no description
	Project     string // resolved by the API via meta=true; empty when unset
	Start       time.Time
	Stop        time.Time // zero while the timer is still running
	Duration    time.Duration
	Tags        []string
	Running     bool     // the timer had not been stopped when the digest was built
	Tickets     []string // the "#412" references found on the entry, in order
}

// Flagged reports whether the entry names a ticket. An unflagged entry is the
// interesting case: work with no ticket behind it — planning, research, review —
// leaves no commit or pull request either, so this section is its only record.
func (e Entry) Flagged() bool {
	return len(e.Tickets) > 0
}

// Digest is the result of reading a window of time entries.
type Digest struct {
	Entries []Entry
	Total   time.Duration // the summed duration of every entry, running included

	// Unflagged is the part of Total spent on entries naming no ticket.
	Unflagged time.Duration
}

// Client reads the token owner's time entries.
type Client struct {
	http *http.Client
}

// New builds a Client for a Toggl API token. Mirroring gh.New(token), the token
// is itself the credential; Toggl takes it as the username of an HTTP Basic
// pair whose password is the literal "api_token".
func New(token string) *Client {
	return &Client{
		http: &http.Client{
			Timeout:   10 * time.Second,
			Transport: basicAuth{token: token, base: http.DefaultTransport},
		},
	}
}

// basicAuth authenticates every outgoing request with the API token.
type basicAuth struct {
	token string
	base  http.RoundTripper
}

func (a basicAuth) RoundTrip(r *http.Request) (*http.Response, error) {
	// Clone before mutating: RoundTrippers must not modify the caller's request.
	clone := r.Clone(r.Context())
	clone.SetBasicAuth(a.token, "api_token")
	return a.base.RoundTrip(clone)
}

// entryJSON mirrors one element of the /me/time_entries array. The nullable
// fields are pointers so a null decodes to the zero value rather than failing.
type entryJSON struct {
	Description *string  `json:"description"`
	ProjectName *string  `json:"project_name"` // only present with meta=true
	Start       string   `json:"start"`
	Stop        *string  `json:"stop"` // null while the timer runs
	Duration    int64    `json:"duration"`
	Tags        []string `json:"tags"`
}

// Entries returns the token owner's time entries whose start falls in
// [since, until). It returns ErrBeyondLookback when `since` predates what Toggl
// serves, rather than letting the API's own error surface.
//
// The endpoint has no pagination — one request covers the window — which also
// keeps the standup inside Toggl's 30-requests-per-hour user-scoped quota.
func (c *Client) Entries(ctx context.Context, since, until time.Time) (*Digest, error) {
	if time.Since(since) > Lookback {
		return nil, fmt.Errorf("%w: %s", ErrBeyondLookback, since.Format(time.DateOnly))
	}

	// RFC3339 with an explicit offset, not a bare date: Toggl stores entries in
	// UTC but renders them in the account's own profile timezone, which need not
	// match the local zone the window was built in.
	q := url.Values{
		"start_date": {since.Format(time.RFC3339)},
		"end_date":   {until.Format(time.RFC3339)},
		// meta=true resolves project_name inline. Without it the project would
		// need a lookup per entry, which the hourly quota cannot afford.
		"meta": {"true"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/me/time_entries?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkStatus(resp); err != nil {
		return nil, err
	}

	var raw []entryJSON
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("toggl: decoding time entries: %w", err)
	}

	digest := &Digest{}
	for _, r := range raw {
		entry, ok := r.toEntry(since, until)
		if !ok {
			continue
		}
		digest.Entries = append(digest.Entries, entry)
		digest.Total += entry.Duration
		if !entry.Flagged() {
			digest.Unflagged += entry.Duration
		}
	}
	// The API does not promise an order; sort oldest first so the rendered
	// section reads chronologically and two runs agree.
	slices.SortStableFunc(digest.Entries, func(a, b Entry) int {
		return a.Start.Compare(b.Start)
	})

	return digest, nil
}

// checkStatus turns Toggl's error responses into Go errors, naming the two that
// are operational rather than programming mistakes: 402 is the hourly quota
// (Toggl uses it where most APIs use 429) and 429 is the rate limiter.
func checkStatus(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusPaymentRequired:
		return fmt.Errorf("toggl: hourly request quota exhausted (402), resets in %s",
			resp.Header.Get("X-Toggl-Quota-Resets-In"))
	case http.StatusTooManyRequests:
		return errors.New("toggl: rate limited (429): pace requests at ~1/second")
	case http.StatusForbidden:
		return errors.New("toggl: authentication failed (403): check TOGGL_API_TOKEN")
	default:
		return fmt.Errorf("toggl: %s", resp.Status)
	}
}

// toEntry flattens one decoded entry, returning ok=false when it is unusable or
// starts outside [since, until). The window is re-checked here because the API
// filters on its own timezone rendering of start_date/end_date.
func (r entryJSON) toEntry(since, until time.Time) (Entry, bool) {
	start, err := time.Parse(time.RFC3339, r.Start)
	if err != nil {
		return Entry{}, false
	}
	if start.Before(since) || !start.Before(until) {
		return Entry{}, false
	}

	entry := Entry{
		Description: deref(r.Description),
		Project:     deref(r.ProjectName),
		Start:       start,
		Tags:        r.Tags,
	}
	entry.Tickets = tickets(entry.Description, entry.Tags)

	// A running timer reports duration as the negative of its start epoch (the
	// "-1 convention"), so the field is meaningless until the timer stops. Time
	// it from the start instead — a 05:00 cron can well catch a live timer.
	if r.Stop == nil || *r.Stop == "" {
		entry.Running = true
		entry.Duration = time.Since(start).Truncate(time.Minute)
		return entry, true
	}

	stop, err := time.Parse(time.RFC3339, *r.Stop)
	if err != nil {
		return Entry{}, false
	}
	entry.Stop = stop
	entry.Duration = stop.Sub(start)
	if r.Duration > 0 {
		// Prefer Toggl's own figure when it is meaningful: it accounts for edits
		// that stop-minus-start would miss.
		entry.Duration = time.Duration(r.Duration) * time.Second
	}
	return entry, true
}

// tickets collects the distinct ticket references on an entry, keeping the
// order they were written in. Tags are searched as well as the description: a
// reference is a reference wherever the owner put it.
func tickets(description string, tags []string) []string {
	var found []string
	for _, text := range append([]string{description}, tags...) {
		for _, ref := range ticketRe.FindAllString(text, -1) {
			if !slices.Contains(found, ref) {
				found = append(found, ref)
			}
		}
	}
	return found
}

// Label renders a duration as "2h 15m", the form a report reads best. It rounds
// to the nearest minute rather than ever showing seconds.
func Label(d time.Duration) string {
	d = d.Round(time.Minute)
	if d < 0 {
		d = 0
	}
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// deref returns the pointed-to string, or "" for a JSON null.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}
