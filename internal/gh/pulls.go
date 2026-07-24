package gh

import (
	"context"
	"strings"
	"time"

	"github.com/shurcooL/githubv4"
)

// PullRequest is a pull request authored by the PAT owner.
type PullRequest struct {
	Repo      string // "owner/name"
	Number    int
	Title     string
	Body      string // the PR description (may be empty)
	URL       string
	State     string // "OPEN", "CLOSED", or "MERGED"
	Draft     bool   // a draft (work-in-progress) pull request
	UpdatedAt time.Time
}

// PullRequestDigest is the result of scanning the viewer's pull requests.
type PullRequestDigest struct {
	Login        string // the viewer (PAT owner) the pull requests belong to
	Org          string // the organization they were filtered to
	PullRequests []PullRequest
}

// pullNode mirrors one node in the viewer's pullRequests connection.
type pullNode struct {
	Number     githubv4.Int
	Title      githubv4.String
	Body       githubv4.String
	URL        githubv4.String `graphql:"url"`
	State      githubv4.String
	IsDraft    githubv4.Boolean
	UpdatedAt  githubv4.DateTime
	Repository struct {
		NameWithOwner githubv4.String
		Owner         struct {
			Login githubv4.String
		}
	}
}

// MyPullRequests returns the pull requests authored by the PAT owner (the
// GraphQL "viewer") in org that were updated in [since, until). It complements
// MyProjectItems, which only sees pull requests added to the project board.
//
// The connection is walked newest-updated first, so the scan stops as soon as
// it reaches a pull request updated before `since`. Pull requests updated at or
// after `until` are skipped rather than stopping the walk, since older ones
// still inside the window follow them.
func (c *Client) MyPullRequests(ctx context.Context, org string, since, until time.Time) (*PullRequestDigest, error) {
	var q struct {
		Viewer struct {
			Login        githubv4.String
			PullRequests struct {
				PageInfo pageInfo
				Nodes    []pullNode
			} `graphql:"pullRequests(first:50, after:$cursor, orderBy:{field:UPDATED_AT, direction:DESC})"`
		}
	}

	variables := map[string]any{
		"cursor": (*githubv4.String)(nil),
	}

	digest := &PullRequestDigest{Org: org}
	for {
		if err := c.query(ctx, &q, variables); err != nil {
			return nil, err
		}

		digest.Login = string(q.Viewer.Login)

		stop := false
		for _, node := range q.Viewer.PullRequests.Nodes {
			// Ordered by update time desc: once a pull request was last updated
			// before `since`, no later one can be in range either.
			if node.UpdatedAt.Time.Before(since) {
				stop = true
				break
			}
			pr, ok := node.toPullRequest(org, until)
			if ok {
				digest.PullRequests = append(digest.PullRequests, pr)
			}
		}

		page := q.Viewer.PullRequests.PageInfo
		if stop || !bool(page.HasNextPage) {
			break
		}
		variables["cursor"] = githubv4.NewString(page.EndCursor)
	}

	return digest, nil
}

// toPullRequest flattens a node, returning ok=false when it belongs to another
// organization, was updated at or after until, or is release automation.
func (n pullNode) toPullRequest(org string, until time.Time) (PullRequest, bool) {
	if !strings.EqualFold(string(n.Repository.Owner.Login), org) {
		return PullRequest{}, false
	}
	if !n.UpdatedAt.Time.Before(until) {
		return PullRequest{}, false
	}
	if isReleaseCommit(string(n.Title)) {
		return PullRequest{}, false
	}
	return PullRequest{
		Repo:      string(n.Repository.NameWithOwner),
		Number:    int(n.Number),
		Title:     string(n.Title),
		Body:      string(n.Body),
		URL:       string(n.URL),
		State:     string(n.State),
		Draft:     bool(n.IsDraft),
		UpdatedAt: n.UpdatedAt.Time,
	}, true
}
