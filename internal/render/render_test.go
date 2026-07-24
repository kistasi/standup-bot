package render

import (
	"strings"
	"testing"
	"time"

	"github.com/kistasi/standup-bot/internal/gcal"
	"github.com/kistasi/standup-bot/internal/gh"
)

// fixed reference time; rendering uses .Local(), so expectations are built with
// the same Local formatting to stay timezone-independent.
var (
	t1 = time.Date(2026, 7, 19, 9, 30, 0, 0, time.UTC)
	t2 = time.Date(2026, 7, 19, 14, 5, 0, 0, time.UTC)
)

func fmtTime(ts time.Time) string { return ts.Local().Format(timeLayout) }

func TestDigest_ProjectAndCommits(t *testing.T) {
	project := &gh.ProjectDigest{
		Login: "octocat",
		Title: "Roadmap",
		Items: []gh.Item{
			{
				Type:      "Issue",
				Title:     "Fix login bug",
				Body:      "Users can't log in.\nStack trace attached.",
				URL:       "https://github.com/acme/app/issues/1",
				State:     "OPEN",
				UpdatedAt: t1,
			},
			{
				Type:      "DraftIssue",
				Title:     "Spike on caching",
				Body:      "", // no body -> no indented block
				URL:       "", // draft -> "(draft)"
				State:     "DRAFT",
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

	got := Digest(project, nil, commits, nil, 7, t1)

	// Project item line with type, URL and indented body.
	wantIssueLine := "[" + fmtTime(t1) + "]\tIssue\tOPEN\tFix login bug\thttps://github.com/acme/app/issues/1"
	if !strings.Contains(got, wantIssueLine) {
		t.Errorf("missing issue line %q in:\n%s", wantIssueLine, got)
	}
	if !strings.Contains(got, "    Users can't log in.\n    Stack trace attached.") {
		t.Errorf("issue body not indented as expected in:\n%s", got)
	}

	// Draft item uses "(draft)" placeholder for the empty URL and DRAFT state.
	wantDraftLine := "[" + fmtTime(t2) + "]\tDraftIssue\tDRAFT\tSpike on caching\t(draft)"
	if !strings.Contains(got, wantDraftLine) {
		t.Errorf("missing draft line %q in:\n%s", wantDraftLine, got)
	}

	// Project summary footer.
	wantProjFooter := `2 item(s) assigned to octocat updated since ` + fmtTime(t1) + ` in project #7 "Roadmap"`
	if !strings.Contains(got, wantProjFooter) {
		t.Errorf("missing project footer %q in:\n%s", wantProjFooter, got)
	}

	// Commit line and indented body.
	wantCommitLine := "[" + fmtTime(t2) + "]\tacme/app\tAdd retry logic\thttps://github.com/acme/app/commit/abc"
	if !strings.Contains(got, wantCommitLine) {
		t.Errorf("missing commit line %q in:\n%s", wantCommitLine, got)
	}
	if !strings.Contains(got, "    Retries transient failures.") {
		t.Errorf("commit body not indented in:\n%s", got)
	}

	// Commit summary footer.
	wantCommitFooter := "1 commit(s) by dev@acme.com in acme since " + fmtTime(t1)
	if !strings.Contains(got, wantCommitFooter) {
		t.Errorf("missing commit footer %q in:\n%s", wantCommitFooter, got)
	}

	// Project section precedes commit section.
	if strings.Index(got, "Roadmap") > strings.Index(got, "Add retry logic") {
		t.Error("expected project section to render before commits")
	}
}

func TestDigest_PullRequestItemTyped(t *testing.T) {
	// Pull requests on the board must be distinguishable from tickets, so the
	// monthly report can cite each kind separately.
	project := &gh.ProjectDigest{
		Login: "octocat",
		Items: []gh.Item{
			{
				Type:      "PullRequest",
				Title:     "Add retry logic",
				URL:       "https://github.com/acme/app/pull/2",
				State:     "MERGED",
				UpdatedAt: t1,
			},
		},
	}
	got := Digest(project, nil, &gh.CommitDigest{}, nil, 7, t1)

	want := "[" + fmtTime(t1) + "]\tPullRequest\tMERGED\tAdd retry logic\thttps://github.com/acme/app/pull/2"
	if !strings.Contains(got, want) {
		t.Errorf("missing pull request line %q in:\n%s", want, got)
	}
}

func TestDigest_PullsDedupedAgainstBoard(t *testing.T) {
	// A pull request already on the board belongs to the project section only;
	// one that never made it there gets its own entry.
	project := &gh.ProjectDigest{
		Login: "octocat",
		Items: []gh.Item{
			{
				Type:      "PullRequest",
				Title:     "On the board",
				URL:       "https://github.com/acme/app/pull/1",
				State:     "MERGED",
				UpdatedAt: t1,
			},
		},
	}
	pulls := &gh.PullRequestDigest{
		Login: "octocat",
		Org:   "acme",
		PullRequests: []gh.PullRequest{
			{
				Repo:      "acme/app",
				Title:     "On the board",
				URL:       "https://github.com/acme/app/pull/1",
				State:     "MERGED",
				UpdatedAt: t1,
			},
			{
				Repo:      "acme/tools",
				Title:     "Off the board",
				Body:      "Bumps the linter.",
				URL:       "https://github.com/acme/tools/pull/9",
				State:     "OPEN",
				Draft:     true,
				UpdatedAt: t2,
			},
		},
	}

	got := Digest(project, pulls, &gh.CommitDigest{}, nil, 7, t1)

	wantLine := "[" + fmtTime(t2) + "]\tPullRequest\tacme/tools\tOPEN (draft)\tOff the board\thttps://github.com/acme/tools/pull/9"
	if !strings.Contains(got, wantLine) {
		t.Errorf("missing pull request line %q in:\n%s", wantLine, got)
	}
	if !strings.Contains(got, "    Bumps the linter.") {
		t.Errorf("pull request body not indented in:\n%s", got)
	}
	// The board's own pull request appears once, in the project section.
	if n := strings.Count(got, "https://github.com/acme/app/pull/1"); n != 1 {
		t.Errorf("board pull request rendered %d times, want 1, in:\n%s", n, got)
	}
	wantFooter := "1 further pull request(s) by octocat in acme updated since " + fmtTime(t1) + ", not on the project board"
	if !strings.Contains(got, wantFooter) {
		t.Errorf("missing pull request footer %q in:\n%s", wantFooter, got)
	}
	// Pull requests sit between the project and commit sections.
	if strings.Index(got, "Off the board") > strings.Index(got, "commit(s) by") {
		t.Error("expected pull request section to render before commits")
	}
}

func TestDigest_NoPullsSectionWhenNil(t *testing.T) {
	got := Digest(&gh.ProjectDigest{}, nil, &gh.CommitDigest{}, nil, 1, t1)
	if strings.Contains(got, "pull request(s)") {
		t.Errorf("nil pulls should render no pull request section, got:\n%s", got)
	}
}

func TestDigest_Empty(t *testing.T) {
	project := &gh.ProjectDigest{Login: "octocat", Title: "Roadmap"}
	commits := &gh.CommitDigest{Org: "acme", Emails: []string{"dev@acme.com"}}

	got := Digest(project, nil, commits, nil, 7, t1)

	if !strings.Contains(got, "0 item(s) assigned to octocat") {
		t.Errorf("empty project footer missing in:\n%s", got)
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
	got := Digest(&gh.ProjectDigest{}, nil, commits, nil, 1, t1)

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
	got := Digest(&gh.ProjectDigest{}, nil, &gh.CommitDigest{}, events, 1, t1)

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
	got := Digest(&gh.ProjectDigest{}, nil, &gh.CommitDigest{}, nil, 1, t1)
	if strings.Contains(got, "calendar event(s)") {
		t.Errorf("nil events should render no calendar section, got:\n%s", got)
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
