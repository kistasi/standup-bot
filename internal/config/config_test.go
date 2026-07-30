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
	t.Setenv("AUTHOR_EMAILS", "a@example.com, b@example.com")
	t.Setenv("SLACK_WEBHOOK_URL", "")
	t.Setenv("GOOGLE_CREDENTIALS_JSON", "")
	t.Setenv("GOOGLE_CALENDAR_ID", "")
	t.Setenv("GOOGLE_DRIVE_FOLDER", "")
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

func TestLoad_CalendarDisabledByDefault(t *testing.T) {
	setAll(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.CalendarEnabled() {
		t.Error("CalendarEnabled() = true with no calendar vars set, want false")
	}
}

func TestLoad_CalendarEnabledWhenBothSet(t *testing.T) {
	setAll(t)
	t.Setenv("GOOGLE_CREDENTIALS_JSON", `{"type":"authorized_user"}`)
	t.Setenv("GOOGLE_CALENDAR_ID", "me@example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.CalendarEnabled() {
		t.Error("CalendarEnabled() = false with both calendar vars set, want true")
	}
	if cfg.GoogleCalendarID != "me@example.com" {
		t.Errorf("GoogleCalendarID = %q, want %q", cfg.GoogleCalendarID, "me@example.com")
	}
}

func TestLoad_DriveDisabledByDefault(t *testing.T) {
	setAll(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DriveEnabled() {
		t.Error("DriveEnabled() = true with no Drive vars set, want false")
	}
}

func TestLoad_DriveEnabledWhenBothSet(t *testing.T) {
	setAll(t)
	t.Setenv("GOOGLE_CREDENTIALS_JSON", `{"type":"authorized_user"}`)
	t.Setenv("GOOGLE_DRIVE_FOLDER", "  standup-bot  ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.DriveEnabled() {
		t.Error("DriveEnabled() = false with both Drive vars set, want true")
	}
	if cfg.GoogleDriveFolder != "standup-bot" {
		t.Errorf("GoogleDriveFolder = %q, want trimmed %q", cfg.GoogleDriveFolder, "standup-bot")
	}
	// Drive alone must not switch the calendar on: the two share credentials
	// but are independent features.
	if cfg.CalendarEnabled() {
		t.Error("CalendarEnabled() = true with only Drive configured, want false")
	}
}

func TestLoad_CalendarAndDriveTogether(t *testing.T) {
	setAll(t)
	t.Setenv("GOOGLE_CREDENTIALS_JSON", `{"type":"authorized_user"}`)
	t.Setenv("GOOGLE_CALENDAR_ID", "me@example.com")
	t.Setenv("GOOGLE_DRIVE_FOLDER", "standup-bot")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.CalendarEnabled() || !cfg.DriveEnabled() {
		t.Errorf("CalendarEnabled() = %v, DriveEnabled() = %v, want both true on one credential",
			cfg.CalendarEnabled(), cfg.DriveEnabled())
	}
}

func TestLoad_DriveFolderWithoutCredentialsErrors(t *testing.T) {
	setAll(t)
	t.Setenv("GOOGLE_DRIVE_FOLDER", "standup-bot")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error when GOOGLE_DRIVE_FOLDER has no credentials")
	}
	if !strings.Contains(err.Error(), "GOOGLE_CREDENTIALS_JSON") {
		t.Errorf("Load() error = %q, want it to name GOOGLE_CREDENTIALS_JSON", err)
	}
}

func TestLoad_CredentialsWithoutConsumerErrors(t *testing.T) {
	setAll(t)
	t.Setenv("GOOGLE_CREDENTIALS_JSON", `{"type":"authorized_user"}`)

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want error when credentials are set but nothing uses them")
	}
	// The error should point at both things it could be for.
	for _, want := range []string{"GOOGLE_CALENDAR_ID", "GOOGLE_DRIVE_FOLDER"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Load() error = %q, want it to name %s", err, want)
		}
	}
}

func TestLoad_CalendarPartialErrors(t *testing.T) {
	for _, set := range []string{"GOOGLE_CREDENTIALS_JSON", "GOOGLE_CALENDAR_ID"} {
		t.Run("only "+set, func(t *testing.T) {
			setAll(t)
			t.Setenv(set, "value")

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() error = nil, want error when only %s is set", set)
			}
			// The error should name the missing counterpart.
			missing := "GOOGLE_CALENDAR_ID"
			if set == "GOOGLE_CALENDAR_ID" {
				missing = "GOOGLE_CREDENTIALS_JSON"
			}
			if !strings.Contains(err.Error(), missing) {
				t.Errorf("Load() error = %q, want it to name missing %s", err, missing)
			}
		})
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	for _, key := range []string{"GH_PAT", "GH_ORG", "AUTHOR_EMAILS"} {
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
	// 2026-07-20 is a Monday; the days that follow are Tue–Sun.
	monday := time.Date(2026, 7, 20, 7, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		now      time.Time
		wantDays int // calendar days Since should reach back
	}{
		{"monday reaches back to friday", monday, 3},
		{"tuesday reaches back to monday", monday.AddDate(0, 0, 1), 1},
		{"friday reaches back to thursday", monday.AddDate(0, 0, 4), 1},
		{"sunday reaches back to saturday", monday.AddDate(0, 0, 6), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := since(tt.now)
			want := tt.now.AddDate(0, 0, -tt.wantDays)
			if !got.Equal(want) {
				t.Errorf("since(%s) = %v, want %v (%d days back)", tt.now.Weekday(), got, want, tt.wantDays)
			}
		})
	}
}

func TestPeriodLabel(t *testing.T) {
	// 2026-07-20 is a Monday; the days that follow are Tue–Sun.
	monday := time.Date(2026, 7, 20, 7, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		now  time.Time
		want string
	}{
		{"monday", monday, "Friday"},
		{"tuesday", monday.AddDate(0, 0, 1), "yesterday"},
		{"friday", monday.AddDate(0, 0, 4), "yesterday"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := periodLabel(tt.now); got != tt.want {
				t.Errorf("periodLabel(%s) = %q, want %q", tt.now.Weekday(), got, tt.want)
			}
		})
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
