package gh

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shurcooL/githubv4"
)

// stubServer returns a Client whose GraphQL endpoint is an httptest server that
// replies with the given response bodies in order, one per query. Each body is
// the JSON string placed under the top-level {"data": ...} envelope by the
// caller. bodies lets a test drive multi-page paging.
func stubServer(t *testing.T, bodies ...string) (*Client, *[]map[string]any) {
	t.Helper()
	var requests []map[string]any
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(raw, &req)
		requests = append(requests, req)

		if i >= len(bodies) {
			t.Errorf("unexpected query #%d (only %d responses stubbed)", i+1, len(bodies))
			http.Error(w, "no more responses", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, bodies[i])
		i++
	}))
	t.Cleanup(srv.Close)

	return &Client{gql: githubv4.NewEnterpriseClient(srv.URL, srv.Client())}, &requests
}

func TestOrgCommits_FiltersNoiseAndStopsOnOldRepo(t *testing.T) {
	since := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)
	inRange := "2026-07-19T12:00:00Z"

	// One page. First repo (pushed in range) has: a normal commit (kept), a
	// release commit (kept and marked delivered), a merge commit with 2 parents
	// (dropped). The second repo was pushed before `since`, so the scan stops and
	// never reads its commits.
	body := `{"data":{"organization":{"repositories":{
		"pageInfo":{"hasNextPage":true,"endCursor":"NEXT"},
		"nodes":[
			{
				"nameWithOwner":"acme/app",
				"pushedAt":"2026-07-19T13:00:00Z",
				"defaultBranchRef":{"target":{
					"history":{"nodes":[
						{"oid":"c1","messageHeadline":"Add retry logic","messageBody":"body","url":"https://x/c1","committedDate":"` + inRange + `","parents":{"totalCount":1}},
						{"oid":"c2","messageHeadline":"Release 2026-07-19 (#9)","messageBody":"","url":"https://x/c2","committedDate":"` + inRange + `","parents":{"totalCount":1}},
						{"oid":"c3","messageHeadline":"Merge branch main","messageBody":"","url":"https://x/c3","committedDate":"` + inRange + `","parents":{"totalCount":2}}
					]}
				}}
			},
			{
				"nameWithOwner":"acme/stale",
				"pushedAt":"2026-07-18T09:00:00Z",
				"defaultBranchRef":{"target":{
					"history":{"nodes":[
						{"oid":"z9","messageHeadline":"Should be skipped","messageBody":"","url":"https://x/z9","committedDate":"2026-07-18T09:00:00Z","parents":{"totalCount":1}}
					]}
				}}
			}
		]
	}}}}`

	client, requests := stubServer(t, body)

	digest, err := client.OrgCommits(context.Background(), "acme", since, until, []string{"dev@acme.com"})
	if err != nil {
		t.Fatalf("OrgCommits error = %v", err)
	}

	if digest.Org != "acme" {
		t.Errorf("Org = %q, want acme", digest.Org)
	}
	if len(digest.Commits) != 2 {
		t.Fatalf("got %d commits, want 2 (merge + stale-repo dropped, release kept): %+v", len(digest.Commits), digest.Commits)
	}
	c := digest.Commits[0]
	if c.Repo != "acme/app" || c.Message != "Add retry logic" || c.Body != "body" ||
		c.URL != "https://x/c1" || c.OID != "c1" || c.Delivered {
		t.Errorf("commit = %+v, unexpected fields", c)
	}
	// The release commit is the delivery, so it is reported rather than skipped.
	if r := digest.Commits[1]; r.OID != "c2" || !r.Delivered {
		t.Errorf("commit[1] = %+v, want the release commit marked delivered", r)
	}

	// The scan stopped at the stale repo, so despite hasNextPage=true it made
	// exactly one query rather than following the cursor.
	if len(*requests) != 1 {
		t.Errorf("made %d queries, want 1 (should stop at stale repo)", len(*requests))
	}
}

func TestOrgCommits_PassesEmailsAndSince(t *testing.T) {
	body := `{"data":{"organization":{"repositories":{
		"pageInfo":{"hasNextPage":false,"endCursor":""},
		"nodes":[]
	}}}}`
	client, requests := stubServer(t, body)
	since := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)

	if _, err := client.OrgCommits(context.Background(), "acme", since, until, []string{"a@x.com", "b@x.com"}); err != nil {
		t.Fatalf("OrgCommits error = %v", err)
	}

	vars, _ := (*requests)[0]["variables"].(map[string]any)
	emails, ok := vars["emails"].([]any)
	if !ok || len(emails) != 2 || emails[0] != "a@x.com" || emails[1] != "b@x.com" {
		t.Errorf("emails variable = %v, want [a@x.com b@x.com]", vars["emails"])
	}
	if vars["org"] != "acme" {
		t.Errorf("org variable = %v, want acme", vars["org"])
	}
	// Both ends of the window reach the history query; the monthly report
	// depends on `until` to stop at the end of its month.
	if vars["since"] != "2026-07-19T00:00:00Z" {
		t.Errorf("since variable = %v, want 2026-07-19T00:00:00Z", vars["since"])
	}
	if vars["until"] != "2026-07-20T00:00:00Z" {
		t.Errorf("until variable = %v, want 2026-07-20T00:00:00Z", vars["until"])
	}
}

// A repository can hold more in-window commits than fit on one history page —
// a month of a busy repo, say. The rest must be followed up per repository
// rather than silently truncated at the first page.
func TestOrgCommits_PagesRepositoryHistory(t *testing.T) {
	since := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	at := "2026-07-19T12:00:00Z"

	// The org page reports the repo's history as having a next page.
	orgPage := `{"data":{"organization":{"repositories":{
		"pageInfo":{"hasNextPage":false,"endCursor":""},
		"nodes":[{
			"nameWithOwner":"acme/app",
			"pushedAt":"2026-07-31T13:00:00Z",
			"defaultBranchRef":{"target":{
				"history":{
					"pageInfo":{"hasNextPage":true,"endCursor":"H1"},
					"nodes":[{"oid":"c1","messageHeadline":"First","messageBody":"","url":"https://x/c1","committedDate":"` + at + `","parents":{"totalCount":1}}]
				}
			}}
		}]
	}}}}`
	// Follow-up page 1 for that repo: an ordinary commit and a release commit —
	// both kept, the release one flagged as delivered.
	repoPage1 := `{"data":{"repository":{"defaultBranchRef":{"target":{
		"history":{
			"pageInfo":{"hasNextPage":true,"endCursor":"H2"},
			"nodes":[
				{"oid":"c2","messageHeadline":"Second","messageBody":"","url":"https://x/c2","committedDate":"` + at + `","parents":{"totalCount":1}},
				{"oid":"c3","messageHeadline":"Release 2026-07-19","messageBody":"","url":"https://x/c3","committedDate":"` + at + `","parents":{"totalCount":1}}
			]
		}
	}}}}}`
	repoPage2 := `{"data":{"repository":{"defaultBranchRef":{"target":{
		"history":{
			"pageInfo":{"hasNextPage":false,"endCursor":""},
			"nodes":[{"oid":"c4","messageHeadline":"Third","messageBody":"","url":"https://x/c4","committedDate":"` + at + `","parents":{"totalCount":1}}]
		}
	}}}}}`

	client, requests := stubServer(t, orgPage, repoPage1, repoPage2)

	digest, err := client.OrgCommits(context.Background(), "acme", since, until, []string{"dev@acme.com"})
	if err != nil {
		t.Fatalf("OrgCommits error = %v", err)
	}

	var got []string
	for _, c := range digest.Commits {
		if c.Repo != "acme/app" {
			t.Errorf("commit %q has repo %q, want acme/app", c.Message, c.Repo)
		}
		got = append(got, c.Message)
	}
	want := []string{"First", "Second", "Release 2026-07-19", "Third"}
	if len(got) != len(want) {
		t.Fatalf("got commits %v, want %v (no page truncated)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got commits %v, want %v", got, want)
		}
	}

	if len(*requests) != 3 {
		t.Fatalf("made %d queries, want 3 (org page + 2 history pages)", len(*requests))
	}
	// The follow-up queries address the repository by owner and name, split out
	// of nameWithOwner, and carry the previous page's cursor.
	vars, _ := (*requests)[1]["variables"].(map[string]any)
	if vars["owner"] != "acme" || vars["name"] != "app" {
		t.Errorf("follow-up owner/name = %v/%v, want acme/app", vars["owner"], vars["name"])
	}
	if vars["cursor"] != "H1" {
		t.Errorf("first follow-up cursor = %v, want H1", vars["cursor"])
	}
	vars, _ = (*requests)[2]["variables"].(map[string]any)
	if vars["cursor"] != "H2" {
		t.Errorf("second follow-up cursor = %v, want H2", vars["cursor"])
	}
}

// A history page that reports no next page must not trigger a follow-up query.
func TestOrgCommits_NoFollowUpWhenHistoryFitsOnePage(t *testing.T) {
	body := `{"data":{"organization":{"repositories":{
		"pageInfo":{"hasNextPage":false,"endCursor":""},
		"nodes":[{
			"nameWithOwner":"acme/app",
			"pushedAt":"2026-07-19T13:00:00Z",
			"defaultBranchRef":{"target":{
				"history":{
					"pageInfo":{"hasNextPage":false,"endCursor":""},
					"nodes":[{"oid":"c1","messageHeadline":"Only","messageBody":"","url":"https://x/c1","committedDate":"2026-07-19T12:00:00Z","parents":{"totalCount":1}}]
				}
			}}
		}]
	}}}}`

	client, requests := stubServer(t, body)
	since := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)

	digest, err := client.OrgCommits(context.Background(), "acme", since, since.Add(24*time.Hour), []string{"dev@acme.com"})
	if err != nil {
		t.Fatalf("OrgCommits error = %v", err)
	}
	if len(digest.Commits) != 1 {
		t.Errorf("got %d commits, want 1", len(digest.Commits))
	}
	if len(*requests) != 1 {
		t.Errorf("made %d queries, want 1 (history fit on one page)", len(*requests))
	}
}

func TestMyPullRequests_FiltersAndStopsOnOldPull(t *testing.T) {
	since := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)

	// One page holding, in update order: a pull request in the org and window
	// (kept), one in another org (dropped), a release-branch pull request (kept
	// and marked delivered), one updated at `until` (dropped, exclusive end),
	// then one updated before `since` — which stops the walk, so the cursor is
	// never followed.
	body := `{"data":{"viewer":{
		"login":"octocat",
		"pullRequests":{
			"pageInfo":{"hasNextPage":true,"endCursor":"NEXT"},
			"nodes":[
				{"number":9,"title":"Bump linter","body":"desc","url":"https://x/9","state":"OPEN","headRefName":"chore/linter","isDraft":true,"updatedAt":"2026-07-19T12:00:00Z","repository":{"nameWithOwner":"acme/tools","owner":{"login":"acme"}}},
				{"number":8,"title":"Elsewhere","body":"","url":"https://x/8","state":"OPEN","isDraft":false,"updatedAt":"2026-07-19T11:00:00Z","repository":{"nameWithOwner":"other/app","owner":{"login":"other"}}},
				{"number":7,"title":"Cut the sprint","body":"","url":"https://x/7","state":"MERGED","headRefName":"release/2026-07-19","isDraft":false,"updatedAt":"2026-07-19T10:00:00Z","repository":{"nameWithOwner":"acme/app","owner":{"login":"acme"}}},
				{"number":6,"title":"Next month","body":"","url":"https://x/6","state":"OPEN","isDraft":false,"updatedAt":"2026-07-20T00:00:00Z","repository":{"nameWithOwner":"acme/app","owner":{"login":"acme"}}},
				{"number":5,"title":"Too old","body":"","url":"https://x/5","state":"MERGED","isDraft":false,"updatedAt":"2026-07-18T23:00:00Z","repository":{"nameWithOwner":"acme/app","owner":{"login":"acme"}}}
			]
		}
	}}}`

	client, requests := stubServer(t, body)

	digest, err := client.MyPullRequests(context.Background(), "acme", since, until)
	if err != nil {
		t.Fatalf("MyPullRequests error = %v", err)
	}

	if digest.Login != "octocat" || digest.Org != "acme" {
		t.Errorf("digest = %+v, want login octocat in org acme", digest)
	}
	if len(digest.PullRequests) != 2 {
		t.Fatalf("got %d pull requests, want 2: %+v", len(digest.PullRequests), digest.PullRequests)
	}
	pr := digest.PullRequests[0]
	if pr.Number != 9 || pr.Repo != "acme/tools" || pr.Title != "Bump linter" ||
		pr.Body != "desc" || pr.URL != "https://x/9" || pr.State != "OPEN" ||
		pr.Branch != "chore/linter" || !pr.Draft || pr.Delivered {
		t.Errorf("pull request = %+v, unexpected fields", pr)
	}
	// The release branch is a delivery, so it is reported and flagged.
	if rel := digest.PullRequests[1]; rel.Number != 7 || rel.Branch != "release/2026-07-19" || !rel.Delivered {
		t.Errorf("pull request[1] = %+v, want the release branch marked delivered", rel)
	}

	// The walk stopped at the out-of-window pull request, so despite
	// hasNextPage=true it made exactly one query.
	if len(*requests) != 1 {
		t.Errorf("made %d queries, want 1 (should stop at the old pull request)", len(*requests))
	}
}

func TestMyPullRequests_Pages(t *testing.T) {
	page1 := `{"data":{"viewer":{
		"login":"octocat",
		"pullRequests":{
			"pageInfo":{"hasNextPage":true,"endCursor":"CUR1"},
			"nodes":[
				{"number":9,"title":"First","body":"","url":"https://x/9","state":"OPEN","isDraft":false,"updatedAt":"2026-07-19T12:00:00Z","repository":{"nameWithOwner":"acme/app","owner":{"login":"acme"}}}
			]
		}
	}}}`
	page2 := `{"data":{"viewer":{
		"login":"octocat",
		"pullRequests":{
			"pageInfo":{"hasNextPage":false,"endCursor":""},
			"nodes":[
				{"number":8,"title":"Second","body":"","url":"https://x/8","state":"MERGED","isDraft":false,"updatedAt":"2026-07-19T09:00:00Z","repository":{"nameWithOwner":"acme/app","owner":{"login":"acme"}}}
			]
		}
	}}}`

	client, requests := stubServer(t, page1, page2)
	since := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)

	digest, err := client.MyPullRequests(context.Background(), "acme", since, since.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("MyPullRequests error = %v", err)
	}
	if len(digest.PullRequests) != 2 {
		t.Fatalf("got %d pull requests, want 2 across both pages", len(digest.PullRequests))
	}
	if vars, _ := (*requests)[1]["variables"].(map[string]any); vars["cursor"] != "CUR1" {
		t.Errorf("second query cursor = %v, want CUR1", vars["cursor"])
	}
}
