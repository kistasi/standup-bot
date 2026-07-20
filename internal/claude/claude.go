// Package claude wraps the Anthropic Messages API for the standup bot.
package claude

import (
	"context"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// StandupSystem is the system instruction for turning a raw digest into a
// concise standup update.
const StandupSystem = "You are a standup assistant. Given a plain-text digest " +
	"of a developer's GitHub project items and commits from the last day, write " +
	"a concise first-person standup update. Begin the message with exactly the " +
	"lines \"Hi Team,\" and \"My yesterday was about:\" separated by a blank " +
	"line (two newlines after \"Hi Team,\") and nothing before it. Group " +
	"related work, lead with what shipped, and keep it to a few short bullet " +
	"points. Omit noise and links. End the message with exactly the line " +
	"\"Have a nice day!\" and nothing after it."

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
