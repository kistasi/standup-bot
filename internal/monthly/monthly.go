// Package monthly implements the CLI-only monthly report: it asks for a year
// and month, turns them into a reporting window, and writes the finished
// markdown to a file. Unlike the standup, nothing here runs on a schedule.
package monthly

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Dir is the directory reports are written to, relative to the working
// directory.
const Dir = "reports"

// Ask prompts for the year and month to report on, reading answers from in and
// writing the prompts to out. Both answers default to the month before now, so
// running the report right after a month ends is a matter of pressing enter
// twice.
func Ask(in io.Reader, out io.Writer, now time.Time) (int, time.Month, error) {
	prev := now.AddDate(0, -1, 0)

	// One buffered reader for both questions: a fresh reader per question would
	// buffer past the first newline and swallow the second answer.
	r := bufio.NewReader(in)

	year, err := askInt(r, out, "Year", prev.Year())
	if err != nil {
		return 0, 0, err
	}
	if year < 2000 || year > 2100 {
		return 0, 0, fmt.Errorf("year %d is out of range (2000-2100)", year)
	}

	month, err := askInt(r, out, "Month (1-12)", int(prev.Month()))
	if err != nil {
		return 0, 0, err
	}
	if month < 1 || month > 12 {
		return 0, 0, fmt.Errorf("month %d is out of range (1-12)", month)
	}

	return year, time.Month(month), nil
}

// askInt writes one prompt and parses the answer, returning def when the answer
// is blank. A final line without a trailing newline still counts as an answer;
// only EOF with nothing read is an error.
func askInt(r *bufio.Reader, out io.Writer, label string, def int) (int, error) {
	fmt.Fprintf(out, "%s [%d]: ", label, def)

	line, err := r.ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return 0, fmt.Errorf("reading %s: %w", label, err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	n, err := strconv.Atoi(line)
	if err != nil {
		return 0, fmt.Errorf("%s %q: not a number", label, line)
	}
	return n, nil
}

// Window returns the half-open reporting window [since, until) covering the
// given month in the local timezone: the first instant of the month, and the
// first instant of the next one.
func Window(year int, month time.Month) (time.Time, time.Time) {
	since := time.Date(year, month, 1, 0, 0, 0, 0, time.Local)
	return since, since.AddDate(0, 1, 0)
}

// Label names the month for the report's prose and heading, e.g. "June 2026".
func Label(year int, month time.Month) string {
	return fmt.Sprintf("%s %d", month, year)
}

// Write saves the report under dir as <year>-<month>.md (e.g.
// reports/2026-06.md), creating dir if needed, and returns the path written.
func Write(dir string, year int, month time.Month, report string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%04d-%02d.md", year, int(month)))

	// The model occasionally pads its reply with a blank line either side; a
	// markdown file should start on its heading and end in exactly one newline.
	report = strings.TrimSpace(report) + "\n"
	if err := os.WriteFile(path, []byte(report), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}
