package gh

import (
	"context"
	"strings"
	"time"

	"github.com/shurcooL/githubv4"
)

// isReleaseCommit reports whether a commit headline is an automated release
// commit (e.g. "Release 2026-07-20 (#506)"), which is noise in a standup.
func isReleaseCommit(headline string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(headline)), "release ")
}

// Commit is a single commit authored by one of the requested emails.
type Commit struct {
	Repo          string // "owner/name"
	OID           string
	Message       string // the commit's headline (first line)
	Body          string // the commit message body (everything after the headline)
	URL           string
	CommittedDate time.Time
}

// CommitDigest is the result of scanning an org's repositories for commits.
type CommitDigest struct {
	Org     string
	Emails  []string // the author emails the scan was filtered to
	Commits []Commit
}

// commitNode mirrors one commit in a repository's default-branch history.
type commitNode struct {
	OID             githubv4.GitObjectID
	MessageHeadline githubv4.String
	MessageBody     githubv4.String
	URL             githubv4.String `graphql:"url"`
	CommittedDate   githubv4.GitTimestamp
	Parents         struct {
		TotalCount githubv4.Int
	} `graphql:"parents(first:0)"`
}

// OrgCommits returns commits authored by any of the given emails, made on the
// default branch of any repository in org since `since`. Filtering by
// author email (rather than by GitHub account) is deliberate: it matches
// commits even when the author's git email is not linked to their account.
//
// Repositories are walked newest-pushed first, so the scan stops as soon as it
// reaches a repository whose last push predates `since`.
func (c *Client) OrgCommits(ctx context.Context, org string, since time.Time, emails []string) (*CommitDigest, error) {
	gqlEmails := make([]githubv4.String, len(emails))
	for i, e := range emails {
		gqlEmails[i] = githubv4.String(e)
	}

	var q struct {
		Organization struct {
			Repositories struct {
				PageInfo struct {
					HasNextPage githubv4.Boolean
					EndCursor   githubv4.String
				}
				Nodes []struct {
					NameWithOwner    githubv4.String
					PushedAt         githubv4.DateTime
					DefaultBranchRef struct {
						Target struct {
							Commit struct {
								History struct {
									Nodes []commitNode
								} `graphql:"history(since:$since, first:100, author:{emails:$emails})"`
							} `graphql:"... on Commit"`
						}
					}
				}
			} `graphql:"repositories(first:50, after:$cursor, orderBy:{field:PUSHED_AT, direction:DESC})"`
		} `graphql:"organization(login:$org)"`
	}

	variables := map[string]any{
		"org":    githubv4.String(org),
		"since":  githubv4.GitTimestamp{Time: since},
		"emails": gqlEmails,
		"cursor": (*githubv4.String)(nil),
	}

	digest := &CommitDigest{Org: org, Emails: emails}
	for {
		if err := c.query(ctx, &q, variables); err != nil {
			return nil, err
		}

		stop := false
		for _, repo := range q.Organization.Repositories.Nodes {
			// Ordered by push time desc: once a repo was last pushed before
			// `since`, no later repo can have commits in range either.
			if repo.PushedAt.Time.Before(since) {
				stop = true
				break
			}
			name := string(repo.NameWithOwner)
			for _, node := range repo.DefaultBranchRef.Target.Commit.History.Nodes {
				// Skip release automation and merge commits (2+ parents):
				// both are noise in a standup.
				if isReleaseCommit(string(node.MessageHeadline)) || node.Parents.TotalCount > 1 {
					continue
				}
				digest.Commits = append(digest.Commits, Commit{
					Repo:          name,
					OID:           string(node.OID),
					Message:       string(node.MessageHeadline),
					Body:          string(node.MessageBody),
					URL:           string(node.URL),
					CommittedDate: node.CommittedDate.Time,
				})
			}
		}

		page := q.Organization.Repositories.PageInfo
		if stop || !bool(page.HasNextPage) {
			break
		}
		variables["cursor"] = githubv4.NewString(page.EndCursor)
	}

	return digest, nil
}
