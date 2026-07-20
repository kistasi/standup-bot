// Package claude wraps the Anthropic Messages API for the standup bot.
package claude

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"
)

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
		MaxTokens: 1024,
		System: []anthropic.TextBlockParam{
			{Text: system},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
		Model: anthropic.ModelClaudeOpus4_6,
	})
	if err != nil {
		return "", err
	}
	return message.Content[0].Text, nil
}
