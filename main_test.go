package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kistasi/standup-bot/internal/config"
	"github.com/kistasi/standup-bot/internal/gcal"
	"github.com/kistasi/standup-bot/internal/gh"
	"github.com/kistasi/standup-bot/internal/toggl"
)

// Only the rejection branch is exercised here: the other two reach out to
// GitHub and Anthropic, so they belong to the internal packages' tests.
func TestDispatch_UnknownCommand(t *testing.T) {
	err := dispatch([]string{"weekly"})
	if err == nil {
		t.Fatal("dispatch() error = nil, want an error for an unknown command")
	}
	if !strings.Contains(err.Error(), `"weekly"`) || !strings.Contains(err.Error(), "usage") {
		t.Errorf("dispatch() error = %v, want it to name the command and show usage", err)
	}
}

// The Drive filename is date-based so a re-run on the same day replaces that
// day's backup instead of adding a second copy.
func TestStandupName(t *testing.T) {
	now := time.Date(2026, 7, 24, 9, 30, 0, 0, time.UTC)
	if got, want := standupName(now), "2026-07-24-standup.md"; got != want {
		t.Errorf("standupName() = %q, want %q", got, want)
	}
	later := now.Add(6 * time.Hour)
	if standupName(later) != standupName(now) {
		t.Error("standupName() changed within the same day, want one file per day")
	}
}

// An empty window must not reach Slack, so hasActivity has to be false for
// exactly the digest render would print nothing from: no pull requests, no
// commits, no meetings, and no ticket-less time entries.
func TestHasActivity(t *testing.T) {
	empty := func() (*gh.PullRequestDigest, *gh.CommitDigest) {
		return &gh.PullRequestDigest{}, &gh.CommitDigest{}
	}

	t.Run("nothing at all", func(t *testing.T) {
		pulls, commits := empty()
		if hasActivity(pulls, commits, nil, nil) {
			t.Error("hasActivity() = true, want false for an empty window")
		}
	})

	t.Run("empty optional sections", func(t *testing.T) {
		pulls, commits := empty()
		if hasActivity(pulls, commits, &gcal.Digest{}, &toggl.Digest{}) {
			t.Error("hasActivity() = true, want false when the calendar and Toggl are on but empty")
		}
	})

	t.Run("only ticketed time", func(t *testing.T) {
		pulls, commits := empty()
		// Entries naming a ticket are dropped by render, so they are not
		// activity as far as the standup is concerned.
		times := &toggl.Digest{Entries: []toggl.Entry{{Description: "fix #12", Tickets: []string{"#12"}}}}
		if hasActivity(pulls, commits, nil, times) {
			t.Error("hasActivity() = true, want false when every time entry names a ticket")
		}
	})

	t.Run("each section alone is enough", func(t *testing.T) {
		pulls, commits := empty()
		cases := map[string]bool{
			"pull request": hasActivity(&gh.PullRequestDigest{PullRequests: []gh.PullRequest{{Title: "Add a thing"}}}, commits, nil, nil),
			"commit":       hasActivity(pulls, &gh.CommitDigest{Commits: []gh.Commit{{Message: "Add a thing"}}}, nil, nil),
			"meeting":      hasActivity(pulls, commits, &gcal.Digest{Events: []gcal.Event{{Summary: "Planning"}}}, nil),
			"time entry":   hasActivity(pulls, commits, nil, &toggl.Digest{Entries: []toggl.Entry{{Description: "pairing"}}}),
		}
		for name, got := range cases {
			if !got {
				t.Errorf("hasActivity() = false with only a %s, want true", name)
			}
		}
	})
}

// Both commands call backup unconditionally, so the "Drive not configured"
// case has to return without building a client — there are no credentials to
// build one from.
func TestBackup_NoOpWhenDriveDisabled(t *testing.T) {
	cfg := &config.Config{} // no GoogleDriveFolder, no credentials
	if err := backup(context.Background(), cfg, "2026-07-24-standup.md", []byte("report")); err != nil {
		t.Errorf("backup() error = %v, want nil when the Drive backup is off", err)
	}
}

// With Drive switched on but the credential unusable, the failure is surfaced
// rather than swallowed: a backup that silently did nothing would be worse
// than a failed run.
func TestBackup_ReportsBadCredentials(t *testing.T) {
	cfg := &config.Config{
		GoogleDriveFolder:     "standup-bot",
		GoogleCredentialsJSON: "not json",
	}
	if err := backup(context.Background(), cfg, "2026-07-24-standup.md", []byte("report")); err == nil {
		t.Error("backup() error = nil, want the malformed credential surfaced")
	}
}
