package render

import (
	"strings"
	"testing"
	"time"

	"github.com/kistasi/standup-bot/internal/gcal"
	"github.com/kistasi/standup-bot/internal/gh"
	"github.com/kistasi/standup-bot/internal/toggl"
)

// fixed reference time; rendering uses .Local(), so expectations are built with
// the same Local formatting to stay timezone-independent.
var (
	t1 = time.Date(2026, 7, 19, 9, 30, 0, 0, time.UTC)
	t2 = time.Date(2026, 7, 19, 14, 5, 0, 0, time.UTC)
)

func fmtTime(ts time.Time) string { return ts.Local().Format(timeLayout) }

func TestDigest_PullsAndCommits(t *testing.T) {
	pulls := &gh.PullRequestDigest{
		Login: "octocat",
		Org:   "acme",
		PullRequests: []gh.PullRequest{
			{
				Repo:      "acme/app",
				Title:     "Fix login bug",
				Body:      "Users can't log in.\nStack trace attached.",
				URL:       "https://github.com/acme/app/pull/1",
				State:     "MERGED",
				Branch:    "fix/login",
				UpdatedAt: t1,
			},
			{
				Repo:      "acme/tools",
				Title:     "Bump the linter",
				URL:       "https://github.com/acme/tools/pull/9",
				State:     "OPEN",
				Branch:    "chore/linter",
				Draft:     true,
				UpdatedAt: t2,
			},
		},
	}
	commits := &gh.CommitDigest{
		Org:    "acme",
		Emails: []string{"dev@acme.com"},
		Commits: []gh.Commit{
			{
				Repo:          "acme/app",
				Message:       "Add retry logic",
				Body:          "Retries transient failures.",
				URL:           "https://github.com/acme/app/commit/abc",
				CommittedDate: t2,
			},
		},
	}

	got := Digest(pulls, commits, nil, nil, t1)

	// Pull request line with repo, state, branch, title, URL, and indented body.
	wantPull := "[" + fmtTime(t1) + "]\tPullRequest\tacme/app\tMERGED\tfix/login\tFix login bug\thttps://github.com/acme/app/pull/1"
	if !strings.Contains(got, wantPull) {
		t.Errorf("missing pull request line %q in:\n%s", wantPull, got)
	}
	if !strings.Contains(got, "    Users can't log in.\n    Stack trace attached.") {
		t.Errorf("pull request body not indented as expected in:\n%s", got)
	}

	// A draft pull request says so in the state column.
	wantDraft := "\tOPEN (draft)\tchore/linter\tBump the linter\t"
	if !strings.Contains(got, wantDraft) {
		t.Errorf("missing draft pull request line %q in:\n%s", wantDraft, got)
	}

	wantPullFooter := "2 pull request(s) by octocat in acme updated since " + fmtTime(t1)
	if !strings.Contains(got, wantPullFooter) {
		t.Errorf("missing pull request footer %q in:\n%s", wantPullFooter, got)
	}

	// Commit line and indented body.
	wantCommitLine := "[" + fmtTime(t2) + "]\tacme/app\tAdd retry logic\thttps://github.com/acme/app/commit/abc"
	if !strings.Contains(got, wantCommitLine) {
		t.Errorf("missing commit line %q in:\n%s", wantCommitLine, got)
	}
	if !strings.Contains(got, "    Retries transient failures.") {
		t.Errorf("commit body not indented in:\n%s", got)
	}

	wantCommitFooter := "1 commit(s) by dev@acme.com in acme since " + fmtTime(t1)
	if !strings.Contains(got, wantCommitFooter) {
		t.Errorf("missing commit footer %q in:\n%s", wantCommitFooter, got)
	}

	// Nothing here shipped, so no line carries the marker.
	if strings.Contains(got, "DELIVERED") {
		t.Errorf("unexpected DELIVERED marker in:\n%s", got)
	}

	// Pull requests precede commits.
	if strings.Index(got, "Fix login bug") > strings.Index(got, "Add retry logic") {
		t.Error("expected the pull request section to render before commits")
	}
}

func TestDigest_DeliveredMarked(t *testing.T) {
	pulls := &gh.PullRequestDigest{
		Login: "octocat",
		Org:   "acme",
		PullRequests: []gh.PullRequest{
			{
				Repo:      "acme/app",
				Title:     "Release 2026-07-19",
				URL:       "https://github.com/acme/app/pull/12",
				State:     "MERGED",
				Branch:    "release/2026-07-19",
				UpdatedAt: t1,
				Delivered: true,
			},
		},
	}
	commits := &gh.CommitDigest{
		Org:    "acme",
		Emails: []string{"dev@acme.com"},
		Commits: []gh.Commit{
			{
				Repo:          "acme/app",
				Message:       "Release 2026-07-19 (#12)",
				URL:           "https://github.com/acme/app/commit/def",
				CommittedDate: t2,
				Delivered:     true,
			},
			{
				Repo:          "acme/app",
				Message:       "Add retry logic",
				URL:           "https://github.com/acme/app/commit/abc",
				CommittedDate: t2,
			},
		},
	}

	got := Digest(pulls, commits, nil, nil, t1)

	wantPull := "\trelease/2026-07-19\tRelease 2026-07-19\thttps://github.com/acme/app/pull/12\tDELIVERED\n"
	if !strings.Contains(got, wantPull) {
		t.Errorf("release pull request not marked delivered; want %q in:\n%s", wantPull, got)
	}
	wantCommit := "\tRelease 2026-07-19 (#12)\thttps://github.com/acme/app/commit/def\tDELIVERED\n"
	if !strings.Contains(got, wantCommit) {
		t.Errorf("release commit not marked delivered; want %q in:\n%s", wantCommit, got)
	}
	// Exactly the two release entries are marked — not the ordinary commit.
	if n := strings.Count(got, "DELIVERED"); n != 2 {
		t.Errorf("DELIVERED appears %d times, want 2, in:\n%s", n, got)
	}
}

func TestDigest_Empty(t *testing.T) {
	pulls := &gh.PullRequestDigest{Login: "octocat", Org: "acme"}
	commits := &gh.CommitDigest{Org: "acme", Emails: []string{"dev@acme.com"}}

	got := Digest(pulls, commits, nil, nil, t1)

	if !strings.Contains(got, "0 pull request(s) by octocat in acme") {
		t.Errorf("empty pull request footer missing in:\n%s", got)
	}
	if !strings.Contains(got, "0 commit(s) by dev@acme.com in acme") {
		t.Errorf("empty commit footer missing in:\n%s", got)
	}
}

func TestDigest_MultipleEmailsJoined(t *testing.T) {
	commits := &gh.CommitDigest{
		Org:    "acme",
		Emails: []string{"a@acme.com", "b@acme.com"},
	}
	got := Digest(&gh.PullRequestDigest{}, commits, nil, nil, t1)

	if !strings.Contains(got, "by a@acme.com, b@acme.com in acme") {
		t.Errorf("emails not joined with ', ' in:\n%s", got)
	}
}

func TestDigest_Events(t *testing.T) {
	events := &gcal.Digest{
		CalendarID: "me@acme.com",
		Events: []gcal.Event{
			{Summary: "Sprint planning", Start: t1, End: t2},
		},
	}
	got := Digest(&gh.PullRequestDigest{}, &gh.CommitDigest{}, events, nil, t1)

	wantLine := "[" + fmtTime(t1) + "]\t" + fmtTime(t2) + "\tSprint planning"
	if !strings.Contains(got, wantLine) {
		t.Errorf("missing event line %q in:\n%s", wantLine, got)
	}
	wantFooter := "1 calendar event(s) in me@acme.com since " + fmtTime(t1)
	if !strings.Contains(got, wantFooter) {
		t.Errorf("missing event footer %q in:\n%s", wantFooter, got)
	}
}

func TestDigest_NoEventsSectionWhenNil(t *testing.T) {
	got := Digest(&gh.PullRequestDigest{}, &gh.CommitDigest{}, nil, nil, t1)
	if strings.Contains(got, "calendar event(s)") {
		t.Errorf("nil events should render no calendar section, got:\n%s", got)
	}
}

func TestDigest_TrackedTime(t *testing.T) {
	times := &toggl.Digest{
		Total:     105 * time.Minute,
		Unflagged: 15 * time.Minute,
		Entries: []toggl.Entry{
			{
				Description: "#412 Fix login bug",
				Project:     "App",
				Start:       t1,
				Stop:        t1.Add(90 * time.Minute),
				Duration:    90 * time.Minute,
				Tags:        []string{"dev", "urgent"},
				Tickets:     []string{"#412"},
			},
			{
				// No ticket: the entry no commit will ever account for.
				Description: "Release planning",
				Project:     "Internal",
				Start:       t2,
				Duration:    15 * time.Minute,
			},
		},
	}
	got := Digest(&gh.PullRequestDigest{}, &gh.CommitDigest{}, nil, times, t1)

	// A flagged entry renders plainly...
	wantLine := "[" + fmtTime(t1) + "]\t1h 30m\tApp\t#412 Fix login bug\tdev, urgent\n"
	if !strings.Contains(got, wantLine) {
		t.Errorf("missing time entry line %q in:\n%s", wantLine, got)
	}
	// ...an unflagged one carries the marker.
	wantUnflagged := "[" + fmtTime(t2) + "]\t15m\tInternal\tRelease planning\t\tNO TICKET\n"
	if !strings.Contains(got, wantUnflagged) {
		t.Errorf("missing unflagged entry line %q in:\n%s", wantUnflagged, got)
	}
	if n := strings.Count(got, "NO TICKET"); n != 1 {
		t.Errorf("NO TICKET appears %d times, want 1, in:\n%s", n, got)
	}

	wantFooter := "2 time entries totalling 1h 45m tracked since " + fmtTime(t1)
	if !strings.Contains(got, wantFooter) {
		t.Errorf("missing tracked-time footer %q in:\n%s", wantFooter, got)
	}
	wantSplit := "1 of them (15m) name no ticket"
	if !strings.Contains(got, wantSplit) {
		t.Errorf("missing unflagged split %q in:\n%s", wantSplit, got)
	}
	// Tracked time closes the digest, after the commit section.
	if strings.Index(got, "commit(s) by") > strings.Index(got, "Fix login bug") {
		t.Error("expected the tracked-time section to render after commits")
	}
}

// The footer's unflagged figures are derived from the rendered entries, so a
// caller-supplied total can never make the summary contradict the lines.
func TestDigest_UnflaggedFooterFollowsTheEntries(t *testing.T) {
	times := &toggl.Digest{
		Total:     time.Hour,
		Unflagged: 99 * time.Hour, // wrong on purpose
		Entries: []toggl.Entry{
			{Description: "#1 tracked", Start: t1, Duration: 30 * time.Minute, Tickets: []string{"#1"}},
			{Description: "research", Start: t2, Duration: 30 * time.Minute},
		},
	}
	got := Digest(&gh.PullRequestDigest{}, &gh.CommitDigest{}, nil, times, t1)

	if !strings.Contains(got, "1 of them (30m) name no ticket") {
		t.Errorf("footer did not follow the rendered entries in:\n%s", got)
	}
}

func TestDigest_TrackedTimePlaceholdersAndRunning(t *testing.T) {
	// An entry needs neither a description nor a project, and a timer may still
	// be running when the digest is built — none of which may render blank.
	times := &toggl.Digest{
		Total: 30 * time.Minute,
		Entries: []toggl.Entry{
			{Start: t1, Duration: 30 * time.Minute, Running: true},
		},
	}
	got := Digest(&gh.PullRequestDigest{}, &gh.CommitDigest{}, nil, times, t1)

	wantLine := "[" + fmtTime(t1) + "]\t30m\t(no project)\t(no description)\t\tNO TICKET\t(still running)\n"
	if !strings.Contains(got, wantLine) {
		t.Errorf("missing running entry line %q in:\n%s", wantLine, got)
	}
	// The footer's singular form.
	if !strings.Contains(got, "1 time entry totalling 30m tracked since") {
		t.Errorf("singular footer missing in:\n%s", got)
	}
}

func TestDigest_NoTrackedTimeSectionWhenNil(t *testing.T) {
	got := Digest(&gh.PullRequestDigest{}, &gh.CommitDigest{}, nil, nil, t1)
	if strings.Contains(got, "tracked since") {
		t.Errorf("nil tracked time should render no section, got:\n%s", got)
	}
}

func TestWriteBody(t *testing.T) {
	t.Run("blank produces nothing", func(t *testing.T) {
		var b strings.Builder
		writeBody(&b, "   \n\t ")
		if b.String() != "" {
			t.Errorf("writeBody on blank = %q, want empty", b.String())
		}
	})

	t.Run("strips trailing CR and indents", func(t *testing.T) {
		var b strings.Builder
		writeBody(&b, "line one\r\nline two")
		want := "    line one\n    line two\n"
		if b.String() != want {
			t.Errorf("writeBody = %q, want %q", b.String(), want)
		}
	})
}
