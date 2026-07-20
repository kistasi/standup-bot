package gh

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shurcooL/githubv4"
)

func TestAuthTransport_InjectsBearerWithoutMutatingCaller(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
	}))
	defer srv.Close()

	tr := authTransport{token: "sekret", base: http.DefaultTransport}

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error = %v", err)
	}
	resp.Body.Close()

	if gotAuth != "Bearer sekret" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer sekret")
	}
	// The caller's request must be left untouched (the RoundTripper clones it).
	if h := req.Header.Get("Authorization"); h != "" {
		t.Errorf("caller request mutated: Authorization = %q, want empty", h)
	}
}

func TestIsReleaseCommit(t *testing.T) {
	tests := []struct {
		headline string
		want     bool
	}{
		{"release 2026-07-20 (#506)", true},
		{"Release 2026-07-20", true},
		{"RELEASE v1.2.3", true},
		{"  release stuff", true}, // leading whitespace is trimmed
		{"released the hounds", false},
		{"releasing", false}, // no trailing space after "release"
		{"Fix the release bug", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isReleaseCommit(tt.headline); got != tt.want {
			t.Errorf("isReleaseCommit(%q) = %v, want %v", tt.headline, got, tt.want)
		}
	}
}

// issueNode builds an Issue/PullRequest itemNode with a single assignee.
func issueNode(typename, title, body, url, state, assignee string, updated time.Time) itemNode {
	c := issueContent{
		Title:     githubv4.String(title),
		Body:      githubv4.String(body),
		URL:       githubv4.String(url),
		State:     githubv4.String(state),
		UpdatedAt: githubv4.DateTime{Time: updated},
	}
	c.Assignees.Nodes = []struct{ Login githubv4.String }{{Login: githubv4.String(assignee)}}

	var n itemNode
	n.Content.Typename = githubv4.String(typename)
	if typename == "PullRequest" {
		n.Content.PullRequest = c
	} else {
		n.Content.Issue = c
	}
	return n
}

func draftNode(title, body, assignee string, updated time.Time) itemNode {
	c := draftContent{
		Title:     githubv4.String(title),
		Body:      githubv4.String(body),
		UpdatedAt: githubv4.DateTime{Time: updated},
	}
	c.Assignees.Nodes = []struct{ Login githubv4.String }{{Login: githubv4.String(assignee)}}

	var n itemNode
	n.Content.Typename = "DraftIssue"
	n.Content.DraftIssue = c
	return n
}

func TestToItem(t *testing.T) {
	since := time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)
	recent := since.Add(time.Hour)
	old := since.Add(-time.Hour)

	t.Run("issue assigned to viewer is kept", func(t *testing.T) {
		n := issueNode("Issue", "Bug", "desc", "https://x/1", "OPEN", "octocat", recent)
		item, ok := n.toItem("octocat", since)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if item.Type != "Issue" || item.Title != "Bug" || item.Body != "desc" ||
			item.URL != "https://x/1" || item.State != "OPEN" || !item.UpdatedAt.Equal(recent) {
			t.Errorf("item = %+v, unexpected fields", item)
		}
	})

	t.Run("pull request reads PullRequest fragment", func(t *testing.T) {
		n := issueNode("PullRequest", "Add feature", "b", "https://x/2", "MERGED", "octocat", recent)
		item, ok := n.toItem("octocat", since)
		if !ok || item.Type != "PullRequest" || item.Title != "Add feature" || item.State != "MERGED" {
			t.Errorf("item = %+v, ok = %v; want PullRequest fields", item, ok)
		}
	})

	t.Run("draft issue has DRAFT state and no url", func(t *testing.T) {
		n := draftNode("Idea", "notes", "octocat", recent)
		item, ok := n.toItem("octocat", since)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if item.Type != "DraftIssue" || item.State != "DRAFT" || item.URL != "" {
			t.Errorf("item = %+v, want DraftIssue/DRAFT/empty-url", item)
		}
	})

	t.Run("not assigned to viewer is dropped", func(t *testing.T) {
		n := issueNode("Issue", "Bug", "d", "https://x/1", "OPEN", "someone-else", recent)
		if _, ok := n.toItem("octocat", since); ok {
			t.Error("ok = true, want false for item assigned to another user")
		}
	})

	t.Run("updated before since is dropped", func(t *testing.T) {
		n := issueNode("Issue", "Bug", "d", "https://x/1", "OPEN", "octocat", old)
		if _, ok := n.toItem("octocat", since); ok {
			t.Error("ok = true, want false for stale item")
		}
	})

	t.Run("updated exactly at since is kept", func(t *testing.T) {
		n := issueNode("Issue", "Bug", "d", "https://x/1", "OPEN", "octocat", since)
		if _, ok := n.toItem("octocat", since); !ok {
			t.Error("ok = false, want true when UpdatedAt == since")
		}
	})

	t.Run("unknown typename is dropped", func(t *testing.T) {
		var n itemNode
		n.Content.Typename = "" // e.g. a redacted item the PAT can't see
		if _, ok := n.toItem("octocat", since); ok {
			t.Error("ok = true, want false for empty typename")
		}
	})
}
