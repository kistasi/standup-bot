// Package config resolves the standup bot's runtime configuration from the
// environment (loading a .env file if present).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds everything main needs to assemble and render a digest.
type Config struct {
	GitHubToken   string   // GH_PAT
	Org           string   // GH_ORG
	ProjectNumber int      // GH_PROJECT
	AuthorEmails  []string // AUTHOR_EMAILS

	// SlackWebhookURL is optional: when set, the summary is posted to the Slack
	// incoming webhook; when empty, it is only printed.
	SlackWebhookURL string // SLACK_WEBHOOK_URL

	// GoogleCredentialsJSON is the credential shared by both Google features
	// below: a Google credentials JSON — either authorized-user credentials
	// (e.g. from `gcloud auth application-default login`, which reads the user's
	// own calendar and Drive) or a service-account key. It is optional, but
	// required by whichever of the two features is switched on.
	GoogleCredentialsJSON string // GOOGLE_CREDENTIALS_JSON

	// GoogleCalendarID switches calendar events into the digest: the calendar to
	// read (the owner's email, or "primary").
	GoogleCalendarID string // GOOGLE_CALENDAR_ID

	// GoogleDriveFolder switches on the Drive backup: the name of the My Drive
	// folder finished reports are uploaded to. It is a name rather than an ID
	// because the drive.file scope only sees files the bot created, so the bot
	// creates the folder itself — see internal/gdrive.
	GoogleDriveFolder string // GOOGLE_DRIVE_FOLDER
}

// CalendarEnabled reports whether Google Calendar is configured. Load
// guarantees the credentials accompany it, so testing the switch suffices.
func (c *Config) CalendarEnabled() bool {
	return c.GoogleCalendarID != ""
}

// DriveEnabled reports whether the Drive backup is configured. Load guarantees
// the credentials accompany it, so testing the switch suffices.
func (c *Config) DriveEnabled() bool {
	return c.GoogleDriveFolder != ""
}

// Load reads configuration from the environment. Every variable is required
// except SLACK_WEBHOOK_URL and the GOOGLE_* group: there are no defaults or
// fallbacks, so Load returns an error naming any required variable that is
// unset (or malformed, for typed values), and any half-configured Google
// feature.
func Load() (*Config, error) {
	_ = godotenv.Load()

	token, err := requireEnv("GH_PAT")
	if err != nil {
		return nil, err
	}
	org, err := requireEnv("GH_ORG")
	if err != nil {
		return nil, err
	}
	projectRaw, err := requireEnv("GH_PROJECT")
	if err != nil {
		return nil, err
	}
	project, err := strconv.Atoi(projectRaw)
	if err != nil {
		return nil, fmt.Errorf("GH_PROJECT %q: %w", projectRaw, err)
	}
	emailsRaw, err := requireEnv("AUTHOR_EMAILS")
	if err != nil {
		return nil, err
	}
	emails := splitEmails(emailsRaw)
	if len(emails) == 0 {
		return nil, fmt.Errorf("AUTHOR_EMAILS %q: no emails", emailsRaw)
	}

	// The two Google features are optional and independent, but both are driven
	// by one credential. Half a configuration is a misconfiguration, so name the
	// missing half rather than silently disabling the feature.
	credsJSON := strings.TrimSpace(os.Getenv("GOOGLE_CREDENTIALS_JSON"))
	calendarID := strings.TrimSpace(os.Getenv("GOOGLE_CALENDAR_ID"))
	driveFolder := strings.TrimSpace(os.Getenv("GOOGLE_DRIVE_FOLDER"))
	switch {
	case credsJSON == "" && calendarID != "":
		return nil, fmt.Errorf("GOOGLE_CALENDAR_ID is set but GOOGLE_CREDENTIALS_JSON is not: set both to enable calendar, or neither")
	case credsJSON == "" && driveFolder != "":
		return nil, fmt.Errorf("GOOGLE_DRIVE_FOLDER is set but GOOGLE_CREDENTIALS_JSON is not: set both to enable the Drive backup, or neither")
	case credsJSON != "" && calendarID == "" && driveFolder == "":
		return nil, fmt.Errorf("GOOGLE_CREDENTIALS_JSON is set but neither GOOGLE_CALENDAR_ID nor GOOGLE_DRIVE_FOLDER is: set one to say what it is for, or unset it")
	}

	return &Config{
		GitHubToken:           token,
		Org:                   org,
		ProjectNumber:         project,
		AuthorEmails:          emails,
		SlackWebhookURL:       strings.TrimSpace(os.Getenv("SLACK_WEBHOOK_URL")),
		GoogleCredentialsJSON: credsJSON,
		GoogleCalendarID:      calendarID,
		GoogleDriveFolder:     driveFolder,
	}, nil
}

// Since returns the start of the reporting window: the previous standup run.
// The bot runs Mon–Fri, so a weekday reaches back to yesterday, but Monday
// reaches back across the weekend to Friday. This is derived from the current
// weekday, so there is no lookback variable to keep in sync with the schedule.
func (c *Config) Since() time.Time {
	return since(time.Now())
}

// since is Since with the clock injected, for testing.
func since(now time.Time) time.Time {
	days := 1
	if now.Weekday() == time.Monday {
		days = 3 // Friday, Saturday, Sunday
	}
	return now.AddDate(0, 0, -days)
}

// PeriodLabel names the start of the reporting window for the standup message:
// "Friday" on Monday (the window reaches back across the weekend), "yesterday"
// on other weekdays. It stays in step with Since.
func (c *Config) PeriodLabel() string {
	return periodLabel(time.Now())
}

// periodLabel is PeriodLabel with the clock injected, for testing.
func periodLabel(now time.Time) string {
	if now.Weekday() == time.Monday {
		return "Friday"
	}
	return "yesterday"
}

// requireEnv returns the value of key, or an error when it is unset or empty.
func requireEnv(key string) (string, error) {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("%s is not set", key)
}

// splitEmails parses a comma-separated list of git emails, dropping blanks.
func splitEmails(raw string) []string {
	var emails []string
	for _, e := range strings.Split(raw, ",") {
		if e = strings.TrimSpace(e); e != "" {
			emails = append(emails, e)
		}
	}
	return emails
}
