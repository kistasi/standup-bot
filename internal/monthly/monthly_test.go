package monthly

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAsk(t *testing.T) {
	// now sits in July, so both defaults point at June.
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		input     string
		wantYear  int
		wantMonth time.Month
	}{
		{"explicit answers", "2025\n3\n", 2025, time.March},
		{"blank answers take the previous month", "\n\n", 2026, time.June},
		{"blank year keeps current-ish default", "\n11\n", 2026, time.November},
		{"whitespace is trimmed", "  2024  \n  2  \n", 2024, time.February},
		{"missing final newline still answers", "2024\n2", 2024, time.February},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			year, month, err := Ask(strings.NewReader(tt.input), &out, now)
			if err != nil {
				t.Fatalf("Ask() error = %v", err)
			}
			if year != tt.wantYear || month != tt.wantMonth {
				t.Errorf("Ask() = %d, %v; want %d, %v", year, month, tt.wantYear, tt.wantMonth)
			}
			// Both prompts were shown, with the previous month as the default.
			if !strings.Contains(out.String(), "Year [2026]: ") ||
				!strings.Contains(out.String(), "Month (1-12) [6]: ") {
				t.Errorf("prompts = %q, want both with June-2026 defaults", out.String())
			}
		})
	}
}

func TestAsk_JanuaryDefaultsToPreviousDecember(t *testing.T) {
	// The default is the previous month, which in January means the previous
	// year's December.
	now := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	year, month, err := Ask(strings.NewReader("\n\n"), &strings.Builder{}, now)
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if year != 2025 || month != time.December {
		t.Errorf("Ask() = %d, %v; want 2025, December", year, month)
	}
}

func TestAsk_Errors(t *testing.T) {
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"non-numeric year", "june\n", "not a number"},
		{"non-numeric month", "2026\nJune\n", "not a number"},
		{"year out of range", "1999\n", "out of range"},
		{"month out of range", "2026\n13\n", "out of range"},
		{"month zero", "2026\n0\n", "out of range"},
		{"no input at all", "", "reading Year"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Ask(strings.NewReader(tt.input), &strings.Builder{}, now)
			if err == nil {
				t.Fatalf("Ask() error = nil, want one containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Ask() error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestWindow(t *testing.T) {
	since, until := Window(2026, time.June)

	wantSince := time.Date(2026, 6, 1, 0, 0, 0, 0, time.Local)
	wantUntil := time.Date(2026, 7, 1, 0, 0, 0, 0, time.Local)
	if !since.Equal(wantSince) || !until.Equal(wantUntil) {
		t.Errorf("Window(2026, June) = %v, %v; want %v, %v", since, until, wantSince, wantUntil)
	}
}

func TestWindow_DecemberRollsOverTheYear(t *testing.T) {
	_, until := Window(2026, time.December)

	want := time.Date(2027, 1, 1, 0, 0, 0, 0, time.Local)
	if !until.Equal(want) {
		t.Errorf("Window(2026, December) until = %v, want %v", until, want)
	}
}

func TestLabel(t *testing.T) {
	if got := Label(2026, time.June); got != "June 2026" {
		t.Errorf("Label(2026, June) = %q, want %q", got, "June 2026")
	}
}

func TestWrite(t *testing.T) {
	// A nested directory that does not exist yet: Write must create it.
	dir := filepath.Join(t.TempDir(), "reports")

	path, err := Write(dir, 2026, time.June, "# June 2026\n\nDid things.")
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if want := filepath.Join(dir, "2026-06-monthly-report.md"); path != want {
		t.Errorf("Write() path = %q, want %q", path, want)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading report: %v", err)
	}
	// The report is saved verbatim apart from a guaranteed trailing newline.
	if want := "# June 2026\n\nDid things.\n"; string(got) != want {
		t.Errorf("report = %q, want %q", got, want)
	}
}

func TestWrite_TrimsSurroundingBlankLines(t *testing.T) {
	// The model sometimes pads its reply; the file should still start on the
	// heading and end in exactly one newline.
	dir := t.TempDir()
	path, err := Write(dir, 2026, time.December, "\n# December 2026\n\n\n")
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if want := filepath.Join(dir, "2026-12-monthly-report.md"); path != want {
		t.Errorf("Write() path = %q, want %q (month zero-padded)", path, want)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "# December 2026\n" {
		t.Errorf("report = %q, want the padding trimmed to one trailing newline", got)
	}
}

func TestWrite_Overwrites(t *testing.T) {
	// Re-running the report for the same month replaces the previous file
	// rather than appending to it.
	dir := t.TempDir()
	if _, err := Write(dir, 2026, time.June, "old"); err != nil {
		t.Fatalf("first Write() error = %v", err)
	}
	path, err := Write(dir, 2026, time.June, "new")
	if err != nil {
		t.Fatalf("second Write() error = %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "new\n" {
		t.Errorf("report = %q, want the second write to replace the first", got)
	}
}
