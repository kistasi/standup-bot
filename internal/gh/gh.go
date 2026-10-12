// Package gh wraps the GitHub GraphQL v4 API for the standup bot.
package gh

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/shurcooL/githubv4"
)

// Client is a thin wrapper around a typed GitHub GraphQL client.
type Client struct {
	gql *githubv4.Client
}

// New builds a Client authenticated with the given personal access token.
func New(token string) *Client {
	httpClient := &http.Client{
		Transport: authTransport{token: token, base: retryTransport{
			base:  http.DefaultTransport,
			waits: []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second},
		}},
	}
	return &Client{gql: githubv4.NewClient(httpClient)}
}

// authTransport injects a bearer token into every outgoing request.
type authTransport struct {
	token string
	base  http.RoundTripper
}

func (t authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// Clone before mutating: RoundTrippers must not modify the caller's request.
	clone := r.Clone(r.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(clone)
}

// retryTransport re-sends a request when GitHub answers with a gateway error
// (502, 503 or 504). Those are GitHub's own transient failures — the "Unicorn"
// page — and every request this package makes is a read-only GraphQL query, so
// sending it again is safe. waits holds the pause before each retry; once it is
// exhausted the last response is returned as-is.
type retryTransport struct {
	base  http.RoundTripper
	waits []time.Duration
}

func (t retryTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		req := r
		if attempt > 0 && r.Body != nil {
			// The previous attempt consumed the body; rewind it from GetBody,
			// which net/http sets for the in-memory bodies githubv4 sends.
			if r.GetBody == nil {
				return nil, errNoRewind
			}
			body, err := r.GetBody()
			if err != nil {
				return nil, err
			}
			req = r.Clone(r.Context())
			req.Body = body
		}

		resp, err := t.base.RoundTrip(req)
		if err != nil || !transient(resp.StatusCode) || attempt == len(t.waits) {
			return resp, err
		}
		resp.Body.Close()

		select {
		case <-time.After(t.waits[attempt]):
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
}

var errNoRewind = errors.New("gh: cannot retry a request whose body cannot be rewound")

// transient reports whether status is a gateway error worth retrying.
func transient(status int) bool {
	return status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable ||
		status == http.StatusGatewayTimeout
}

// query runs a typed GraphQL query. It exists so callers share one context
// entry point and so tests can stub the underlying client if needed.
func (c *Client) query(ctx context.Context, q any, variables map[string]any) error {
	return c.gql.Query(ctx, q, variables)
}
