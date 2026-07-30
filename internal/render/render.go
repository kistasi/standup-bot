// Package render formats gh digests into the plain-text standup report that is
// printed to stdout or fed to the summarizer.
package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/kistasi/standup-bot/internal/gcal"
	"github.com/kistasi/standup-bot/internal/gh"
	"github.com/kistasi/standup-bot/internal/toggl"
)

const timeLayout = "2006-01-02 15:04"

// delivered is the marker appended to a release pull request or commit line.
// The summarizer is told to read it as "this shipped".
const delivered = "\tDELIVERED"

// Digest renders the pull requests, commits, calendar events, then tracked time
// as one report. events and times may each be nil — they are when Google
// Calendar or Toggl is not configured, and times is also nil when the window
// predates Toggl's lookback limit — in which case that section is omitted.
// Tickets are deliberately not part of the digest: the report follows the code
// that was written and released.
func Digest(pulls *gh.PullRequestDigest, commits *gh.CommitDigest, events *gcal.Digest, times *toggl.Digest, since time.Time) string {
	var b strings.Builder
	writePulls(&b, pulls, since)
	writeCommits(&b, commits, since)
	if events != nil {
		writeEvents(&b, events, since)
	}
	if times != nil {
		writeTimes(&b, times, since)
	}
	return b.String()
}

// writePulls renders the viewer's pull requests. A pull request opened from a
// release branch carries the DELIVERED marker and its branch name, so the
// summarizer can report it as shipped.
func writePulls(b *strings.Builder, d *gh.PullRequestDigest, since time.Time) {
	for _, pr := range d.PullRequests {
		state := pr.State
		if pr.Draft {
			state += " (draft)"
		}
		fmt.Fprintf(b, "[%s]\tPullRequest\t%s\t%s\t%s\t%s\t%s%s\n",
			pr.UpdatedAt.Local().Format(timeLayout), pr.Repo, state, pr.Branch,
			pr.Title, pr.URL, mark(pr.Delivered))
		writeBody(b, pr.Body)
	}

	fmt.Fprintf(b, "\n%d pull request(s) by %s in %s updated since %s\n",
		len(d.PullRequests), d.Login, d.Org, since.Local().Format(timeLayout))
}

// writeCommits renders a commit digest, marking release commits DELIVERED.
func writeCommits(b *strings.Builder, d *gh.CommitDigest, since time.Time) {
	for _, c := range d.Commits {
		fmt.Fprintf(b, "[%s]\t%s\t%s\t%s%s\n",
			c.CommittedDate.Local().Format(timeLayout), c.Repo, c.Message, c.URL, mark(c.Delivered))
		writeBody(b, c.Body)
	}

	fmt.Fprintf(b, "\n%d commit(s) by %s in %s since %s\n",
		len(d.Commits), strings.Join(d.Emails, ", "), d.Org, since.Local().Format(timeLayout))
}

// writeEvents renders a calendar digest: one line per event with its time
// range, followed by a count footer.
func writeEvents(b *strings.Builder, d *gcal.Digest, since time.Time) {
	for _, e := range d.Events {
		fmt.Fprintf(b, "[%s]\t%s\t%s\n",
			e.Start.Local().Format(timeLayout), e.End.Local().Format(timeLayout), e.Summary)
	}

	fmt.Fprintf(b, "\n%d calendar event(s) in %s since %s\n",
		len(d.Events), d.CalendarID, since.Local().Format(timeLayout))
}

// writeTimes renders the tracked time that names no ticket, and only that: one
// line per entry with its start, duration, project, description and tags. Time
// logged against a ticket is deliberately left out — the pull request and commit
// sections already account for that work, and the digest carries no figure for
// how long it took. What remains is the work with no code behind it, which
// nothing else in the digest records. A still-running timer is flagged, since
// its duration is only as of the moment the digest was built.
func writeTimes(b *strings.Builder, d *toggl.Digest, since time.Time) {
	// Counted from the entries actually written, so the footer can never
	// disagree with the lines above it.
	n, total := 0, time.Duration(0)
	for _, e := range d.Entries {
		if e.Flagged() {
			continue
		}
		n++
		total += e.Duration

		description := e.Description
		if description == "" {
			description = "(no description)"
		}
		project := e.Project
		if project == "" {
			project = "(no project)"
		}
		fmt.Fprintf(b, "[%s]\t%s\t%s\t%s\t%s",
			e.Start.Local().Format(timeLayout), toggl.Label(e.Duration), project, description,
			strings.Join(e.Tags, ", "))
		if e.Running {
			fmt.Fprint(b, "\t(still running)")
		}
		fmt.Fprintln(b)
	}

	fmt.Fprintf(b, "\n%d time entr%s totalling %s since %s, none tied to a ticket: work with no commit or pull request behind it\n",
		n, plural(n), toggl.Label(total), since.Local().Format(timeLayout))
}

// plural is the "entry"/"entries" suffix for the tracked-time footer.
func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

// mark returns the trailing DELIVERED marker for a released entry, or nothing.
func mark(isDelivered bool) string {
	if isDelivered {
		return delivered
	}
	return ""
}

// writeBody renders a trimmed description block indented under its entry, or
// nothing when empty.
func writeBody(b *strings.Builder, body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}
	for _, line := range strings.Split(body, "\n") {
		fmt.Fprintf(b, "    %s\n", strings.TrimRight(line, "\r"))
	}
}
