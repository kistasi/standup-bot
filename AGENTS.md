# AGENTS.md

This file provides guidance to agentic coding tools when working with code in this repository.

## Overview

`standup-bot` is an early-stage Go application (module `github.com/kistasi/standup-bot`) that assembles standup digests from GitHub project activity, with Claude available to summarize it. It is organized into a small `main` plus two internal packages:

- `main.go` — entry point. Loads env vars, builds a `gh.Client`, fetches the viewer's project items and recent commits (both over the last 24h), and prints them to stdout. Item and commit descriptions are printed indented under each entry so the whole digest can be fed to the Claude API. Author emails for the commit scan are resolved by `authorEmails()` from `AUTHOR_EMAILS` (comma-separated), falling back to `git config user.email`.
- `internal/gh` — typed wrapper around the GitHub GraphQL v4 API (via `shurcooL/githubv4`). `New(token)` builds a bearer-authenticated client.
  - `MyProjectItems(ctx, org, number, since)` pages through an org ProjectV2, flattening Issue / PullRequest / DraftIssue nodes into a `ProjectDigest` filtered to the PAT owner (`viewer`) and to items updated on or after `since`. Each `Item` carries the issue/PR/draft `Body`.
  - `OrgCommits(ctx, org, since, emails)` pages through the org's repositories newest-pushed-first (stopping once a repo's last push predates `since`) and collects default-branch commits authored by any of `emails`, returning a `CommitDigest`. Each `Commit` carries the headline (`Message`) and full `Body`. Filtering is by **author email** (`author:{emails:...}`), not GitHub account: the contributions API and `author:{id:...}` filter both require the commit email to be linked to the account, and silently return nothing when it isn't. Release-automation commits (headline starting `release `) and merge commits (2+ parents) are skipped as standup noise.
- `internal/claude` — thin wrapper around the Anthropic Messages API. `New()` builds a client; `Ask(ctx, system, prompt)` sends a single prompt and returns the reply. Not yet wired into `main` — the digest is currently printed raw rather than summarized.

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

Env vars are loaded from a `.env` file via `godotenv.Load()` (`.env` is gitignored). Copy `.env.example` to `.env` and fill in:

- `GITHUB_PAT` — GitHub personal access token, read directly from the environment in `main.go`. Required; the program exits if it is unset.
- `ANTHROPIC_API_KEY` — read automatically by the Anthropic SDK. `claude.New()` / `anthropic.NewClient()` take no explicit key argument. Only needed once the `claude` package is used.
- `AUTHOR_EMAILS` — optional, comma-separated git emails to attribute commits to in `OrgCommits`. When unset, `main.go` falls back to `git config user.email`.

The org and project number are currently hardcoded in `main.go` (`MyProjectItems(ctx, "DPGAlliance", 14, since)` and `OrgCommits(ctx, "DPGAlliance", since, emails)`).

## Notes

- The Claude model is referenced via SDK constants (e.g. `anthropic.ModelClaudeOpus4_6`), not raw strings.
- `internal/gh` authenticates by injecting a `Bearer` token through a custom `http.RoundTripper` that clones the request before mutating headers — follow that pattern rather than mutating requests in place.
- GraphQL selections are expressed as Go structs with `graphql:"..."` tags, including inline fragments (`... on Issue`); `Item` flattens the union content into one shape.
