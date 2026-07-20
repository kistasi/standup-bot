// Package config resolves the standup bot's runtime configuration from the
// environment (loading a .env file if present).
package config

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Defaults for the org project the digest is assembled from. These were
// previously hardcoded in main.go; they now serve as fallbacks for the
// STANDUP_ORG / STANDUP_PROJECT / STANDUP_LOOKBACK env vars.
const (
	defaultOrg           = "DPGAlliance"
	defaultProjectNumber = 14
	defaultLookback      = 24 * time.Hour
)

// Config holds everything main needs to assemble and render a digest.
type Config struct {
	GitHubToken   string        // GITHUB_PAT; required
	Org           string        // STANDUP_ORG
	ProjectNumber int           // STANDUP_PROJECT
	Lookback      time.Duration // STANDUP_LOOKBACK; the reporting window
	AuthorEmails  []string      // AUTHOR_EMAILS, or `git config user.email`
	Summarize     bool          // STANDUP_SUMMARIZE; feed the digest to Claude
}

// Load reads configuration from the environment, applying defaults. It returns
// an error when a required value is missing or a typed value is malformed.
func Load() (*Config, error) {
	_ = godotenv.Load()

	token := os.Getenv("GITHUB_PAT")
	if token == "" {
		return nil, errors.New("GITHUB_PAT is not set")
	}

	cfg := &Config{
		GitHubToken:   token,
		Org:           envOr("STANDUP_ORG", defaultOrg),
		ProjectNumber: defaultProjectNumber,
		Lookback:      defaultLookback,
		AuthorEmails:  authorEmails(),
		Summarize:     boolEnv("STANDUP_SUMMARIZE"),
	}

	if raw := os.Getenv("STANDUP_PROJECT"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("STANDUP_PROJECT %q: %w", raw, err)
		}
		cfg.ProjectNumber = n
	}
	if raw := os.Getenv("STANDUP_LOOKBACK"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("STANDUP_LOOKBACK %q: %w", raw, err)
		}
		cfg.Lookback = d
	}

	if len(cfg.AuthorEmails) == 0 {
		return nil, errors.New("no author emails: set AUTHOR_EMAILS or configure git user.email")
	}

	return cfg, nil
}

// Since returns the start of the reporting window (now minus the lookback).
func (c *Config) Since() time.Time {
	return time.Now().Add(-c.Lookback)
}

// authorEmails resolves the git emails to attribute "my commits" to. It reads
// AUTHOR_EMAILS (comma-separated) and falls back to `git config user.email`.
func authorEmails() []string {
	if raw := os.Getenv("AUTHOR_EMAILS"); raw != "" {
		var emails []string
		for _, e := range strings.Split(raw, ",") {
			if e = strings.TrimSpace(e); e != "" {
				emails = append(emails, e)
			}
		}
		return emails
	}
	out, err := exec.Command("git", "config", "user.email").Output()
	if err != nil {
		return nil
	}
	if e := strings.TrimSpace(string(out)); e != "" {
		return []string{e}
	}
	return nil
}

// envOr returns the value of key, or fallback when it is unset or empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// boolEnv reports whether key is set to a truthy value ("1", "true", "yes").
func boolEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}
