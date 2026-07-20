# AGENTS.md

This file provides guidance to agentic coding tools when working with code in this repository.

## Overview

`standup-bot` is an early-stage Go application (module `github.com/kistasi/standup-bot`) that assembles standup digests from GitHub project activity, with Claude available to summarize it. It is organized into a thin `main` plus four internal packages:

- `main.go` — entry point. `run()` loads config, builds a `gh.Client`, fetches the viewer's project items and recent commits, renders them via `internal/render`, and feeds the digest to `claude.Ask` for a standup summary. `main` only maps a `run()` error to stderr + exit code.
- `internal/config` — `Load()` resolves configuration from the environment (loading `.env` via `godotenv`). Every variable is required — there are no defaults or fallbacks, and `Load()` errors naming any that is unset (or malformed): `GH_PAT`, `GH_ORG`, `GH_PROJECT` (int), `GH_LOOKBACK` (Go duration), and `AUTHOR_EMAILS` (comma-separated git emails). `Config.Since()` returns `now - Lookback`.
- `internal/render` — `Digest(project, commits, number, since)` formats the two gh digests into the indented plain-text report (project items then commits) that is printed or summarized. Descriptions are printed indented under each entry so the whole thing can be fed to the Claude API.
- `internal/gh` — typed wrapper around the GitHub GraphQL v4 API (via `shurcooL/githubv4`). `New(token)` builds a bearer-authenticated client.
  - `MyProjectItems(ctx, org, number, since)` pages through an org ProjectV2, flattening Issue / PullRequest / DraftIssue nodes into a `ProjectDigest` filtered to the PAT owner (`viewer`) and to items updated on or after `since`. Each `Item` carries the issue/PR/draft `Body`.
  - `OrgCommits(ctx, org, since, emails)` pages through the org's repositories newest-pushed-first (stopping once a repo's last push predates `since`) and collects default-branch commits authored by any of `emails`, returning a `CommitDigest`. Each `Commit` carries the headline (`Message`) and full `Body`. Filtering is by **author email** (`author:{emails:...}`), not GitHub account: the contributions API and `author:{id:...}` filter both require the commit email to be linked to the account, and silently return nothing when it isn't. Release-automation commits (headline starting `release `) and merge commits (2+ parents) are skipped as standup noise.
- `internal/claude` — thin wrapper around the Anthropic Messages API. `New()` builds a client; `Ask(ctx, system, prompt)` sends a single prompt and returns the reply. `StandupSystem` is the system instruction used to turn a digest into a standup update. `main` always runs the digest through it.

## Commands

```bash
go run .          # run the program
go build ./...    # compile
go test ./...     # run all tests (no tests exist yet)
go test -run TestName ./path  # run a single test
go mod tidy       # sync go.mod/go.sum after changing imports
```

Requires Go 1.26+.

## Configuration

Env vars are loaded from a `.env` file via `godotenv.Load()` in `config.Load()` (`.env` is gitignored). Every variable below is required — `config.Load()` errors naming any that is unset (or malformed). Copy `.env.example` to `.env` and fill in:

- `GH_PAT` — GitHub personal access token.
- `ANTHROPIC_API_KEY` — read automatically by the Anthropic SDK. `claude.New()` / `anthropic.NewClient()` take no explicit key argument. Always needed, since the digest is always summarized with Claude.
- `AUTHOR_EMAILS` — comma-separated git emails to attribute commits to in `OrgCommits`. `config.Load()` errors if it yields no email.
- `GH_ORG` / `GH_PROJECT` / `GH_LOOKBACK` — the project the digest is built from (`GH_PROJECT` is an int, `GH_LOOKBACK` is a Go duration).

## Notes

- The Claude model is referenced via SDK constants (e.g. `anthropic.ModelClaudeOpus4_6`), not raw strings.
- `internal/gh` authenticates by injecting a `Bearer` token through a custom `http.RoundTripper` that clones the request before mutating headers — follow that pattern rather than mutating requests in place.
- GraphQL selections are expressed as Go structs with `graphql:"..."` tags, including inline fragments (`... on Issue`); `Item` flattens the union content into one shape.
