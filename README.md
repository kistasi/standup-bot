# standup-bot

I'm too lazy to summarize stuff myself.

`standup-bot` assembles a standup digest from the code you wrote — the pull
requests you opened and the commits you authored since your last standup, plus
the meetings you attended when Google Calendar is configured and the hours you
logged when Toggl is — summarizes it with Claude, prints the result, and
(optionally) posts it to Slack.

Tickets are deliberately out of scope: the digest follows commits and pull
requests, and anything on a **release branch** is reported as *delivered*.

It also writes a **monthly report** on demand — see [Monthly report](#monthly-report).

## How it works

1. Load config from the environment (`.env` in development).
2. Fetch your pull requests and recent org commits since your last standup via
   the GitHub GraphQL API. A pull request opened from a release branch (or
   titled `Release …`), and a release commit, are flagged as delivered.
3. When Google Calendar is configured, fetch the events in the same window
   (declined and all-day events are skipped).
4. When Toggl is configured, fetch the time entries that started in the window.
5. Render them into a plain-text digest.
6. Feed the digest to Claude for a standup-style summary, which links the pull
   requests behind each bullet and calls out what was delivered.
7. Print the summary, and post it to Slack when `SLACK_WEBHOOK_URL` is set.

## Setup

Requires Go 1.26+.

```bash
cp .env.example .env   # then fill in the values below
go run .
```

## Configuration

All variables are required except `SLACK_WEBHOOK_URL`, `TOGGL_API_TOKEN` and
the two Google Calendar variables. `config.Load()` errors naming any required one that is unset
or malformed.

| Variable                  | Description                                                                                                                                          |
| ------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ANTHROPIC_API_KEY`       | Anthropic API key (read automatically by the SDK).                                                                                                   |
| `GH_PAT`                  | GitHub personal access token.                                                                                                                        |
| `GH_ORG`                  | GitHub organization to read from.                                                                                                                    |
| `AUTHOR_EMAILS`           | Comma-separated git author emails to attribute commits to.                                                                                           |
| `SLACK_WEBHOOK_URL`       | Optional. When set, the summary is posted to this Slack incoming webhook; otherwise it is only printed.                                              |
| `TOGGL_API_TOKEN`         | Optional. When set, the time you tracked in the window is added to the digest. See [Toggl](#toggl).                                                   |
| `GOOGLE_CREDENTIALS_JSON` | Optional (paired with `GOOGLE_CALENDAR_ID`). A Google credentials JSON on one line — authorized-user creds (from `gcloud`) or a service-account key. |
| `GOOGLE_CALENDAR_ID`      | Optional (paired with `GOOGLE_CREDENTIALS_JSON`). The calendar to read — your email, or `primary`.                                                   |

> Commits are matched by **author email**, so `AUTHOR_EMAILS` must list the
> emails your commits are actually authored with.

### Toggl

Time tracking is off unless `TOGGL_API_TOKEN` is set; the token is the whole
configuration (no second variable, no OAuth). Copy it from your Toggl profile
page — the bot sends it as the username of an HTTP Basic pair, which is how
Toggl's API takes a token.

Two limits of the API are worth knowing, both handled rather than hidden:

- **~3 months of history.** `go run . monthly` for an older month prints
  `Skipping tracked time: …` on stderr and writes the report without its hours,
  rather than failing. The standup's window is days, so it is never affected.
- **30 requests per hour**, user-scoped, on every Toggl plan. Each run makes
  exactly one request (the endpoint has no pagination, and `meta=true` resolves
  project names inline), so a daily standup plus a monthly report is nowhere
  near the ceiling — but repeated CI retries could be.

A still-running timer is included and flagged as approximate: its duration is
measured from its start, because Toggl reports a running entry's `duration` as a
negative sentinel rather than an elapsed time.

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
reports/2026-06-monthly-report.md
```

Both questions default to the previous month, so pressing enter twice right
after a month ends does the obvious thing. The report covers the pull requests
you opened and the commits you authored between the first instant of that month
and the first instant of the next; it is summarized by Claude as markdown and
saved to `reports/<year>-<month>-monthly-report.md`, overwriting any earlier run for the same
month. `reports/` is gitignored.

Calendar events are left out: a month of meetings is noise in a written report.
Tracked time is included when `TOGGL_API_TOKEN` is set and the month falls
inside Toggl's ~3-month window. Configuration is otherwise the same as the
standup's, minus `SLACK_WEBHOOK_URL` and the Google variables.

## Commands

```bash
make              # list the available targets
make standup      # run the standup            (go run .)
make monthly      # write a monthly report     (go run . monthly)
make build        # compile                    (go build -o standup-bot .)
make test         # run the tests as CI does   (go test -race -cover ./...)
make check        # fmt + vet + test
```

## Scheduling

`.github/workflows/standup.yml` runs the bot on a cron (`0 5 * * 1-5` —
05:00 UTC = 07:00 Budapest in summer, Mon–Fri) and on demand via
`workflow_dispatch`. It reads secrets `GH_PAT` / `ANTHROPIC_API_KEY` /
`SLACK_WEBHOOK_URL` / `TOGGL_API_TOKEN` / `GOOGLE_CREDENTIALS_JSON` and repo
variables `GH_ORG` / `AUTHOR_EMAILS` / `GOOGLE_CALENDAR_ID` (every secret after
the second, and the last variable, optional).

The reporting window is derived from the weekday: a weekday run reaches back to
the previous day, and Monday's run reaches back across the weekend to Friday, so
nothing is missed without a lookback variable to tune.
