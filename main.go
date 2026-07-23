package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kistasi/standup-bot/internal/claude"
	"github.com/kistasi/standup-bot/internal/config"
	"github.com/kistasi/standup-bot/internal/gh"
	"github.com/kistasi/standup-bot/internal/render"
	"github.com/kistasi/standup-bot/internal/slack"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
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
	since := cfg.Since()

	project, err := client.MyProjectItems(ctx, cfg.Org, cfg.ProjectNumber, since)
	if err != nil {
		return err
	}

	commits, err := client.OrgCommits(ctx, cfg.Org, since, cfg.AuthorEmails)
	if err != nil {
		return err
	}

	digest := render.Digest(project, commits, cfg.ProjectNumber, since)

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
	return nil
}
