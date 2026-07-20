// Package gh wraps the GitHub GraphQL v4 API for the standup bot.
package gh

import (
	"context"
	"net/http"

	"github.com/shurcooL/githubv4"
)

// Client is a thin wrapper around a typed GitHub GraphQL client.
type Client struct {
	gql *githubv4.Client
}

// New builds a Client authenticated with the given personal access token.
func New(token string) *Client {
	httpClient := &http.Client{
		Transport: authTransport{token: token, base: http.DefaultTransport},
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

// query runs a typed GraphQL query. It exists so callers share one context
// entry point and so tests can stub the underlying client if needed.
func (c *Client) query(ctx context.Context, q any, variables map[string]any) error {
	return c.gql.Query(ctx, q, variables)
}
