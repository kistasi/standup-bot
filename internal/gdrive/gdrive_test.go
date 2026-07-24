package gdrive

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

// call is one request the client made, reduced to what the tests assert on.
type call struct {
	method string
	path   string
	query  string // the "q" search parameter, empty for non-list calls
	body   string
}

// stubServer returns a Client pointed at an httptest server that replies with
// the given JSON bodies in order, one per request, plus the calls it recorded.
// Credentials are bypassed with WithoutAuthentication, mirroring internal/gcal.
func stubServer(t *testing.T, folderName string, bodies ...string) (*Client, *[]call) {
	t.Helper()
	var calls []call
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, call{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.Query().Get("q"),
			body:   string(body),
		})

		if i >= len(bodies) {
			t.Errorf("unexpected request #%d (%s %s), only %d responses stubbed", i+1, r.Method, r.URL.Path, len(bodies))
			http.Error(w, "no more responses", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, bodies[i])
		i++
	}))
	t.Cleanup(srv.Close)

	svc, err := drive.NewService(context.Background(),
		option.WithEndpoint(srv.URL),
		option.WithHTTPClient(srv.Client()),
		option.WithoutAuthentication(),
	)
	if err != nil {
		t.Fatalf("drive.NewService() error = %v", err)
	}
	return &Client{svc: svc, folderName: folderName}, &calls
}

func TestUpload_CreatesFolderAndFile(t *testing.T) {
	c, calls := stubServer(t, "standup-bot",
		`{"files": []}`,      // no folder yet
		`{"id": "folder-1"}`, // folder created
		`{"files": []}`,      // no such report yet
		`{"webViewLink": "https://drive.example/file-1"}`, // upload
	)

	link, err := c.Upload(context.Background(), "2026-07-24-standup.md", []byte("# Standup\n"))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if want := "https://drive.example/file-1"; link != want {
		t.Errorf("link = %q, want %q", link, want)
	}

	if len(*calls) != 4 {
		t.Fatalf("made %d requests, want 4: %+v", len(*calls), *calls)
	}
	got := *calls

	// The folder lookup must be scoped to folders, by name.
	if !strings.Contains(got[0].query, "name = 'standup-bot'") ||
		!strings.Contains(got[0].query, folderMIME) {
		t.Errorf("folder lookup q = %q, want a name+folder query", got[0].query)
	}
	// The file lookup must be scoped to that folder.
	if !strings.Contains(got[2].query, "'folder-1' in parents") ||
		!strings.Contains(got[2].query, "name = '2026-07-24-standup.md'") {
		t.Errorf("file lookup q = %q, want name+parent query", got[2].query)
	}
	// The new file is created in the folder, with its content.
	if got[3].method != http.MethodPost {
		t.Errorf("upload method = %s, want POST", got[3].method)
	}
	if !strings.Contains(got[3].body, `"folder-1"`) {
		t.Errorf("upload body has no parent folder: %s", got[3].body)
	}
	if !strings.Contains(got[3].body, "# Standup") {
		t.Errorf("upload body has no content: %s", got[3].body)
	}
}

func TestUpload_UpdatesExistingFileInExistingFolder(t *testing.T) {
	c, calls := stubServer(t, "standup-bot",
		`{"files": [{"id": "folder-1"}]}`,                 // folder already there
		`{"files": [{"id": "file-9"}]}`,                   // report already there
		`{"webViewLink": "https://drive.example/file-9"}`, // updated in place
	)

	link, err := c.Upload(context.Background(), "2026-06-monthly-report.md", []byte("# June 2026\n"))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if want := "https://drive.example/file-9"; link != want {
		t.Errorf("link = %q, want %q", link, want)
	}

	got := *calls
	if len(got) != 3 {
		t.Fatalf("made %d requests, want 3 (no folder creation): %+v", len(got), got)
	}
	// An update, not a second file: PATCH against the known ID.
	if got[2].method != http.MethodPatch {
		t.Errorf("update method = %s, want PATCH", got[2].method)
	}
	if !strings.Contains(got[2].path, "file-9") {
		t.Errorf("update path = %q, want it to name file-9", got[2].path)
	}
	if !strings.Contains(got[2].body, "# June 2026") {
		t.Errorf("update body has no content: %s", got[2].body)
	}
}

func TestUpload_ResolvesFolderOnce(t *testing.T) {
	c, calls := stubServer(t, "standup-bot",
		`{"files": [{"id": "folder-1"}]}`,
		`{"files": []}`,
		`{"webViewLink": "https://drive.example/a"}`,
		`{"files": []}`,
		`{"webViewLink": "https://drive.example/b"}`,
	)

	ctx := context.Background()
	for _, name := range []string{"a.md", "b.md"} {
		if _, err := c.Upload(ctx, name, []byte("x")); err != nil {
			t.Fatalf("Upload(%s) error = %v", name, err)
		}
	}

	// Five requests, not six: the second upload reuses the cached folder ID.
	if len(*calls) != 5 {
		t.Fatalf("made %d requests, want 5 (folder looked up once): %+v", len(*calls), *calls)
	}
	if c.folderID != "folder-1" {
		t.Errorf("folderID = %q, want it cached as folder-1", c.folderID)
	}
}

func TestUpload_ReportsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error": {"message": "insufficient scope"}}`, http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	svc, err := drive.NewService(context.Background(),
		option.WithEndpoint(srv.URL),
		option.WithHTTPClient(srv.Client()),
		option.WithoutAuthentication(),
	)
	if err != nil {
		t.Fatalf("drive.NewService() error = %v", err)
	}
	c := &Client{svc: svc, folderName: "standup-bot"}

	_, err = c.Upload(context.Background(), "x.md", []byte("x"))
	if err == nil {
		t.Fatal("Upload() error = nil, want the API failure surfaced")
	}
	// The message should say which folder failed, not just "403".
	if !strings.Contains(err.Error(), "standup-bot") {
		t.Errorf("error = %v, want it to name the folder", err)
	}
}

func TestEscape(t *testing.T) {
	tests := []struct{ in, want string }{
		{"standup-bot", "standup-bot"},
		{"Marton's reports", `Marton\'s reports`},
		{`back\slash`, `back\\slash`},
	}
	for _, tt := range tests {
		if got := escape(tt.in); got != tt.want {
			t.Errorf("escape(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
