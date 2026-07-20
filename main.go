package main

import (
	"context"
	"fmt"
	"os"
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
	}

	fmt.Printf("\n%d item(s) assigned to %s updated since %s in project #%d %q\n",
		len(d.Items), d.Login, since.Local().Format("2006-01-02 15:04"), number, d.Title)
}
