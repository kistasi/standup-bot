package render

import (
	"strings"
	"testing"
	"time"

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

	got := Digest(project, commits, 7, t1)

	// Project item line with URL and indented body.
	wantIssueLine := "[" + fmtTime(t1) + "]\tOPEN\tFix login bug\thttps://github.com/acme/app/issues/1"
	if !strings.Contains(got, wantIssueLine) {
		t.Errorf("missing issue line %q in:\n%s", wantIssueLine, got)
	}
	if !strings.Contains(got, "    Users can't log in.\n    Stack trace attached.") {
		t.Errorf("issue body not indented as expected in:\n%s", got)
	}

	// Draft item uses "(draft)" placeholder for the empty URL and DRAFT state.
	wantDraftLine := "[" + fmtTime(t2) + "]\tDRAFT\tSpike on caching\t(draft)"
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

func TestDigest_Empty(t *testing.T) {
	project := &gh.ProjectDigest{Login: "octocat", Title: "Roadmap"}
	commits := &gh.CommitDigest{Org: "acme", Emails: []string{"dev@acme.com"}}

	got := Digest(project, commits, 7, t1)

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
	got := Digest(&gh.ProjectDigest{}, commits, 1, t1)

	if !strings.Contains(got, "by a@acme.com, b@acme.com in acme") {
		t.Errorf("emails not joined with ', ' in:\n%s", got)
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
