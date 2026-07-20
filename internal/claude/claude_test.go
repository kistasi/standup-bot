package claude

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// stubClient points the Anthropic SDK at a local test server so Ask can be
// exercised without hitting the real Messages API.
func stubClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	api := anthropic.NewClient(
		option.WithBaseURL(srv.URL),
		option.WithAPIKey("test-key"),
	)
	return &Client{api: api}
}

func TestAsk_ReturnsReplyText(t *testing.T) {
	var gotReq struct {
		System   []map[string]any `json:"system"`
		Messages []struct {
			Role    string           `json:"role"`
			Content []map[string]any `json:"content"`
		} `json:"messages"`
		MaxTokens int    `json:"max_tokens"`
		Model     string `json:"model"`
	}

	client := stubClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotReq)

		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"id":"msg_1","type":"message","role":"assistant","model":"claude",
			"content":[{"type":"text","text":"Hi Team,\n\nMy yesterday was about:\n- stuff\n\nHave a nice day!"}],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":10,"output_tokens":20}
		}`)
	})

	got, err := client.Ask(context.Background(), StandupSystem, "the digest")
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if !strings.HasPrefix(got, "Hi Team,") || !strings.HasSuffix(got, "Have a nice day!") {
		t.Errorf("Ask() = %q, want the stubbed reply text", got)
	}

	// The request carried the system instruction, the user prompt, and the
	// configured token budget.
	if len(gotReq.System) == 0 || gotReq.System[0]["text"] != StandupSystem {
		t.Errorf("request system = %+v, want it to carry StandupSystem", gotReq.System)
	}
	if gotReq.MaxTokens != 2048 {
		t.Errorf("request max_tokens = %d, want 2048", gotReq.MaxTokens)
	}
	if len(gotReq.Messages) != 1 || gotReq.Messages[0].Role != "user" {
		t.Fatalf("request messages = %+v, want one user message", gotReq.Messages)
	}
	if gotReq.Messages[0].Content[0]["text"] != "the digest" {
		t.Errorf("request prompt = %v, want %q", gotReq.Messages[0].Content[0]["text"], "the digest")
	}
}

func TestAsk_APIError(t *testing.T) {
	client := stubClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`)
	})

	if _, err := client.Ask(context.Background(), StandupSystem, "x"); err == nil {
		t.Fatal("Ask() error = nil, want error on 400 response")
	}
}

func TestAsk_EmptyContentReturnsEmptyString(t *testing.T) {
	// A response with no content blocks must not panic (it used to index
	// Content[0]); Ask returns an empty reply and no error.
	client := stubClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"id":"msg_1","type":"message","role":"assistant","model":"claude",
			"content":[],
			"stop_reason":"end_turn",
			"usage":{"input_tokens":10,"output_tokens":0}
		}`)
	})

	got, err := client.Ask(context.Background(), StandupSystem, "the digest")
	if err != nil {
		t.Fatalf("Ask() error = %v, want nil", err)
	}
	if got != "" {
		t.Errorf("Ask() = %q, want empty string", got)
	}
}

func TestStandupSystem_Anchors(t *testing.T) {
	// The prompt's whole contract is the fixed opening and closing lines; guard
	// against accidental edits that would drop them.
	for _, anchor := range []string{"Hi Team,", "My yesterday was about:", "Have a nice day!"} {
		if !strings.Contains(StandupSystem, anchor) {
			t.Errorf("StandupSystem missing required anchor %q", anchor)
		}
	}
}
