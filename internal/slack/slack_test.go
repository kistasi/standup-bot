package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPost_Success(t *testing.T) {
	var gotBody map[string]string
	var gotContentType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok")
	}))
	defer srv.Close()

	if err := New(srv.URL).Post(context.Background(), "hello team"); err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody["text"] != "hello team" {
		t.Errorf("payload text = %q, want %q", gotBody["text"], "hello team")
	}
}

func TestPost_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, "invalid_payload")
	}))
	defer srv.Close()

	err := New(srv.URL).Post(context.Background(), "hi")
	if err == nil {
		t.Fatal("Post() error = nil, want error on 400")
	}
	if !strings.Contains(err.Error(), "invalid_payload") {
		t.Errorf("error = %q, want it to include the response body", err)
	}
}

func TestPost_OKStatusButNotOKBody(t *testing.T) {
	// Slack can return 200 with a non-"ok" body; that is still an error here.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "something_bad")
	}))
	defer srv.Close()

	if err := New(srv.URL).Post(context.Background(), "hi"); err == nil {
		t.Fatal("Post() error = nil, want error when body != ok")
	}
}

func TestPost_TrimsWhitespaceInOKBody(t *testing.T) {
	// A trailing newline around "ok" is trimmed and treated as success.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok\n")
	}))
	defer srv.Close()

	if err := New(srv.URL).Post(context.Background(), "hi"); err != nil {
		t.Errorf("Post() error = %v, want nil for \"ok\\n\"", err)
	}
}

func TestPost_TransportError(t *testing.T) {
	// Server is closed immediately, so the request fails at the transport level.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	if err := New(url).Post(context.Background(), "hi"); err == nil {
		t.Fatal("Post() error = nil, want transport error against closed server")
	}
}

func TestPost_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "ok")
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled before the call

	if err := New(srv.URL).Post(ctx, "hi"); err == nil {
		t.Fatal("Post() error = nil, want error for canceled context")
	}
}
