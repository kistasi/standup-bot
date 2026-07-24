// Package render formats gh digests into the plain-text standup report that is
// printed to stdout or fed to the summarizer.
package render

import (
	"fmt"
	"strings"
	"time"

	"github.com/kistasi/standup-bot/internal/gcal"
	"github.com/kistasi/standup-bot/internal/gh"
)

const timeLayout = "2006-01-02 15:04"

// Digest renders the project items, commits, then calendar events as one
// report. events may be nil when Google Calendar is not configured, in which
// case no calendar section is written.
func Digest(project *gh.ProjectDigest, commits *gh.CommitDigest, events *gcal.Digest, number int, since time.Time) string {
	var b strings.Builder
	writeProject(&b, project, number, since)
	writeCommits(&b, commits, since)
	if events != nil {
		writeEvents(&b, events, since)
	}
	return b.String()
}

// writeProject renders a project digest.
func writeProject(b *strings.Builder, d *gh.ProjectDigest, number int, since time.Time) {
	for _, item := range d.Items {
		url := item.URL
		if url == "" {
			url = "(draft)"
		}
		fmt.Fprintf(b, "[%s]\t%s\t%s\t%s\n",
			item.UpdatedAt.Local().Format(timeLayout), item.State, item.Title, url)
		writeBody(b, item.Body)
	}

	fmt.Fprintf(b, "\n%d item(s) assigned to %s updated since %s in project #%d %q\n",
		len(d.Items), d.Login, since.Local().Format(timeLayout), number, d.Title)
}

// writeCommits renders a commit digest.
func writeCommits(b *strings.Builder, d *gh.CommitDigest, since time.Time) {
	for _, c := range d.Commits {
		fmt.Fprintf(b, "[%s]\t%s\t%s\t%s\n",
			c.CommittedDate.Local().Format(timeLayout), c.Repo, c.Message, c.URL)
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
