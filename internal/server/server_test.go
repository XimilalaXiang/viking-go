package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ximilala/viking-go/internal/retriever"
	"github.com/ximilala/viking-go/internal/storage"
	"github.com/ximilala/viking-go/internal/vikingfs"

	_ "github.com/mattn/go-sqlite3"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	store, err := storage.NewStore(":memory:")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	dir := t.TempDir()
	vfs, err := vikingfs.New(dir)
	if err != nil {
		t.Fatalf("NewVikingFS: %v", err)
	}

	ret := retriever.NewHierarchicalRetriever(store, nil, nil, 0)
	return NewServer(store, vfs, ret, "dev", "")
}

func TestHealthEndpoint(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "ok" {
		t.Errorf("status = %v", body["status"])
	}
}

func TestStatusEndpoint(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest("GET", "/api/v1/system/status", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["backend"] != "sqlite" {
		t.Errorf("backend = %v", body["backend"])
	}
}

func TestWriteAndReadContent(t *testing.T) {
	srv := testServer(t)

	// Write
	writeBody, _ := json.Marshal(map[string]string{
		"uri":     "viking://resources/test.md",
		"content": "Hello from test",
	})
	wReq := httptest.NewRequest("POST", "/api/v1/content/write", bytes.NewReader(writeBody))
	wReq.Header.Set("Content-Type", "application/json")
	wW := httptest.NewRecorder()
	srv.ServeHTTP(wW, wReq)

	if wW.Code != http.StatusOK {
		t.Fatalf("write status = %d, body = %s", wW.Code, wW.Body.String())
	}

	// Read
	rReq := httptest.NewRequest("GET", "/api/v1/content/read?uri=viking://resources/test.md", nil)
	rW := httptest.NewRecorder()
	srv.ServeHTTP(rW, rReq)

	if rW.Code != http.StatusOK {
		t.Fatalf("read status = %d", rW.Code)
	}

	var body map[string]any
	json.Unmarshal(rW.Body.Bytes(), &body)
	if body["content"] != "Hello from test" {
		t.Errorf("content = %v", body["content"])
	}
}

func TestLsEndpoint(t *testing.T) {
	srv := testServer(t)

	// Create some files first
	for _, name := range []string{"a.md", "b.md"} {
		body, _ := json.Marshal(map[string]string{
			"uri":     "viking://resources/" + name,
			"content": "content",
		})
		req := httptest.NewRequest("POST", "/api/v1/content/write", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
	}

	req := httptest.NewRequest("GET", "/api/v1/fs/ls?uri=viking://resources", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("ls status = %d", w.Code)
	}

	var entries []map[string]any
	json.Unmarshal(w.Body.Bytes(), &entries)
	if len(entries) != 2 {
		t.Errorf("ls entries = %d, want 2", len(entries))
	}
}

func TestMkdirAndStat(t *testing.T) {
	srv := testServer(t)

	body, _ := json.Marshal(map[string]string{
		"uri": "viking://resources/mydir",
	})
	mkReq := httptest.NewRequest("POST", "/api/v1/fs/mkdir", bytes.NewReader(body))
	mkReq.Header.Set("Content-Type", "application/json")
	mkW := httptest.NewRecorder()
	srv.ServeHTTP(mkW, mkReq)

	if mkW.Code != http.StatusOK {
		t.Fatalf("mkdir status = %d", mkW.Code)
	}

	stReq := httptest.NewRequest("GET", "/api/v1/fs/stat?uri=viking://resources/mydir", nil)
	stW := httptest.NewRecorder()
	srv.ServeHTTP(stW, stReq)

	if stW.Code != http.StatusOK {
		t.Fatalf("stat status = %d", stW.Code)
	}

	var stat map[string]any
	json.Unmarshal(stW.Body.Bytes(), &stat)
	if stat["isDir"] != true {
		t.Errorf("isDir = %v", stat["isDir"])
	}
}

func TestFindWithNoEmbedder(t *testing.T) {
	srv := testServer(t)

	body, _ := json.Marshal(map[string]any{
		"query": "test query",
		"limit": 5,
	})
	req := httptest.NewRequest("POST", "/api/v1/search/find", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("find status = %d, body = %s", w.Code, w.Body.String())
	}

	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	// Should return empty results without error when no embedder is configured
	if result["memories"] != nil && len(result["memories"].([]any)) > 0 {
		t.Errorf("expected empty memories, got %v", result["memories"])
	}
}

func TestRelationsEndpoints(t *testing.T) {
	srv := testServer(t)

	// Create directories
	for _, dir := range []string{"viking://user/memories/a", "viking://user/memories/b"} {
		body, _ := json.Marshal(map[string]string{"uri": dir})
		req := httptest.NewRequest("POST", "/api/v1/fs/mkdir", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
	}

	// Link
	linkBody, _ := json.Marshal(map[string]any{
		"from_uri": "viking://user/memories/a",
		"uris":     []string{"viking://user/memories/b"},
		"reason":   "related",
	})
	lReq := httptest.NewRequest("POST", "/api/v1/relations/link", bytes.NewReader(linkBody))
	lReq.Header.Set("Content-Type", "application/json")
	lW := httptest.NewRecorder()
	srv.ServeHTTP(lW, lReq)
	if lW.Code != http.StatusOK {
		t.Fatalf("link status = %d", lW.Code)
	}

	// Get relations
	gReq := httptest.NewRequest("GET", "/api/v1/relations?uri=viking://user/memories/a", nil)
	gW := httptest.NewRecorder()
	srv.ServeHTTP(gW, gReq)
	if gW.Code != http.StatusOK {
		t.Fatalf("relations status = %d", gW.Code)
	}

	var entries []map[string]any
	json.Unmarshal(gW.Body.Bytes(), &entries)
	if len(entries) != 1 {
		t.Fatalf("relations count = %d, want 1", len(entries))
	}
}
