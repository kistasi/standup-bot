package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/kistasi/standup-bot/internal/gh"
)

func main() {
	_ = godotenv.Load()

	token := os.Getenv("GITHUB_PAT")
	if token == "" {
		fmt.Fprintln(os.Stderr, "GITHUB_PAT is not set")
		os.Exit(1)
	}

	ctx := context.Background()
	client := gh.New(token)

	since := time.Now().Add(-24 * time.Hour)
	digest, err := client.MyProjectItems(ctx, "DPGAlliance", 14, since)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	printDigest(digest, 14, since)

	emails := authorEmails()
	if len(emails) == 0 {
		fmt.Fprintln(os.Stderr, "no author emails: set AUTHOR_EMAILS or configure git user.email")
		os.Exit(1)
	}

	commits, err := client.OrgCommits(ctx, "DPGAlliance", since, emails)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	printCommits(commits, since)
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

// printCommits renders a commit digest to stdout.
func printCommits(d *gh.CommitDigest, since time.Time) {
	for _, c := range d.Commits {
		fmt.Printf("[%s]\t%s\t%s\t%s\n",
			c.CommittedDate.Local().Format("2006-01-02 15:04"), c.Repo, c.Message, c.URL)
		printBody(c.Body)
	}

	fmt.Printf("\n%d commit(s) by %s in %s since %s\n",
		len(d.Commits), strings.Join(d.Emails, ", "), d.Org, since.Local().Format("2006-01-02 15:04"))
}

// printDigest renders a project digest to stdout.
func printDigest(d *gh.ProjectDigest, number int, since time.Time) {
	for _, item := range d.Items {
		url := item.URL
		if url == "" {
			url = "(draft)"
		}
		fmt.Printf("[%s]\t%s\t%s\t%s\n",
			item.UpdatedAt.Local().Format("2006-01-02 15:04"), item.State, item.Title, url)
		printBody(item.Body)
	}

	fmt.Printf("\n%d item(s) assigned to %s updated since %s in project #%d %q\n",
		len(d.Items), d.Login, since.Local().Format("2006-01-02 15:04"), number, d.Title)
}

// printBody renders a trimmed description block indented under its entry, or
// nothing when empty.
func printBody(body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}
	for _, line := range strings.Split(body, "\n") {
		fmt.Printf("    %s\n", strings.TrimRight(line, "\r"))
	}
}
