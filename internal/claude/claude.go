// Package claude wraps the Anthropic Messages API for the standup bot.
package claude

import (
	"context"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// StandupSystem is the system instruction for turning a raw digest into a
// concise standup update. period names the reporting window in the opening
// line — "yesterday" on a normal weekday, "Friday" on a Monday whose window
// reaches back across the weekend.
func StandupSystem(period string) string {
	return "You are a standup assistant. Given a plain-text digest " +
		"of a developer's GitHub project items, commits, and calendar " +
		"events (meetings attended), write " +
		"a concise first-person standup update. Begin the message with exactly the " +
		"lines \"Hi Team,\" and \"My " + period + " was about:\" separated by a blank " +
		"line (two newlines after \"Hi Team,\") and nothing before it. Group " +
		"related work, lead with what shipped, and keep it to a few short bullet " +
		"points. Report only work that was actually done: omit items that were " +
		"merely created or opened for future work (newly filed issues, tickets, " +
		"or PRs with no substantive progress). Omit noise and links. End the " +
		"message with exactly the line \"Have a nice day!\" and nothing after it."
}

// MonthlySystem is the system instruction for turning a month's digest into a
// written markdown report. period names the month covered, e.g. "June 2026".
// Unlike StandupSystem it asks for markdown, since the result is saved to a
// file rather than posted as a chat message.
func MonthlySystem(period string) string {
	return "You are an engineering reporting assistant. Given a plain-text " +
		"digest of a developer's GitHub project items, pull requests, and " +
		"commits for " + period +
		", write a first-person monthly report in GitHub-flavored markdown. " +
		"Each project item line names its type — Issue or DraftIssue (a ticket) " +
		"or PullRequest — followed by its state, title, and URL; the pull " +
		"request section that follows holds further pull requests that were " +
		"never added to the board, and counts for just as much. " +
		"Begin with exactly the heading \"# " + period + "\" and nothing before " +
		"it. Organize the work into a handful of themed sections (\"## \" " +
		"headings) covering what shipped, what progressed, and anything left " +
		"open, each with short bullet points; add a brief summary paragraph " +
		"under the top heading. Group related commits and tickets into one " +
		"entry rather than listing them mechanically, and describe outcomes " +
		"rather than restating commit messages. Every bullet must cite the " +
		"tickets and pull requests behind it as markdown links on their titles, " +
		"e.g. \"[Fix login bug](url)\"; name a draft ticket without a link, " +
		"since drafts have no URL. Report only work that was " +
		"actually done: omit items that were merely created or opened for " +
		"future work. Omit noise. Write nothing after the final section."
}

// Client wraps the Anthropic SDK client.
type Client struct {
	api anthropic.Client
}

// New builds a Client. The Anthropic SDK reads ANTHROPIC_API_KEY from the
// environment automatically, so no key argument is required.
func New() *Client {
	return &Client{api: anthropic.NewClient()}
}

// Ask sends a single prompt with a system instruction and returns the reply.
func (c *Client) Ask(ctx context.Context, system, prompt string) (string, error) {
	message, err := c.api.Messages.New(ctx, anthropic.MessageNewParams{
		MaxTokens: 2048,
		System: []anthropic.TextBlockParam{
			{Text: system},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
		Model: anthropic.ModelClaudeOpus4_8,
	})
	if err != nil {
		return "", err
	}
	// Guard against a response with no text: concatenate any text blocks rather
	// than indexing Content[0], which would panic on an empty content array.
	var reply strings.Builder
	for _, block := range message.Content {
		reply.WriteString(block.Text)
	}
	return reply.String(), nil
}
