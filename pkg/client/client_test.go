package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPClientHealth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(200)
			w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer ts.Close()

	c := NewHTTPClient(ts.URL)
	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
}

func TestHTTPClientHeaders(t *testing.T) {
	var gotHeaders http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts.Close()

	c := NewHTTPClient(ts.URL,
		WithAPIKey("test-key"),
		WithAccountID("acct-1"),
		WithUserID("user-1"),
		WithAgentID("agent-1"),
	)
	c.Health(context.Background())

	if gotHeaders.Get("X-Api-Key") != "test-key" {
		t.Error("expected X-Api-Key header")
	}
	if gotHeaders.Get("X-Account-ID") != "acct-1" {
		t.Error("expected X-Account-ID header")
	}
	if gotHeaders.Get("X-User-ID") != "user-1" {
		t.Error("expected X-User-ID header")
	}
	if gotHeaders.Get("X-Agent-ID") != "agent-1" {
		t.Error("expected X-Agent-ID header")
	}
}

func TestHTTPClientFind(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/search/find" && r.Method == "POST" {
			var req FindRequest
			json.NewDecoder(r.Body).Decode(&req)
			if req.Query != "test query" {
				t.Errorf("expected 'test query', got %q", req.Query)
			}
			json.NewEncoder(w).Encode(FindResponse{
				Results: []FindResult{
					{URI: "viking://test/doc", Abstract: "Test doc", Score: 0.95},
				},
			})
			return
		}
		w.WriteHeader(404)
	}))
	defer ts.Close()

	c := NewHTTPClient(ts.URL)
	resp, err := c.Find(context.Background(), FindRequest{Query: "test query", Limit: 5})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.Results[0].Score != 0.95 {
		t.Errorf("expected score 0.95, got %f", resp.Results[0].Score)
	}
}

func TestHTTPClientServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]string{"error": "internal", "code": "INTERNAL"})
	}))
	defer ts.Close()

	c := NewHTTPClient(ts.URL)
	_, err := c.Status(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "INTERNAL") {
		t.Errorf("expected INTERNAL code in error, got: %v", err)
	}
}

func TestHTTPClientSessions(t *testing.T) {
	sessions := make(map[string]*Session)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/sessions" && r.Method == "POST":
			sess := &Session{ID: "sess-1", CreatedAt: "2026-01-01T00:00:00Z"}
			sessions[sess.ID] = sess
			json.NewEncoder(w).Encode(sess)
		case r.URL.Path == "/api/v1/sessions" && r.Method == "GET":
			list := make([]Session, 0)
			for _, s := range sessions {
				list = append(list, *s)
			}
			json.NewEncoder(w).Encode(list)
		case strings.HasPrefix(r.URL.Path, "/api/v1/sessions/") && r.Method == "GET":
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
			if s, ok := sessions[id]; ok {
				json.NewEncoder(w).Encode(s)
			} else {
				w.WriteHeader(404)
				json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
			}
		case strings.HasPrefix(r.URL.Path, "/api/v1/sessions/") && r.Method == "DELETE":
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
			delete(sessions, id)
			w.WriteHeader(200)
		default:
			w.WriteHeader(404)
		}
	}))
	defer ts.Close()

	c := NewHTTPClient(ts.URL)
	ctx := context.Background()

	sess, err := c.CreateSession(ctx)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sess.ID != "sess-1" {
		t.Errorf("expected sess-1, got %s", sess.ID)
	}

	got, err := c.GetSession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.ID != "sess-1" {
		t.Error("session ID mismatch")
	}

	list, err := c.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 session, got %d", len(list))
	}

	if err := c.DeleteSession(ctx, "sess-1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	list, err = c.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 sessions after delete, got %d", len(list))
	}
}

func TestHTTPClientFilesystem(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/fs/ls":
			json.NewEncoder(w).Encode([]FSEntry{
				{URI: "viking://test/dir", Name: "dir", IsDir: true},
				{URI: "viking://test/file.md", Name: "file.md", IsDir: false},
			})
		case r.URL.Path == "/api/v1/fs/stat":
			json.NewEncoder(w).Encode(FSEntry{URI: r.URL.Query().Get("uri"), Name: "doc", IsDir: false})
		case r.URL.Path == "/api/v1/fs/mkdir":
			w.WriteHeader(200)
		case r.URL.Path == "/api/v1/fs/rm":
			w.WriteHeader(200)
		case r.URL.Path == "/api/v1/fs/mv":
			w.WriteHeader(200)
		default:
			w.WriteHeader(404)
		}
	}))
	defer ts.Close()

	c := NewHTTPClient(ts.URL)
	ctx := context.Background()

	entries, err := c.Ls(ctx, "viking://test/", LsOptions{})
	if err != nil {
		t.Fatalf("Ls: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}

	stat, err := c.Stat(ctx, "viking://test/doc")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if stat.URI != "viking://test/doc" {
		t.Errorf("unexpected URI: %s", stat.URI)
	}

	if err := c.Mkdir(ctx, "viking://test/newdir"); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := c.Rm(ctx, "viking://test/old", true); err != nil {
		t.Fatalf("Rm: %v", err)
	}
	if err := c.Mv(ctx, "viking://test/a", "viking://test/b"); err != nil {
		t.Fatalf("Mv: %v", err)
	}
}

func TestLocalClientInterface(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(200)
			w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(404)
	})

	c := NewLocalClient(handler)
	defer c.Close(context.Background())

	if err := c.Health(context.Background()); err != nil {
		t.Fatalf("LocalClient Health: %v", err)
	}
}

func TestLocalClientRawRequest(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"hello": "world"})
	})

	c := NewLocalClient(handler)
	defer c.Close(context.Background())

	resp, err := c.RawRequest(context.Background(), "GET", "/custom", nil)
	if err != nil {
		t.Fatalf("RawRequest: %v", err)
	}
	var result map[string]string
	json.Unmarshal(resp, &result)
	if result["hello"] != "world" {
		t.Errorf("unexpected response: %s", string(resp))
	}
}

func TestInterfaceCompliance(t *testing.T) {
	var _ Client = (*HTTPClient)(nil)
	var _ Client = (*LocalClient)(nil)
}
