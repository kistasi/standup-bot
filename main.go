package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kistasi/standup-bot/internal/claude"
	"github.com/kistasi/standup-bot/internal/config"
	"github.com/kistasi/standup-bot/internal/gh"
	"github.com/kistasi/standup-bot/internal/render"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run assembles the digest and either prints it raw or, when STANDUP_SUMMARIZE
// is set, feeds it to Claude for a standup summary.
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

	if !cfg.Summarize {
		fmt.Print(digest)
		return nil
	}

	summary, err := claude.New().Ask(ctx, claude.StandupSystem, digest)
	if err != nil {
		return err
	}
	fmt.Println(summary)
	return nil
}
