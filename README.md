# standup-bot

I'm too lazy to summarize stuff myself.

`standup-bot` assembles a standup digest from your GitHub project activity —
the project items and commits you touched since your last standup, plus the
meetings you attended when Google Calendar is configured — summarizes it
with Claude, prints the result, and (optionally) posts it to Slack.

It also writes a **monthly report** on demand — see [Monthly report](#monthly-report).

## How it works

1. Load config from the environment (`.env` in development).
2. Fetch your [ProjectV2](https://docs.github.com/en/issues/planning-and-tracking-with-projects)
   items and recent org commits since your last standup via the GitHub GraphQL API.
3. When Google Calendar is configured, fetch the events in the same window
   (declined and all-day events are skipped).
4. Render them into a plain-text digest.
5. Feed the digest to Claude for a standup-style summary.
6. Print the summary, and post it to Slack when `SLACK_WEBHOOK_URL` is set.

## Setup

Requires Go 1.26+.

```bash
cp .env.example .env   # then fill in the values below
go run .
```

## Configuration

All variables are required except `SLACK_WEBHOOK_URL` and the two Google
Calendar variables. `config.Load()` errors naming any required one that is unset
or malformed.

| Variable                  | Description                                                                                                                                          |
| ------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ANTHROPIC_API_KEY`       | Anthropic API key (read automatically by the SDK).                                                                                                   |
| `GH_PAT`                  | GitHub personal access token.                                                                                                                        |
| `GH_ORG`                  | GitHub organization to read from.                                                                                                                    |
| `GH_PROJECT`              | ProjectV2 number (integer).                                                                                                                          |
| `AUTHOR_EMAILS`           | Comma-separated git author emails to attribute commits to.                                                                                           |
| `SLACK_WEBHOOK_URL`       | Optional. When set, the summary is posted to this Slack incoming webhook; otherwise it is only printed.                                              |
| `GOOGLE_CREDENTIALS_JSON` | Optional (paired with `GOOGLE_CALENDAR_ID`). A Google credentials JSON on one line — authorized-user creds (from `gcloud`) or a service-account key. |
| `GOOGLE_CALENDAR_ID`      | Optional (paired with `GOOGLE_CREDENTIALS_JSON`). The calendar to read — your email, or `primary`.                                                   |

> Commits are matched by **author email**, so `AUTHOR_EMAILS` must list the
> emails your commits are actually authored with.

### Google Calendar

Calendar is off unless **both** `GOOGLE_CREDENTIALS_JSON` and
`GOOGLE_CALENDAR_ID` are set (setting only one is an error). The bot reads the
calendar read-only with a Google credentials JSON; the underlying package
accepts either authorized-user or service-account credentials.

The simplest way to read **your own** calendar with full event details — and the
one that needs no Workspace admin or calendar sharing — is authorized-user
credentials via `gcloud`:

1. Enable the **Google Calendar API** in a Google Cloud project.
2. Mint user credentials with a read-only calendar scope:
   ```bash
   gcloud auth application-default login \
     --scopes=openid,https://www.googleapis.com/auth/calendar.readonly
   ```
3. Put the resulting file (`~/.config/gcloud/application_default_credentials.json`,
   minified to one line) in `GOOGLE_CREDENTIALS_JSON`, and set
   `GOOGLE_CALENDAR_ID` to your email (or `primary`). The refresh token in that
   file keeps working headlessly in CI.

Alternatively, a **service-account** key works if you can share the target
calendar (read-only, "See all event details") with the service account's email —
but many Google Workspace domains restrict external sharing to free/busy only,
which hides event titles.

Events in the reporting window are included, except all-day events and ones you
declined.

## Monthly report

`go run . monthly` writes a longer, written report for a whole month. It is
CLI-only — nothing schedules it, and it never posts to Slack.

```bash
$ go run . monthly
Year [2026]:
Month (1-12) [6]:
Collecting June 2026 activity...
Writing the report...
reports/2026-06.md
```

Both questions default to the previous month, so pressing enter twice right
after a month ends does the obvious thing. The report covers the project items
you touched and the commits you authored between the first instant of that month
and the first instant of the next; it is summarized by Claude as markdown and
saved to `reports/<year>-<month>.md`, overwriting any earlier run for the same
month. `reports/` is gitignored.

Calendar events are left out: a month of meetings is noise in a written report.
Configuration is otherwise the same as the standup's, minus `SLACK_WEBHOOK_URL`
and the Google variables.

## Commands

```bash
go run .          # run the standup
go run . monthly  # write a monthly report (interactive)
go build ./...    # compile
go test ./...     # run all tests
```

## Scheduling

`.github/workflows/standup.yml` runs the bot on a cron (`0 5 * * 1-5` —
05:00 UTC = 07:00 Budapest in summer, Mon–Fri) and on demand via
`workflow_dispatch`. It reads secrets `GH_PAT` / `ANTHROPIC_API_KEY` /
`SLACK_WEBHOOK_URL` / `GOOGLE_CREDENTIALS_JSON` and repo variables `GH_ORG` /
`GH_PROJECT` / `AUTHOR_EMAILS` / `GOOGLE_CALENDAR_ID` (the last two of each pair
optional).

The reporting window is derived from the weekday: a weekday run reaches back to
the previous day, and Monday's run reaches back across the weekend to Friday, so
nothing is missed without a lookback variable to tune.
