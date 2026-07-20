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
	GitHubToken   string        // GH_PAT
	Org           string        // GH_ORG
	ProjectNumber int           // GH_PROJECT
	Lookback      time.Duration // GH_LOOKBACK; the reporting window
	AuthorEmails  []string      // AUTHOR_EMAILS

	// SlackWebhookURL is the only optional variable: when set, the summary is
	// posted to the Slack incoming webhook; when empty, it is only printed.
	SlackWebhookURL string // SLACK_WEBHOOK_URL
}

// Load reads configuration from the environment. Every variable is required
// except SLACK_WEBHOOK_URL: there are no defaults or fallbacks, so Load returns
// an error naming any required variable that is unset (or malformed, for typed
// values).
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
	lookbackRaw, err := requireEnv("GH_LOOKBACK")
	if err != nil {
		return nil, err
	}
	lookback, err := time.ParseDuration(lookbackRaw)
	if err != nil {
		return nil, fmt.Errorf("GH_LOOKBACK %q: %w", lookbackRaw, err)
	}
	emailsRaw, err := requireEnv("AUTHOR_EMAILS")
	if err != nil {
		return nil, err
	}
	emails := splitEmails(emailsRaw)
	if len(emails) == 0 {
		return nil, fmt.Errorf("AUTHOR_EMAILS %q: no emails", emailsRaw)
	}

	return &Config{
		GitHubToken:     token,
		Org:             org,
		ProjectNumber:   project,
		Lookback:        lookback,
		AuthorEmails:    emails,
		SlackWebhookURL: strings.TrimSpace(os.Getenv("SLACK_WEBHOOK_URL")),
	}, nil
}

// Since returns the start of the reporting window (now minus the lookback).
func (c *Config) Since() time.Time {
	return time.Now().Add(-c.Lookback)
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
