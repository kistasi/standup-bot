package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kistasi/standup-bot/internal/claude"
	"github.com/kistasi/standup-bot/internal/config"
	"github.com/kistasi/standup-bot/internal/gcal"
	"github.com/kistasi/standup-bot/internal/gdrive"
	"github.com/kistasi/standup-bot/internal/gh"
	"github.com/kistasi/standup-bot/internal/monthly"
	"github.com/kistasi/standup-bot/internal/render"
	"github.com/kistasi/standup-bot/internal/slack"
	"github.com/kistasi/standup-bot/internal/toggl"
)

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// dispatch picks the command to run. With no arguments it runs the standup —
// that is what the scheduled workflow invokes — while "monthly" runs the
// interactive monthly report, which is CLI-only.
func dispatch(args []string) error {
	switch {
	case len(args) == 0:
		return run()
	case args[0] == "monthly":
		return runMonthly()
	default:
		return fmt.Errorf("unknown command %q (usage: standup-bot [monthly])", args[0])
	}
}

// run assembles the digest — pull requests and commits, plus meetings when the
// calendar is on and tracked time when Toggl is — and feeds it to Claude for a
// standup summary. Project tickets are deliberately not part of it: the report
// follows the code.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx := context.Background()
	client := gh.New(cfg.GitHubToken)

	// One clock for the whole run: the window's end and the backup's filename
	// must agree even if the run straddles midnight.
	now := time.Now()
	since, until := cfg.Since(), now

	pulls, err := client.MyPullRequests(ctx, cfg.Org, since, until)
	if err != nil {
		return err
	}

	commits, err := client.OrgCommits(ctx, cfg.Org, since, until, cfg.AuthorEmails)
	if err != nil {
		return err
	}

	// Calendar events are optional: fetch them only when credentials and a
	// calendar are configured; otherwise events stays nil and the digest omits
	// the section.
	var events *gcal.Digest
	if cfg.CalendarEnabled() {
		cal, err := gcal.New(ctx, []byte(cfg.GoogleCredentialsJSON), cfg.GoogleCalendarID)
		if err != nil {
			return err
		}
		events, err = cal.Events(ctx, since, until)
		if err != nil {
			return err
		}
	}

	times, err := trackedTime(ctx, cfg, since, until)
	if err != nil {
		return err
	}

	// A window with no activity in it has nothing to say. Stopping here keeps
	// the channel free of "nothing to report" messages — and there is no digest
	// worth summarizing or backing up either, so the run ends before the Claude
	// call rather than after it.
	if !hasActivity(pulls, commits, events, times) {
		fmt.Fprintf(os.Stderr, "Nothing logged since %s: no standup posted.\n",
			since.Local().Format("2006-01-02 15:04"))
		return nil
	}

	digest := render.Digest(pulls, commits, events, times, since)

	summary, err := claude.New().Ask(ctx, claude.StandupSystem(cfg.PeriodLabel()), digest)
	if err != nil {
		return err
	}
	fmt.Println(summary)

	// Post to Slack only when a webhook is configured; otherwise the printed
	// summary above is the whole output.
	if cfg.SlackWebhookURL != "" {
		if err := slack.New(cfg.SlackWebhookURL).Post(ctx, summary); err != nil {
			return err
		}
	}

	// Back the summary up to Drive when configured. Unlike the monthly report
	// the standup writes no local file — this run happens on a scheduler with an
	// ephemeral disk — so the text is uploaded straight from memory.
	return backup(ctx, cfg, standupName(now), []byte(summary))
}

// hasActivity reports whether the window holds anything worth a standup. It
// mirrors what render.Digest actually prints: the optional sections count only
// when they are configured (a nil digest is an absent section, not an empty
// one), and tracked time counts only for the entries naming no ticket, since
// those are the only ones the report shows. Meetings count too — a day spent in
// them is still a day to report on.
func hasActivity(pulls *gh.PullRequestDigest, commits *gh.CommitDigest, events *gcal.Digest, times *toggl.Digest) bool {
	switch {
	case len(pulls.PullRequests) > 0, len(commits.Commits) > 0:
		return true
	case events != nil && len(events.Events) > 0:
		return true
	}
	if times == nil {
		return false
	}
	for _, e := range times.Entries {
		if !e.Flagged() {
			return true
		}
	}
	return false
}

// standupName is the Drive filename for a day's standup summary, e.g.
// "2026-07-24-standup.md". Re-running on the same day overwrites that day's
// file rather than piling up duplicates.
func standupName(now time.Time) string {
	return now.Format("2006-01-02") + "-standup.md"
}

// backup uploads one finished report to Drive and reports where it landed. It
// is a no-op when the Drive backup is not configured, so both commands can call
// it unconditionally.
func backup(ctx context.Context, cfg *config.Config, name string, content []byte) error {
	if !cfg.DriveEnabled() {
		return nil
	}
	dr, err := gdrive.New(ctx, []byte(cfg.GoogleCredentialsJSON), cfg.GoogleDriveFolder)
	if err != nil {
		return err
	}
	link, err := dr.Upload(ctx, name, content)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Backed up to", link)
	return nil
}

// trackedTime reads the window's Toggl entries, or returns nil when tracking is
// off. A window older than Toggl's ~3-month lookback is a warning rather than a
// failure: a monthly report for an old month is still worth writing without its
// hours, and the guard means no request is spent on a call Toggl would reject.
func trackedTime(ctx context.Context, cfg *config.Config, since, until time.Time) (*toggl.Digest, error) {
	if !cfg.TogglEnabled() {
		return nil, nil
	}
	times, err := toggl.New(cfg.TogglAPIToken).Entries(ctx, since, until)
	if errors.Is(err, toggl.ErrBeyondLookback) {
		fmt.Fprintln(os.Stderr, "Skipping tracked time:", err)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return times, nil
}

// runMonthly asks for a month, gathers that month's GitHub activity and tracked
// time, and writes a markdown report to disk. Calendar events and Slack are
// deliberately left out: this is a written report, not a daily message.
func runMonthly() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	year, month, err := monthly.Ask(os.Stdin, os.Stdout, time.Now())
	if err != nil {
		return err
	}
	since, until := monthly.Window(year, month)
	label := monthly.Label(year, month)

	ctx := context.Background()
	client := gh.New(cfg.GitHubToken)

	fmt.Fprintf(os.Stderr, "Collecting %s activity...\n", label)
	pulls, err := client.MyPullRequests(ctx, cfg.Org, since, until)
	if err != nil {
		return err
	}

	commits, err := client.OrgCommits(ctx, cfg.Org, since, until, cfg.AuthorEmails)
	if err != nil {
		return err
	}

	times, err := trackedTime(ctx, cfg, since, until)
	if err != nil {
		return err
	}

	digest := render.Digest(pulls, commits, nil, times, since)

	fmt.Fprintln(os.Stderr, "Writing the report...")
	report, err := claude.New().Ask(ctx, claude.MonthlySystem(label), digest)
	if err != nil {
		return err
	}

	path, err := monthly.Write(monthly.Dir, year, month, report)
	if err != nil {
		return err
	}
	fmt.Println(path)

	// Back up the file just written, under the same name it has on disk.
	if !cfg.DriveEnabled() {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return backup(ctx, cfg, filepath.Base(path), content)
}
