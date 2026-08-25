package toggl

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// captureServer points the package at an httptest server replying with body,
// and returns the client plus a getter for the request it recorded.
func captureServer(t *testing.T, status int, body string, headers map[string]string) (*Client, func() *http.Request) {
	t.Helper()
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(context.Background())
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	old := baseURL
	baseURL = srv.URL
	t.Cleanup(func() { baseURL = old })

	return New("tok123"), func() *http.Request { return got }
}

// window returns a recent window, so the lookback guard never trips in tests
// that are not about it. It is truncated to a second because that is all the
// precision an RFC3339 timestamp from Toggl carries — an unrounded bound could
// never be compared exactly against a parsed one.
func window() (time.Time, time.Time) {
	until := time.Now().Truncate(time.Second)
	return until.Add(-24 * time.Hour), until
}

func TestEntries_DecodesAndTotals(t *testing.T) {
	since, until := window()
	start1 := since.Add(time.Hour)
	start2 := since.Add(3 * time.Hour)

	body := `[
		{"client_name":"DPGA","description":"Fix login bug","project_name":"App","start":"` + start2.Format(time.RFC3339) + `","stop":"` + start2.Add(90*time.Minute).Format(time.RFC3339) + `","duration":5400,"tags":["dev"]},
		{"client_name":"DPGA","description":"Standup","project_name":"App","start":"` + start1.Format(time.RFC3339) + `","stop":"` + start1.Add(15*time.Minute).Format(time.RFC3339) + `","duration":900,"tags":null}
	]`

	client, _ := captureServer(t, http.StatusOK, body, nil)

	digest, err := client.Entries(context.Background(), since, until)
	if err != nil {
		t.Fatalf("Entries error = %v", err)
	}

	if len(digest.Entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(digest.Entries), digest.Entries)
	}
	// Sorted oldest first, regardless of the order the API returned.
	if digest.Entries[0].Description != "Standup" {
		t.Errorf("entries not sorted oldest first: %+v", digest.Entries)
	}
	e := digest.Entries[1]
	if e.Description != "Fix login bug" || e.Project != "App" || e.Duration != 90*time.Minute ||
		e.Running || len(e.Tags) != 1 || e.Tags[0] != "dev" {
		t.Errorf("entry = %+v, unexpected fields", e)
	}
	if want := 105 * time.Minute; digest.Unflagged != want {
		t.Errorf("Unflagged = %v, want %v (nothing here names a ticket)", digest.Unflagged, want)
	}
}

func TestEntries_RequestShape(t *testing.T) {
	since, until := window()
	client, req := captureServer(t, http.StatusOK, `[]`, nil)

	if _, err := client.Entries(context.Background(), since, until); err != nil {
		t.Fatalf("Entries error = %v", err)
	}

	r := req()
	if r == nil {
		t.Fatal("no request recorded")
	}
	if r.URL.Path != "/me/time_entries" {
		t.Errorf("path = %q, want /me/time_entries", r.URL.Path)
	}

	q := r.URL.Query()
	// meta=true is what resolves project names inline, sparing a lookup per
	// entry that the hourly quota could not afford.
	if q.Get("meta") != "true" {
		t.Errorf("meta = %q, want true", q.Get("meta"))
	}
	// Full RFC3339 with an offset, not a bare date: Toggl renders in the
	// account's profile timezone, not the machine's.
	for _, key := range []string{"start_date", "end_date"} {
		if _, err := time.Parse(time.RFC3339, q.Get(key)); err != nil {
			t.Errorf("%s = %q, want RFC3339 with offset: %v", key, q.Get(key), err)
		}
	}

	// The token goes out as the Basic username with the literal "api_token" as
	// the password.
	user, pass, ok := r.BasicAuth()
	if !ok || user != "tok123" || pass != "api_token" {
		t.Errorf("basic auth = %q/%q (ok=%v), want tok123/api_token", user, pass, ok)
	}
}

func TestBasicAuth_DoesNotMutateCaller(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	tr := basicAuth{token: "tok", base: http.DefaultTransport}
	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error = %v", err)
	}
	resp.Body.Close()

	if h := req.Header.Get("Authorization"); h != "" {
		t.Errorf("caller request mutated: Authorization = %q, want empty", h)
	}
}

func TestEntries_RunningTimer(t *testing.T) {
	since, until := window()
	start := time.Now().Add(-30 * time.Minute)

	// A running entry has stop:null and a negative duration (the "-1
	// convention"), so the duration field must be ignored.
	body := `[{"client_name":"DPGA","description":"In progress","project_name":null,"start":"` +
		start.Format(time.RFC3339) + `","stop":null,"duration":-1750000000,"tags":null}]`

	client, _ := captureServer(t, http.StatusOK, body, nil)

	digest, err := client.Entries(context.Background(), since, until)
	if err != nil {
		t.Fatalf("Entries error = %v", err)
	}
	if len(digest.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(digest.Entries))
	}
	e := digest.Entries[0]
	if !e.Running {
		t.Error("Running = false, want true for a stop:null entry")
	}
	if e.Duration < 29*time.Minute || e.Duration > 31*time.Minute {
		t.Errorf("Duration = %v, want ~30m timed from the start", e.Duration)
	}
	if !e.Stop.IsZero() {
		t.Errorf("Stop = %v, want zero while running", e.Stop)
	}
	// A null project_name must not break decoding.
	if e.Project != "" {
		t.Errorf("Project = %q, want empty for null", e.Project)
	}
}

func TestEntries_NullDescriptionAndTags(t *testing.T) {
	since, until := window()
	start := since.Add(time.Hour)
	body := `[{"client_name":"DPGA","description":null,"project_name":null,"start":"` + start.Format(time.RFC3339) +
		`","stop":"` + start.Add(time.Hour).Format(time.RFC3339) + `","duration":3600,"tags":null}]`

	client, _ := captureServer(t, http.StatusOK, body, nil)

	digest, err := client.Entries(context.Background(), since, until)
	if err != nil {
		t.Fatalf("Entries error = %v", err)
	}
	if len(digest.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(digest.Entries))
	}
	if e := digest.Entries[0]; e.Description != "" || e.Project != "" || e.Tags != nil {
		t.Errorf("entry = %+v, want zero values for the null fields", e)
	}
}

// The window is half-open, and re-checked client-side because the API filters
// against its own timezone rendering of the bounds.
func TestEntries_FiltersWindowClientSide(t *testing.T) {
	since, until := window()
	body := `[
		{"client_name":"DPGA","description":"before","start":"` + since.Add(-time.Hour).Format(time.RFC3339) + `","stop":"` + since.Format(time.RFC3339) + `","duration":3600},
		{"client_name":"DPGA","description":"at until","start":"` + until.Format(time.RFC3339) + `","stop":"` + until.Add(time.Hour).Format(time.RFC3339) + `","duration":3600},
		{"client_name":"DPGA","description":"inside","start":"` + since.Add(time.Hour).Format(time.RFC3339) + `","stop":"` + since.Add(2*time.Hour).Format(time.RFC3339) + `","duration":3600}
	]`

	client, _ := captureServer(t, http.StatusOK, body, nil)

	digest, err := client.Entries(context.Background(), since, until)
	if err != nil {
		t.Fatalf("Entries error = %v", err)
	}
	if len(digest.Entries) != 1 || digest.Entries[0].Description != "inside" {
		t.Errorf("entries = %+v, want only the in-window one", digest.Entries)
	}
}

// Only the configured client's work is reported: the account also tracks time
// for other clients (and entries with no client at all), which is none of the
// bot's business.
func TestEntries_FiltersOtherClients(t *testing.T) {
	since, until := window()
	start := since.Add(time.Hour)
	stop := start.Add(time.Hour).Format(time.RFC3339)
	entry := func(client string) string {
		return `{` + client + `"description":"work","start":"` + start.Format(time.RFC3339) +
			`","stop":"` + stop + `","duration":3600}`
	}

	body := `[` + entry(`"client_name":"Other Ltd",`) + `,` +
		entry(`"client_name":null,`) + `,` +
		entry(``) + `,` +
		entry(`"client_name":"dpga",`) + `]`

	client, _ := captureServer(t, http.StatusOK, body, nil)

	digest, err := client.Entries(context.Background(), since, until)
	if err != nil {
		t.Fatalf("Entries error = %v", err)
	}
	if len(digest.Entries) != 1 {
		t.Fatalf("got %d entries, want only the DPGA one: %+v", len(digest.Entries), digest.Entries)
	}
	if got := digest.Entries[0].Client; got != "dpga" {
		t.Errorf("Client = %q, want the value the API sent", got)
	}
	if want := time.Hour; digest.Unflagged != want {
		t.Errorf("Unflagged = %v, want %v (only the kept entry counts)", digest.Unflagged, want)
	}
}

func TestEntries_BeyondLookback(t *testing.T) {
	// No server: the guard must trip before any request is made, so an old
	// monthly report does not spend quota on a call Toggl would reject.
	client := New("tok")
	since := time.Now().AddDate(0, -6, 0)

	_, err := client.Entries(context.Background(), since, since.AddDate(0, 1, 0))
	if !errors.Is(err, ErrBeyondLookback) {
		t.Fatalf("Entries error = %v, want ErrBeyondLookback", err)
	}
	// The message names the offending date, so the warning main prints is useful.
	if !strings.Contains(err.Error(), since.Format(time.DateOnly)) {
		t.Errorf("error = %q, want it to name %s", err, since.Format(time.DateOnly))
	}
}

func TestEntries_StatusErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		headers map[string]string
		want    string
	}{
		{"quota", http.StatusPaymentRequired, map[string]string{"X-Toggl-Quota-Resets-In": "600"}, "600"},
		{"rate limited", http.StatusTooManyRequests, nil, "429"},
		{"forbidden", http.StatusForbidden, nil, "TOGGL_API_TOKEN"},
		{"server error", http.StatusInternalServerError, nil, "500"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			since, until := window()
			client, _ := captureServer(t, tt.status, `{}`, tt.headers)

			_, err := client.Entries(context.Background(), since, until)
			if err == nil {
				t.Fatalf("Entries error = nil, want error on %d", tt.status)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestEntries_MalformedBody(t *testing.T) {
	since, until := window()
	client, _ := captureServer(t, http.StatusOK, `not json`, nil)

	if _, err := client.Entries(context.Background(), since, until); err == nil {
		t.Fatal("Entries error = nil, want decode error")
	}
}

func TestEntries_UnparsableTimesDropped(t *testing.T) {
	since, until := window()
	start := since.Add(time.Hour)
	body := `[
		{"client_name":"DPGA","description":"bad start","start":"whenever","stop":null,"duration":1},
		{"client_name":"DPGA","description":"bad stop","start":"` + start.Format(time.RFC3339) + `","stop":"whenever","duration":1}
	]`
	client, _ := captureServer(t, http.StatusOK, body, nil)

	digest, err := client.Entries(context.Background(), since, until)
	if err != nil {
		t.Fatalf("Entries error = %v", err)
	}
	if len(digest.Entries) != 0 {
		t.Errorf("entries = %+v, want none (both timestamps unparsable)", digest.Entries)
	}
}

func TestEntries_ContextCanceled(t *testing.T) {
	since, until := window()
	client, _ := captureServer(t, http.StatusOK, `[]`, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := client.Entries(ctx, since, until); err == nil {
		t.Fatal("Entries error = nil, want error for canceled context")
	}
}

func TestTickets(t *testing.T) {
	tests := []struct {
		name        string
		description string
		tags        []string
		want        []string
	}{
		{"leading reference", "#412 refactor auth", nil, []string{"#412"}},
		{"mid-sentence reference", "pairing on #7 with Dani", nil, []string{"#7"}},
		{"several references", "#1 and #2", nil, []string{"#1", "#2"}},
		{"repeated reference counts once", "#5 then more #5", nil, []string{"#5"}},
		{"reference in a tag", "refactor auth", []string{"#412"}, []string{"#412"}},
		{"description and tag combine", "#1 work", []string{"#2"}, []string{"#1", "#2"}},
		{"no reference", "release planning", []string{"internal"}, nil},
		{"hash without digits", "# planning", nil, nil},
		{"bare number is not a reference", "412 planning", nil, nil},
		{"empty", "", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tickets(tt.description, tt.tags)
			if len(got) != len(tt.want) {
				t.Fatalf("tickets(%q, %v) = %v, want %v", tt.description, tt.tags, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("tickets(%q, %v) = %v, want %v", tt.description, tt.tags, got, tt.want)
				}
			}
		})
	}
}

// The unflagged entries are the point of the section: work with no ticket has
// no commit or pull request either, so nothing else in the digest records it.
func TestEntries_FlagsAndUnflaggedTotal(t *testing.T) {
	since, until := window()
	start := since.Add(time.Hour)
	stop := func(d time.Duration) string { return start.Add(d).Format(time.RFC3339) }

	body := `[
		{"client_name":"DPGA","description":"#412 fix login","start":"` + start.Format(time.RFC3339) + `","stop":"` + stop(time.Hour) + `","duration":3600,"tags":null},
		{"client_name":"DPGA","description":"release planning","start":"` + start.Format(time.RFC3339) + `","stop":"` + stop(30*time.Minute) + `","duration":1800,"tags":null},
		{"client_name":"DPGA","description":"API research","start":"` + start.Format(time.RFC3339) + `","stop":"` + stop(15*time.Minute) + `","duration":900,"tags":null}
	]`

	client, _ := captureServer(t, http.StatusOK, body, nil)

	digest, err := client.Entries(context.Background(), since, until)
	if err != nil {
		t.Fatalf("Entries error = %v", err)
	}
	if len(digest.Entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(digest.Entries))
	}

	flagged := 0
	for _, e := range digest.Entries {
		if e.Flagged() {
			flagged++
		}
	}
	if flagged != 1 {
		t.Errorf("got %d flagged entries, want 1: %+v", flagged, digest.Entries)
	}
	// Only the two entries naming no ticket count towards Unflagged; there is no
	// total across every entry, by design.
	if want := 45 * time.Minute; digest.Unflagged != want {
		t.Errorf("Unflagged = %v, want %v", digest.Unflagged, want)
	}
}

func TestLabel(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0m"},
		{29 * time.Second, "0m"}, // rounded to the nearest minute, never seconds
		{45 * time.Second, "1m"},
		{15 * time.Minute, "15m"},
		{time.Hour, "1h 0m"},
		{135 * time.Minute, "2h 15m"},
		{-time.Hour, "0m"}, // a negative duration never reaches a report
	}
	for _, tt := range tests {
		if got := Label(tt.d); got != tt.want {
			t.Errorf("Label(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestQueryEncodingKeepsOffset(t *testing.T) {
	// A "+" in an RFC3339 offset must survive as %2B, not become a space.
	ts := time.Date(2026, 7, 19, 9, 0, 0, 0, time.FixedZone("CEST", 2*60*60))
	v := url.Values{"start_date": {ts.Format(time.RFC3339)}}
	if got := v.Encode(); !strings.Contains(got, "%2B02%3A00") {
		t.Errorf("encoded query = %q, want the +02:00 offset escaped", got)
	}
}
