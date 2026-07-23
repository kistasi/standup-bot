// Package slack posts messages to a Slack incoming webhook.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// boldRe matches standard-Markdown **bold** spans. Slack's mrkdwn uses single
// asterisks for bold, so **bold** would otherwise render with the asterisks
// showing literally.
var boldRe = regexp.MustCompile(`\*\*(.+?)\*\*`)

// toMrkdwn converts the subset of standard Markdown that Claude emits into
// Slack's mrkdwn dialect. Only bold differs for our content: `code` spans use
// single backticks in both, and links are omitted by the standup prompt.
func toMrkdwn(text string) string {
	return boldRe.ReplaceAllString(text, "*$1*")
}

// Client posts messages to a single Slack incoming webhook.
type Client struct {
	webhookURL string
	http       *http.Client
}

// New builds a Client for the given incoming-webhook URL. Mirroring gh.New(token),
// the URL is itself the credential.
func New(webhookURL string) *Client {
	return &Client{
		webhookURL: webhookURL,
		http:       &http.Client{Timeout: 10 * time.Second},
	}
}

// Post sends text to the webhook as a simple message. A Slack incoming webhook
// responds with 200/"ok" on success or a non-200 status with an error string
// (e.g. "invalid_payload") otherwise; both are treated as errors here.
func (c *Client) Post(ctx context.Context, text string) error {
	payload, err := json.Marshal(map[string]string{"text": toMrkdwn(text)})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.webhookURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "ok" {
		return fmt.Errorf("slack webhook: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}
