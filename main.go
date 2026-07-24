package main

import (
	"context"
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

// run assembles the digest and feeds it to Claude for a standup summary.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx := context.Background()
	client := gh.New(cfg.GitHubToken)
	since, until := cfg.Since(), time.Now()

	project, err := client.MyProjectItems(ctx, cfg.Org, cfg.ProjectNumber, since, until)
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

	digest := render.Digest(project, nil, commits, events, cfg.ProjectNumber, since)

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
	if cfg.DriveEnabled() {
		link, err := backup(ctx, cfg, standupName(time.Now()), []byte(summary))
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "Backed up to", link)
	}
	return nil
}

// standupName is the Drive filename for a day's standup summary, e.g.
// "2026-07-24-standup.md". Re-running on the same day overwrites that day's
// file rather than piling up duplicates.
func standupName(now time.Time) string {
	return now.Format("2006-01-02") + "-standup.md"
}

// backup uploads one finished report to Drive, returning a link to it.
func backup(ctx context.Context, cfg *config.Config, name string, content []byte) (string, error) {
	dr, err := gdrive.New(ctx, []byte(cfg.GoogleCredentialsJSON), cfg.GoogleDriveFolder)
	if err != nil {
		return "", err
	}
	return dr.Upload(ctx, name, content)
}

// runMonthly asks for a month, gathers that month's GitHub activity, and writes
// a markdown report to disk. Calendar events and Slack are deliberately left
// out: this is a written report, not a daily message.
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
	project, err := client.MyProjectItems(ctx, cfg.Org, cfg.ProjectNumber, since, until)
	if err != nil {
		return err
	}

	// Unlike the standup, the monthly report also covers pull requests that were
	// never added to the project board; render drops the ones already listed
	// above.
	pulls, err := client.MyPullRequests(ctx, cfg.Org, since, until)
	if err != nil {
		return err
	}

	commits, err := client.OrgCommits(ctx, cfg.Org, since, until, cfg.AuthorEmails)
	if err != nil {
		return err
	}

	digest := render.Digest(project, pulls, commits, nil, cfg.ProjectNumber, since)

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
	if cfg.DriveEnabled() {
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		link, err := backup(ctx, cfg, filepath.Base(path), content)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "Backed up to", link)
	}
	return nil
}
