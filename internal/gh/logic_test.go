package gh

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestIsReleaseBranch(t *testing.T) {
	tests := []struct {
		ref  string
		want bool
	}{
		{"release", true},
		{"releases", true},
		{"release/2026-07-20", true},
		{"release-1.2.3", true},
		{"release_1.2.3", true},
		{"release.1", true},
		{"releases/1.2.3", true},
		{"RELEASE/1.2.3", true},
		{" release/1.2.3 ", true}, // whitespace is trimmed
		{"released", false},       // no separator after the prefix
		{"releasing", false},
		{"feature/release-notes", false},
		{"main", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isReleaseBranch(tt.ref); got != tt.want {
			t.Errorf("isReleaseBranch(%q) = %v, want %v", tt.ref, got, tt.want)
		}
	}
}

// pullRequestNode builds a pullNode in org "acme" with the given branch/title.
func pullRequestNode(title, branch string, updated time.Time) pullNode {
	var n pullNode
	n.Title = githubv4.String(title)
	n.HeadRefName = githubv4.String(branch)
	n.State = "MERGED"
	n.URL = "https://x/1"
	n.UpdatedAt = githubv4.DateTime{Time: updated}
	n.Repository.NameWithOwner = "acme/app"
	n.Repository.Owner.Login = "acme"
	return n
}

func TestToPullRequest_Delivered(t *testing.T) {
	updated := time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)
	until := updated.Add(time.Hour)

	t.Run("release branch is kept and marked delivered", func(t *testing.T) {
		pr, ok := pullRequestNode("Merge for the sprint", "release/2026-07-20", updated).toPullRequest("acme", until)
		if !ok {
			t.Fatal("ok = false, want true: a release pull request is delivered work, not noise")
		}
		if !pr.Delivered {
			t.Error("Delivered = false, want true for a release branch")
		}
		if pr.Branch != "release/2026-07-20" {
			t.Errorf("Branch = %q, want release/2026-07-20", pr.Branch)
		}
	})

	t.Run("release title is marked delivered", func(t *testing.T) {
		pr, ok := pullRequestNode("Release 2026-07-20", "hotfix", updated).toPullRequest("acme", until)
		if !ok || !pr.Delivered {
			t.Errorf("pr = %+v, ok = %v; want a delivered pull request", pr, ok)
		}
	})

	t.Run("ordinary branch is not delivered", func(t *testing.T) {
		pr, ok := pullRequestNode("Add retry logic", "feature/retries", updated).toPullRequest("acme", until)
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if pr.Delivered {
			t.Error("Delivered = true, want false for a feature branch")
		}
	})

	t.Run("other org is dropped", func(t *testing.T) {
		n := pullRequestNode("Add retry logic", "main", updated)
		n.Repository.Owner.Login = "other"
		if _, ok := n.toPullRequest("acme", until); ok {
			t.Error("ok = true, want false for another organization")
		}
	})

	// The window is half-open, so a pull request updated at `until` belongs to
	// the next window.
	t.Run("updated at until is dropped", func(t *testing.T) {
		if _, ok := pullRequestNode("Add retry logic", "main", until).toPullRequest("acme", until); ok {
			t.Error("ok = true, want false when UpdatedAt == until")
		}
	})
}

func TestAppendCommits_KeepsReleasesDropsMerges(t *testing.T) {
	node := func(headline string, parents int) commitNode {
		var n commitNode
		n.MessageHeadline = githubv4.String(headline)
		n.Parents.TotalCount = githubv4.Int(parents)
		return n
	}

	got := appendCommits(nil, "acme/app", []commitNode{
		node("Add retry logic", 1),
		node("Release 2026-07-19 (#9)", 1),
		node("Merge branch main", 2),
	})

	if len(got) != 2 {
		t.Fatalf("got %d commits, want 2 (merge dropped, release kept): %+v", len(got), got)
	}
	if got[0].Delivered {
		t.Error("commit[0].Delivered = true, want false for an ordinary commit")
	}
	if !got[1].Delivered {
		t.Error("commit[1].Delivered = false, want true for a release commit")
	}
}

func TestRetryTransport_RetriesGatewayErrorsWithBody(t *testing.T) {
	var calls int
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if calls < 3 {
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := retryTransport{base: http.DefaultTransport, waits: []time.Duration{0, 0, 0}}
	req, err := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(`{"query":"q"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error = %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
	for i, b := range bodies {
		if b != `{"query":"q"}` {
			t.Errorf("attempt %d body = %q, want the original body", i+1, b)
		}
	}
}

func TestRetryTransport_GivesUpAfterWaits(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	tr := retryTransport{base: http.DefaultTransport, waits: []time.Duration{0, 0}}
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error = %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (one try plus two retries)", calls)
	}
}

func TestRetryTransport_DoesNotRetryOtherErrors(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	tr := retryTransport{base: http.DefaultTransport, waits: []time.Duration{0, 0}}
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error = %v", err)
	}
	resp.Body.Close()

	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}
