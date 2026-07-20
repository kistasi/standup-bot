package config

import (
	"strings"
	"testing"
	"time"
)

// setAll sets every required variable to a valid value and clears the optional
// Slack URL, so individual tests can override or unset one field at a time.
func setAll(t *testing.T) {
	t.Helper()
	t.Setenv("GH_PAT", "token123")
	t.Setenv("GH_ORG", "acme")
	t.Setenv("GH_PROJECT", "42")
	t.Setenv("GH_LOOKBACK", "24h")
	t.Setenv("AUTHOR_EMAILS", "a@example.com, b@example.com")
	t.Setenv("SLACK_WEBHOOK_URL", "")
}

func TestLoad_Valid(t *testing.T) {
	setAll(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.GitHubToken != "token123" {
		t.Errorf("GitHubToken = %q, want %q", cfg.GitHubToken, "token123")
	}
	if cfg.Org != "acme" {
		t.Errorf("Org = %q, want %q", cfg.Org, "acme")
	}
	if cfg.ProjectNumber != 42 {
		t.Errorf("ProjectNumber = %d, want 42", cfg.ProjectNumber)
	}
	if cfg.Lookback != 24*time.Hour {
		t.Errorf("Lookback = %v, want 24h", cfg.Lookback)
	}
	want := []string{"a@example.com", "b@example.com"}
	if len(cfg.AuthorEmails) != len(want) {
		t.Fatalf("AuthorEmails = %v, want %v", cfg.AuthorEmails, want)
	}
	for i, e := range want {
		if cfg.AuthorEmails[i] != e {
			t.Errorf("AuthorEmails[%d] = %q, want %q", i, cfg.AuthorEmails[i], e)
		}
	}
	if cfg.SlackWebhookURL != "" {
		t.Errorf("SlackWebhookURL = %q, want empty", cfg.SlackWebhookURL)
	}
}

func TestLoad_SlackOptional(t *testing.T) {
	setAll(t)
	// Whitespace around the URL is trimmed.
	t.Setenv("SLACK_WEBHOOK_URL", "  https://hooks.slack.com/x  ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SlackWebhookURL != "https://hooks.slack.com/x" {
		t.Errorf("SlackWebhookURL = %q, want trimmed URL", cfg.SlackWebhookURL)
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	for _, key := range []string{"GH_PAT", "GH_ORG", "GH_PROJECT", "GH_LOOKBACK", "AUTHOR_EMAILS"} {
		t.Run(key, func(t *testing.T) {
			setAll(t)
			t.Setenv(key, "") // requireEnv treats empty/whitespace as unset

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() error = nil, want error naming %s", key)
			}
			if !strings.Contains(err.Error(), key) {
				t.Errorf("Load() error = %q, want it to name %s", err, key)
			}
		})
	}
}

func TestLoad_WhitespaceOnlyRequiredIsMissing(t *testing.T) {
	setAll(t)
	t.Setenv("GH_ORG", "   ")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error for whitespace-only GH_ORG")
	}
}

func TestLoad_MalformedProject(t *testing.T) {
	setAll(t)
	t.Setenv("GH_PROJECT", "not-a-number")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error for non-integer GH_PROJECT")
	}
	if !strings.Contains(err.Error(), "GH_PROJECT") {
		t.Errorf("Load() error = %q, want it to name GH_PROJECT", err)
	}
}

func TestLoad_MalformedLookback(t *testing.T) {
	setAll(t)
	t.Setenv("GH_LOOKBACK", "yesterday")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error for malformed GH_LOOKBACK")
	}
	if !strings.Contains(err.Error(), "GH_LOOKBACK") {
		t.Errorf("Load() error = %q, want it to name GH_LOOKBACK", err)
	}
}

func TestLoad_EmailsAllBlank(t *testing.T) {
	setAll(t)
	// Non-empty (so requireEnv passes) but yields no emails after splitting.
	t.Setenv("AUTHOR_EMAILS", " , , ")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error for AUTHOR_EMAILS with no emails")
	}
	if !strings.Contains(err.Error(), "AUTHOR_EMAILS") {
		t.Errorf("Load() error = %q, want it to name AUTHOR_EMAILS", err)
	}
}

func TestSince(t *testing.T) {
	cfg := &Config{Lookback: 2 * time.Hour}

	before := time.Now().Add(-2 * time.Hour)
	got := cfg.Since()
	after := time.Now().Add(-2 * time.Hour)

	if got.Before(before.Add(-time.Second)) || got.After(after.Add(time.Second)) {
		t.Errorf("Since() = %v, want ~now-2h (between %v and %v)", got, before, after)
	}
}

func TestSplitEmails(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"single", "a@x.com", []string{"a@x.com"}},
		{"multiple with spaces", " a@x.com , b@y.com ", []string{"a@x.com", "b@y.com"}},
		{"trailing comma", "a@x.com,", []string{"a@x.com"}},
		{"blanks only", " , ,", nil},
		{"empty", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitEmails(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("splitEmails(%q) = %v, want %v", tt.in, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("splitEmails(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRequireEnv(t *testing.T) {
	t.Setenv("SOME_KEY", "  value  ")
	got, err := requireEnv("SOME_KEY")
	if err != nil {
		t.Fatalf("requireEnv error = %v", err)
	}
	if got != "value" {
		t.Errorf("requireEnv = %q, want trimmed %q", got, "value")
	}

	t.Setenv("SOME_KEY", "   ")
	if _, err := requireEnv("SOME_KEY"); err == nil {
		t.Error("requireEnv on whitespace-only = nil error, want error")
	}
}
