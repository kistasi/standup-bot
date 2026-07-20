# standup-bot

I'm too lazy to summarize stuff myself.

`standup-bot` assembles a standup digest from your GitHub project activity —
the project items and commits you touched over a lookback window — summarizes it
with Claude, prints the result, and (optionally) posts it to Slack.

## How it works

1. Load config from the environment (`.env` in development).
2. Fetch your [ProjectV2](https://docs.github.com/en/issues/planning-and-tracking-with-projects)
   items and recent org commits over the last `GH_LOOKBACK` via the GitHub GraphQL API.
3. Render them into a plain-text digest.
4. Feed the digest to Claude for a standup-style summary.
5. Print the summary, and post it to Slack when `SLACK_WEBHOOK_URL` is set.

## Setup

Requires Go 1.26+.

```bash
cp .env.example .env   # then fill in the values below
go run .
```

## Configuration

All variables are required except `SLACK_WEBHOOK_URL`. `config.Load()` errors
naming any required one that is unset or malformed.

| Variable | Description |
| --- | --- |
| `ANTHROPIC_API_KEY` | Anthropic API key (read automatically by the SDK). |
| `GH_PAT` | GitHub personal access token. |
| `GH_ORG` | GitHub organization to read from. |
| `GH_PROJECT` | ProjectV2 number (integer). |
| `GH_LOOKBACK` | How far back to look, as a Go duration (e.g. `24h`, `72h`). |
| `AUTHOR_EMAILS` | Comma-separated git author emails to attribute commits to. |
| `SLACK_WEBHOOK_URL` | Optional. When set, the summary is posted to this Slack incoming webhook; otherwise it is only printed. |

> Commits are matched by **author email**, so `AUTHOR_EMAILS` must list the
> emails your commits are actually authored with.

## Commands

```bash
go run .          # run the program
go build ./...    # compile
go test ./...     # run all tests
```

## Scheduling

`.github/workflows/standup.yml` runs the bot on a cron (`0 7 * * 1-5` —
07:00 UTC, Mon–Fri) and on demand via `workflow_dispatch`. It reads secrets
`GH_PAT` / `ANTHROPIC_API_KEY` / `SLACK_WEBHOOK_URL` and repo variables
`GH_ORG` / `GH_PROJECT` / `GH_LOOKBACK` / `AUTHOR_EMAILS`.

Monday's run only reaches back `GH_LOOKBACK`, so a 24h window misses Friday and
the weekend — widen `GH_LOOKBACK` (e.g. `72h`) to cover it.
