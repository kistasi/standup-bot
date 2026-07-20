# AGENTS.md

This file provides guidance to agentic coding tools when working with code in this repository.

## Overview

`standup-bot` is an early-stage Go application (module `github.com/kistasi/standup-bot`) that assembles standup digests from GitHub project activity, with Claude available to summarize it. It is organized into a small `main` plus two internal packages:

- `main.go` — entry point. Loads env vars, builds a `gh.Client`, fetches the viewer's project items updated in the last 24h, and prints a digest to stdout.
- `internal/gh` — typed wrapper around the GitHub GraphQL v4 API (via `shurcooL/githubv4`). `New(token)` builds a bearer-authenticated client; `MyProjectItems(ctx, org, number, since)` pages through an org ProjectV2, flattening Issue / PullRequest / DraftIssue nodes into a `ProjectDigest` filtered to the PAT owner (`viewer`) and to items updated on or after `since`.
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

The org and project number are currently hardcoded in `main.go` (`MyProjectItems(ctx, "DPGAlliance", 14, since)`).

## Notes

- The Claude model is referenced via SDK constants (e.g. `anthropic.ModelClaudeOpus4_6`), not raw strings.
- `internal/gh` authenticates by injecting a `Bearer` token through a custom `http.RoundTripper` that clones the request before mutating headers — follow that pattern rather than mutating requests in place.
- GraphQL selections are expressed as Go structs with `graphql:"..."` tags, including inline fragments (`... on Issue`); `Item` flattens the union content into one shape.
