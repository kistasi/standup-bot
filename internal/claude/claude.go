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
		"of a developer's GitHub pull requests, commits, calendar " +
		"events (meetings attended), and tracked time entries, write " +
		"a concise first-person standup update. Begin the message with exactly the " +
		"lines \"Hi Team,\" and \"My " + period + " was about:\" separated by a blank " +
		"line (two newlines after \"Hi Team,\") and nothing before it. Group " +
		"related work, lead with what shipped, and keep it to a few short bullet " +
		"points. " + deliveredRule + timeRule +
		"Cite the pull requests behind each bullet as markdown links on their " +
		"titles, e.g. \"[Fix login bug](url)\"; link pull requests only, not " +
		"commits. Report only work that was actually done: omit items that were " +
		"merely created or opened for future work (pull requests with no " +
		"substantive progress). Omit noise. End the " +
		"message with exactly the line \"Have a nice day!\" and nothing after it."
}

// deliveredRule explains render's DELIVERED marker. Both prompts share it: a
// release branch is how work reaches production, so it must be reported as
// delivered rather than as one more merged pull request.
const deliveredRule = "A digest line ending in \"DELIVERED\" is a release — a " +
	"pull request from a release branch, or a release commit. Report those " +
	"explicitly as delivered (shipped to production), and say what the release " +
	"contained where the digest shows it. "

// MonthlySystem is the system instruction for turning a month's digest into a
// written markdown report. period names the month covered, e.g. "June 2026".
// Unlike StandupSystem it asks for markdown, since the result is saved to a
// file rather than posted as a chat message.
func MonthlySystem(period string) string {
	return "You are an engineering reporting assistant. Given a plain-text " +
		"digest of a developer's GitHub pull requests, commits, and tracked " +
		"time entries for " + period +
		", write a first-person monthly report in GitHub-flavored markdown. " +
		"Each pull request line carries its repository, state, head branch, " +
		"title, and URL; the commit section that follows records the code " +
		"behind them. " + deliveredRule + timeRule +
		"Begin with exactly the heading \"# " + period + "\" and nothing before " +
		"it. Organize the work into a handful of themed sections (\"## \" " +
		"headings) covering what shipped, what progressed, and anything left " +
		"open, each with short bullet points; add a brief summary paragraph " +
		"under the top heading. Group related commits and pull requests into one " +
		"entry rather than listing them mechanically, and describe outcomes " +
		"rather than restating commit messages. Every bullet must cite the " +
		"pull requests behind it as markdown links on their titles, " +
		"e.g. \"[Fix login bug](url)\"; link pull requests only, not commits. " +
		"Report only work that was " +
		"actually done: omit items that were merely created or opened for " +
		"future work. Omit noise. Write nothing after the final section."
}

// timeRule explains the tracked-time section, which is present only when Toggl
// is configured. It has to be worded for its absence too: the prompt is a
// constant, while the section is not.
const timeRule = "The digest may end with a tracked-time section — one line per " +
	"time entry with its duration, project, description and tags, then a total. " +
	"When it is there, use it to weight what mattered and mention the total " +
	"where it is worth saying; a line marked \"(still running)\" is a timer that " +
	"had not been stopped, so treat its duration as approximate. Never invent " +
	"hours when the section is absent. "

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
