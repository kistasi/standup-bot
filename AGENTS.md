# AGENTS.md

This file provides guidance to agentic coding tools when working with code in this repository.

## Overview

`standup-bot` is an early-stage Go application (module `github.com/kistasi/standup-bot`) that assembles standup digests from GitHub project activity (and, optionally, Google Calendar meetings), summarizes them with Claude, and optionally posts the result to Slack. It is organized into a thin `main` plus six internal packages:

- `main.go` — entry point. `run()` loads config, builds a `gh.Client`, fetches the viewer's project items and recent commits, fetches calendar events when `cfg.CalendarEnabled()` (else `events` stays nil), renders them via `internal/render`, feeds the digest to `claude.Ask` for a standup summary, prints it, and — when `SLACK_WEBHOOK_URL` is set — posts it via `slack.Post`. `main` only maps a `run()` error to stderr + exit code.
- `internal/config` — `Load()` resolves configuration from the environment (loading `.env` via `godotenv`). Required vars (no defaults/fallbacks; `Load()` errors naming any unset or malformed one): `GH_PAT`, `GH_ORG`, `GH_PROJECT` (int), and `AUTHOR_EMAILS` (comma-separated git emails). Optional vars: `SLACK_WEBHOOK_URL` (when set, the summary is posted to Slack; when empty, only printed) and the calendar pair `GOOGLE_CREDENTIALS_JSON` / `GOOGLE_CALENDAR_ID` — both must be set together (`Load()` errors if exactly one is set), and `Config.CalendarEnabled()` reports whether they are. `Config.Since()` returns the start of the reporting window computed from the weekday (via the injectable `since(now)`): a weekday reaches back one day, but Monday reaches back three (across the weekend to Friday). There is no lookback variable — the window can't drift out of sync with the Mon–Fri schedule. `Config.PeriodLabel()` (via `periodLabel(now)`) names that window for the standup message — "yesterday" on weekdays, "Friday" on Monday — and `main` feeds it to `claude.StandupSystem`.
- `internal/render` — `Digest(project, commits, events, number, since)` formats the digests into the indented plain-text report (project items, then commits, then calendar events) that is printed or summarized. `events` may be nil (calendar disabled), in which case the calendar section is omitted. Descriptions are printed indented under each entry so the whole thing can be fed to the Claude API.
- `internal/gh` — typed wrapper around the GitHub GraphQL v4 API (via `shurcooL/githubv4`). `New(token)` builds a bearer-authenticated client.
  - `MyProjectItems(ctx, org, number, since)` pages through an org ProjectV2, flattening Issue / PullRequest / DraftIssue nodes into a `ProjectDigest` filtered to the PAT owner (`viewer`) and to items updated on or after `since`. Each `Item` carries the issue/PR/draft `Body`.
  - `OrgCommits(ctx, org, since, emails)` pages through the org's repositories newest-pushed-first (stopping once a repo's last push predates `since`) and collects default-branch commits authored by any of `emails`, returning a `CommitDigest`. Each `Commit` carries the headline (`Message`) and full `Body`. Filtering is by **author email** (`author:{emails:...}`), not GitHub account: the contributions API and `author:{id:...}` filter both require the commit email to be linked to the account, and silently return nothing when it isn't. Release-automation commits (headline starting `release `) and merge commits (2+ parents) are skipped as standup noise.
- `internal/gcal` — wrapper around the Google Calendar v3 API (`google.golang.org/api/calendar/v3`). `New(ctx, credsJSON, calendarID)` builds a read-only client from a Google credentials JSON via `option.WithCredentialsJSON` — either **authorized-user** creds (from `gcloud auth application-default login --scopes=…calendar.readonly`, reading the signed-in user's own calendar with full event details — the approach used here, since Workspace domains often restrict external calendar sharing to free/busy) or a **service-account** key (needs the calendar shared read-only with the SA email). `Events(ctx, since, until)` lists the calendar's timed events in the window (`SingleEvents(true)` expands recurrences; paginates on `NextPageToken`), returning a `Digest`. `toEvent` drops standup noise: cancelled instances, all-day events (a `Start.Date` with empty `Start.DateTime`), and events the owner declined (the `Self` attendee's `ResponseStatus == "declined"`).
- `internal/claude` — thin wrapper around the Anthropic Messages API. `New()` builds a client; `Ask(ctx, system, prompt)` sends a single prompt and returns the reply. `StandupSystem(period)` builds the system instruction used to turn a digest (project items, commits, and calendar meetings) into a standup update; `period` is the reporting-window label that opens the message ("My <period> was about:") — `main` passes `cfg.PeriodLabel()` ("yesterday" on weekdays, "Friday" on Monday, matching `Since`). `main` always runs the digest through it.
- `internal/slack` — posts messages to a Slack incoming webhook using stdlib `net/http` (no new deps). `New(webhookURL)` mirrors `gh.New(token)` (the URL is the credential); `Post(ctx, text)` sends `{"text": text}` as JSON with a 10s-timeout client and treats any non-`200`/non-`ok` response as an error. `toMrkdwn` first converts the one bit of standard Markdown Claude emits that Slack's mrkdwn spells differently — `**bold**` becomes `*bold*` (`` `code` `` is the same in both, and the prompt omits links). `main` posts only when `SLACK_WEBHOOK_URL` is configured.

## Commands

```bash
go run .          # run the program
go build ./...    # compile
go test ./...     # run all tests (every internal package has tests)
go test -run TestName ./path  # run a single test
go mod tidy       # sync go.mod/go.sum after changing imports
```

Requires Go 1.26+.

## Configuration

Env vars are loaded from a `.env` file via `godotenv.Load()` in `config.Load()` (`.env` is gitignored). Every variable below is required except `SLACK_WEBHOOK_URL` and the calendar pair — `config.Load()` errors naming any required one that is unset (or malformed). Copy `.env.example` to `.env` and fill in:

- `GH_PAT` — GitHub personal access token.
- `ANTHROPIC_API_KEY` — read automatically by the Anthropic SDK. `claude.New()` / `anthropic.NewClient()` take no explicit key argument. Always needed, since the digest is always summarized with Claude.
- `AUTHOR_EMAILS` — comma-separated git emails to attribute commits to in `OrgCommits`. `config.Load()` errors if it yields no email.
- `GH_ORG` / `GH_PROJECT` — the project the digest is built from (`GH_PROJECT` is an int). The reporting window is no longer configurable; `config.Since()` derives it from the weekday.
- `SLACK_WEBHOOK_URL` — optional Slack incoming-webhook URL. When set, the summary is posted there in addition to being printed; when empty, it is only printed.
- `GOOGLE_CREDENTIALS_JSON` / `GOOGLE_CALENDAR_ID` — optional, and travel together (setting exactly one is an error). `GOOGLE_CREDENTIALS_JSON` is a Google credentials JSON (single line): authorized-user creds from `gcloud auth application-default login --scopes=openid,https://www.googleapis.com/auth/calendar.readonly` (reads your own calendar, full details), or a service-account key (needs the calendar shared with the SA email). `GOOGLE_CALENDAR_ID` is the calendar to read (your email, or `primary`). When both are set, calendar events feed the digest.

## Scheduling

`.github/workflows/standup.yml` runs `go run .` on a cron (`0 5 * * 1-5` — 05:00 UTC, Mon–Fri) and via `workflow_dispatch`. That's 07:00 Budapest during summer (CEST); GitHub cron is UTC with no DST support, so in winter (CET) it lands at 06:00 Budapest. Cron may be delayed a few minutes under GitHub load. Secrets `GH_PAT` / `ANTHROPIC_API_KEY` / `SLACK_WEBHOOK_URL` / `GOOGLE_CREDENTIALS_JSON` and repo Variables `GH_ORG` / `GH_PROJECT` / `AUTHOR_EMAILS` / `GOOGLE_CALENDAR_ID` feed the job's env (`GOOGLE_*` optional, both or neither). The reporting window is derived from the weekday in `config.Since()` (Monday reaches back to Friday, other weekdays to yesterday), so Fri + the weekend are covered with no lookback variable to tune.

`.github/workflows/test.yml` is CI: on push to `main`, on any pull request, and via `workflow_dispatch` it runs `go build ./...`, `go vet ./...`, and `go test -race -cover ./...`. It needs no secrets or variables.

## Notes

- The Claude model is referenced via SDK constants (e.g. `anthropic.ModelClaudeOpus4_8`), not raw strings.
- `internal/gh` authenticates by injecting a `Bearer` token through a custom `http.RoundTripper` that clones the request before mutating headers — follow that pattern rather than mutating requests in place.
- GraphQL selections are expressed as Go structs with `graphql:"..."` tags, including inline fragments (`... on Issue`); `Item` flattens the union content into one shape.
