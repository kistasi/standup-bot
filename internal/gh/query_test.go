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

func TestMyProjectItems_FiltersAndPages(t *testing.T) {
	// Page 1: an issue assigned to the viewer (kept), an issue assigned to
	// someone else (dropped). Page 2: a draft assigned to the viewer (kept).
	page1 := `{"data":{
		"viewer":{"login":"octocat"},
		"organization":{"projectV2":{
			"title":"Roadmap",
			"items":{
				"pageInfo":{"hasNextPage":true,"endCursor":"CUR1"},
				"nodes":[
					{"content":{"__typename":"Issue","title":"Mine","body":"b1","url":"https://x/1","state":"OPEN","updatedAt":"2026-07-19T10:00:00Z","assignees":{"nodes":[{"login":"octocat"}]}}},
					{"content":{"__typename":"Issue","title":"Theirs","body":"b2","url":"https://x/2","state":"OPEN","updatedAt":"2026-07-19T10:00:00Z","assignees":{"nodes":[{"login":"other"}]}}}
				]
			}
		}}
	}}`
	page2 := `{"data":{
		"viewer":{"login":"octocat"},
		"organization":{"projectV2":{
			"title":"Roadmap",
			"items":{
				"pageInfo":{"hasNextPage":false,"endCursor":""},
				"nodes":[
					{"content":{"__typename":"DraftIssue","title":"Draft mine","body":"d","updatedAt":"2026-07-19T11:00:00Z","assignees":{"nodes":[{"login":"octocat"}]}}}
				]
			}
		}}
	}}`

	client, requests := stubServer(t, page1, page2)
	since := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)

	digest, err := client.MyProjectItems(context.Background(), "acme", 7, since, until)
	if err != nil {
		t.Fatalf("MyProjectItems error = %v", err)
	}

	if digest.Login != "octocat" {
		t.Errorf("Login = %q, want octocat", digest.Login)
	}
	if digest.Title != "Roadmap" {
		t.Errorf("Title = %q, want Roadmap", digest.Title)
	}
	if len(digest.Items) != 2 {
		t.Fatalf("got %d items, want 2 (viewer's issue + draft): %+v", len(digest.Items), digest.Items)
	}
	if digest.Items[0].Title != "Mine" || digest.Items[0].Type != "Issue" {
		t.Errorf("item[0] = %+v, want viewer's issue", digest.Items[0])
	}
	if digest.Items[1].Title != "Draft mine" || digest.Items[1].State != "DRAFT" {
		t.Errorf("item[1] = %+v, want viewer's draft", digest.Items[1])
	}

	// Two queries were made; the second carried the page-1 end cursor.
	if len(*requests) != 2 {
		t.Fatalf("made %d queries, want 2", len(*requests))
	}
	if vars, _ := (*requests)[1]["variables"].(map[string]any); vars["cursor"] != "CUR1" {
		t.Errorf("second query cursor = %v, want CUR1", vars["cursor"])
	}
}

func TestOrgCommits_FiltersNoiseAndStopsOnOldRepo(t *testing.T) {
	since := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)
	inRange := "2026-07-19T12:00:00Z"

	// One page. First repo (pushed in range) has: a normal commit (kept), a
	// release commit (dropped), a merge commit with 2 parents (dropped). The
	// second repo was pushed before `since`, so the scan stops and never reads
	// its commits.
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
	if len(digest.Commits) != 1 {
		t.Fatalf("got %d commits, want 1 (release + merge + stale-repo dropped): %+v", len(digest.Commits), digest.Commits)
	}
	c := digest.Commits[0]
	if c.Repo != "acme/app" || c.Message != "Add retry logic" || c.Body != "body" ||
		c.URL != "https://x/c1" || c.OID != "c1" {
		t.Errorf("commit = %+v, unexpected fields", c)
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

func TestMyPullRequests_FiltersAndStopsOnOldPull(t *testing.T) {
	since := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)

	// One page holding, in update order: a pull request in the org and window
	// (kept), one in another org (dropped), a release pull request (dropped),
	// one updated at `until` (dropped, exclusive end), then one updated before
	// `since` — which stops the walk, so the cursor is never followed.
	body := `{"data":{"viewer":{
		"login":"octocat",
		"pullRequests":{
			"pageInfo":{"hasNextPage":true,"endCursor":"NEXT"},
			"nodes":[
				{"number":9,"title":"Bump linter","body":"desc","url":"https://x/9","state":"OPEN","isDraft":true,"updatedAt":"2026-07-19T12:00:00Z","repository":{"nameWithOwner":"acme/tools","owner":{"login":"acme"}}},
				{"number":8,"title":"Elsewhere","body":"","url":"https://x/8","state":"OPEN","isDraft":false,"updatedAt":"2026-07-19T11:00:00Z","repository":{"nameWithOwner":"other/app","owner":{"login":"other"}}},
				{"number":7,"title":"Release 2026-07-19","body":"","url":"https://x/7","state":"MERGED","isDraft":false,"updatedAt":"2026-07-19T10:00:00Z","repository":{"nameWithOwner":"acme/app","owner":{"login":"acme"}}},
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
	if len(digest.PullRequests) != 1 {
		t.Fatalf("got %d pull requests, want 1: %+v", len(digest.PullRequests), digest.PullRequests)
	}
	pr := digest.PullRequests[0]
	if pr.Number != 9 || pr.Repo != "acme/tools" || pr.Title != "Bump linter" ||
		pr.Body != "desc" || pr.URL != "https://x/9" || pr.State != "OPEN" || !pr.Draft {
		t.Errorf("pull request = %+v, unexpected fields", pr)
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
