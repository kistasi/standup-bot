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

// Digest renders the project items, pull requests, commits, then calendar
// events as one report. Both pulls and events may be nil — the standup passes
// no pull requests, and events is nil when Google Calendar is not configured —
// in which case that section is omitted.
func Digest(project *gh.ProjectDigest, pulls *gh.PullRequestDigest, commits *gh.CommitDigest, events *gcal.Digest, number int, since time.Time) string {
	var b strings.Builder
	writeProject(&b, project, number, since)
	if pulls != nil {
		writePulls(&b, pulls, project, since)
	}
	writeCommits(&b, commits, since)
	if events != nil {
		writeEvents(&b, events, since)
	}
	return b.String()
}

// writeProject renders a project digest. Each line carries the item's type
// ("Issue", "PullRequest", "DraftIssue") so a summarizer can tell tickets and
// pull requests apart and cite them separately.
func writeProject(b *strings.Builder, d *gh.ProjectDigest, number int, since time.Time) {
	for _, item := range d.Items {
		url := item.URL
		if url == "" {
			url = "(draft)"
		}
		fmt.Fprintf(b, "[%s]\t%s\t%s\t%s\t%s\n",
			item.UpdatedAt.Local().Format(timeLayout), item.Type, item.State, item.Title, url)
		writeBody(b, item.Body)
	}

	fmt.Fprintf(b, "\n%d item(s) assigned to %s updated since %s in project #%d %q\n",
		len(d.Items), d.Login, since.Local().Format(timeLayout), number, d.Title)
}

// writePulls renders the viewer's pull requests, skipping any already listed in
// the project section: a pull request on the board would otherwise be reported
// twice, once per section.
func writePulls(b *strings.Builder, d *gh.PullRequestDigest, project *gh.ProjectDigest, since time.Time) {
	onBoard := make(map[string]bool, len(project.Items))
	for _, item := range project.Items {
		if item.URL != "" {
			onBoard[item.URL] = true
		}
	}

	n := 0
	for _, pr := range d.PullRequests {
		if onBoard[pr.URL] {
			continue
		}
		n++
		state := pr.State
		if pr.Draft {
			state += " (draft)"
		}
		fmt.Fprintf(b, "[%s]\tPullRequest\t%s\t%s\t%s\t%s\n",
			pr.UpdatedAt.Local().Format(timeLayout), pr.Repo, state, pr.Title, pr.URL)
		writeBody(b, pr.Body)
	}

	fmt.Fprintf(b, "\n%d further pull request(s) by %s in %s updated since %s, not on the project board\n",
		n, d.Login, d.Org, since.Local().Format(timeLayout))
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
