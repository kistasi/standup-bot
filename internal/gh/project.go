package gh

import (
	"context"
	"time"

	"github.com/shurcooL/githubv4"
)

// Item is a single project item flattened from its underlying content
// (Issue, PullRequest, or DraftIssue).
type Item struct {
	Type      string // "Issue", "PullRequest", or "DraftIssue"
	Title     string
	Body      string // the issue/PR/draft description (may be empty)
	URL       string // empty for draft issues
	State     string // "DRAFT" for draft issues
	UpdatedAt time.Time
}

// ProjectDigest is the result of scanning an organization project.
type ProjectDigest struct {
	Login string // the viewer (PAT owner) the items were filtered to
	Title string // the project's title
	Items []Item
}

// assignees is the shared assignee connection selection.
type assignees struct {
	Nodes []struct{ Login githubv4.String }
}

// issueContent is the selection shared by Issue and PullRequest fragments;
// both expose the same fields we care about.
type issueContent struct {
	Title     githubv4.String
	Body      githubv4.String
	URL       githubv4.String `graphql:"url"`
	State     githubv4.String
	UpdatedAt githubv4.DateTime
	Assignees assignees `graphql:"assignees(first:10)"`
}

// draftContent is the DraftIssue fragment; drafts have no URL or state.
type draftContent struct {
	Title     githubv4.String
	Body      githubv4.String
	UpdatedAt githubv4.DateTime
	Assignees assignees `graphql:"assignees(first:10)"`
}

// itemNode mirrors one node in the projectV2 items connection.
type itemNode struct {
	Content struct {
		Typename    githubv4.String `graphql:"__typename"`
		Issue       issueContent    `graphql:"... on Issue"`
		PullRequest issueContent    `graphql:"... on PullRequest"`
		DraftIssue  draftContent    `graphql:"... on DraftIssue"`
	}
}

// MyProjectItems returns the items in org project #number that are assigned to
// the PAT owner (the GraphQL "viewer") and were updated in [since, until).
// The standup passes the current time as until; the monthly report passes the
// end of the month it covers.
func (c *Client) MyProjectItems(ctx context.Context, org string, number int, since, until time.Time) (*ProjectDigest, error) {
	var q struct {
		Viewer struct {
			Login githubv4.String
		}
		Organization struct {
			ProjectV2 struct {
				Title githubv4.String
				Items struct {
					PageInfo struct {
						HasNextPage githubv4.Boolean
						EndCursor   githubv4.String
					}
					Nodes []itemNode
				} `graphql:"items(first:100, after:$cursor)"`
			} `graphql:"projectV2(number:$number)"`
		} `graphql:"organization(login:$org)"`
	}

	variables := map[string]any{
		"org":    githubv4.String(org),
		"number": githubv4.Int(number),
		"cursor": (*githubv4.String)(nil),
	}

	digest := &ProjectDigest{}
	for {
		if err := c.query(ctx, &q, variables); err != nil {
			return nil, err
		}

		digest.Login = string(q.Viewer.Login)
		digest.Title = string(q.Organization.ProjectV2.Title)

		for _, node := range q.Organization.ProjectV2.Items.Nodes {
			item, ok := node.toItem(digest.Login, since, until)
			if ok {
				digest.Items = append(digest.Items, item)
			}
		}

		page := q.Organization.ProjectV2.Items.PageInfo
		if !page.HasNextPage {
			break
		}
		variables["cursor"] = githubv4.NewString(page.EndCursor)
	}

	return digest, nil
}

// toItem flattens a node into an Item, returning ok=false when the item should
// be skipped (unreadable content, not assigned to login, or updated outside
// [since, until)).
func (n itemNode) toItem(login string, since, until time.Time) (Item, bool) {
	typename := string(n.Content.Typename)

	var (
		item      Item
		assignees assignees
	)
	switch typename {
	case "Issue", "PullRequest":
		c := n.Content.Issue
		if typename == "PullRequest" {
			c = n.Content.PullRequest
		}
		item = Item{
			Type:      typename,
			Title:     string(c.Title),
			Body:      string(c.Body),
			URL:       string(c.URL),
			State:     string(c.State),
			UpdatedAt: c.UpdatedAt.Time,
		}
		assignees = c.Assignees
	case "DraftIssue":
		c := n.Content.DraftIssue
		item = Item{
			Type:      typename,
			Title:     string(c.Title),
			Body:      string(c.Body),
			State:     "DRAFT",
			UpdatedAt: c.UpdatedAt.Time,
		}
		assignees = c.Assignees
	default:
		// Empty typename: e.g. a redacted item the PAT can't see.
		return Item{}, false
	}

	if item.UpdatedAt.Before(since) || !item.UpdatedAt.Before(until) {
		return Item{}, false
	}
	for _, a := range assignees.Nodes {
		if string(a.Login) == login {
			return item, true
		}
	}
	return Item{}, false
}
